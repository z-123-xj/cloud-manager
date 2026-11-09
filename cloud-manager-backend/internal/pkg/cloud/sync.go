package cloud

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun"
	"cloud-manager-backend/internal/pkg/cloud/aws"
	"cloud-manager-backend/internal/pkg/cloud/cloudflare"
	"cloud-manager-backend/internal/pkg/cloud/tencent"
	"cloud-manager-backend/pkg/logger"
	"cloud-manager-backend/pkg/utils"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 进度推送 ====================

// SyncProgress 同步进度管理器
type SyncProgress struct {
	mu      sync.Mutex
	clients map[string]chan string // taskID -> message channel
}

var progressHub = &SyncProgress{
	clients: make(map[string]chan string),
}

func (h *SyncProgress) subscribe(taskID string) chan string {
	h.mu.Lock()
	defer h.mu.Unlock()
	// 如果 channel 已存在（SyncAllResources 已创建），直接复用
	if ch, ok := h.clients[taskID]; ok {
		return ch
	}
	ch := make(chan string, 500)
	h.clients[taskID] = ch
	return ch
}

func (h *SyncProgress) unsubscribe(taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.clients[taskID]; ok {
		close(ch)
		delete(h.clients, taskID)
	}
}

func (h *SyncProgress) send(taskID, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.clients[taskID]; ok {
		select {
		case ch <- msg:
		default:
			// channel 满了就丢弃，避免阻塞同步
		}
	}
}

// logf 同时输出到终端和 SSE
func logf(taskID, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Print(msg)
	progressHub.send(taskID, msg)
}

// ==================== 同步器接口 ====================

// CloudSyncer 定义云平台同步器接口
type CloudSyncer interface {
	SyncDomains(account models.CloudAccount)    // 域名注册列表同步（domains 表）
	SyncDnsDomains(account models.CloudAccount) // DNS可管理域名同步（dns_domains 表）
	SyncEcs(account models.CloudAccount)        // ECS实例同步
	SyncCdn(account models.CloudAccount)        // CDN加速域名同步（cdn_domains 表，aliyun/aws 实现，其余平台跳过）
}

func dispatchSync(platform string) CloudSyncer {
	switch platform {
	case "aliyun":
		return &aliyun.Syncer{}
	case "aws":
		return &aws.Syncer{}
	case "tencent":
		return &tencent.Syncer{}
	case "cloudflare":
		return &cloudflare.Syncer{}
	default:
		return nil
	}
}

var syncMu sync.Mutex

// ==================== 同步入口 ====================

// SyncAllResources 全账号同步入口（资源类型为 domains/dns/ecs/cdn/all 之一）
// accountIDs: 非空时只同步指定账号（前端资源页按账号勾选）；为空则同步全部启用账号
func SyncAllResources(resourceType string, taskID string, accountIDs []string) {
	go runSync(taskID, accountIDs, []string{resourceType})
}

// SyncAccountResources 单账号按需同步入口（云账号管理页使用）
// accountIDs: 指定账号列表；resources: 本次要同步的资源类型列表（域名/DNS域名/ECS/CDN 任意组合）
func SyncAccountResources(accountIDs []string, resources []string, taskID string) {
	go runSync(taskID, accountIDs, resources)
}

// containsResource 判断资源类型是否在本次同步范围内（"all" 覆盖除 cdn 外的全部资源，
// cdn 需显式选择或包含在 all 中）
func containsResource(resources []string, target string) bool {
	for _, r := range resources {
		if r == target || r == "all" {
			return true
		}
	}
	return false
}

// syncAccountResource 对单个账号执行一种资源的同步（含开始日志）
func syncAccountResource(account models.CloudAccount, resourceType string) {
	syncer := dispatchSync(account.Platform)
	if syncer == nil {
		skip := fmt.Sprintf("跳过不支持的平台 [%s] 账号: %s", account.Platform, account.Name)
		fmt.Println(skip)
		fmt.Fprintln(os.Stderr, skip)
		logger.Info("sync", skip)
		return
	}
	switch resourceType {
	case "domains":
		fmt.Printf("\n========== 开始同步域名 账号: %s (平台: %s) ==========\n", account.Name, account.Platform)
		syncer.SyncDomains(account)
	case "dns":
		fmt.Printf("\n========== 开始同步DNS域名 账号: %s (平台: %s) ==========\n", account.Name, account.Platform)
		syncer.SyncDnsDomains(account)
	case "ecs":
		fmt.Printf("\n========== 开始同步ECS 账号: %s (平台: %s) ==========\n", account.Name, account.Platform)
		syncer.SyncEcs(account)
	case "cdn":
		fmt.Printf("\n========== 开始同步CDN 账号: %s (平台: %s) ==========\n", account.Name, account.Platform)
		syncer.SyncCdn(account)
	case "all":
		fmt.Printf("\n========== 开始同步全部资源 账号: %s (平台: %s) ==========\n", account.Name, account.Platform)
		syncer.SyncDomains(account)
		syncer.SyncDnsDomains(account)
		syncer.SyncEcs(account)
		syncer.SyncCdn(account)
	}
}

// runSync 同步任务主流程（全账号同步与单账号按需同步共用）
//   - accountID 为空：遍历所有账号（原“同步全部”行为）；非空：只同步指定账号
//   - resources：本次同步的资源类型列表，支持任意组合（域名/DNS域名/ECS/CDN/all）
//   - 带 2 小时超时保护、SSE 通道、下线标记与统计
func runSync(taskID string, accountIDs []string, resources []string) {
	// 防止并发同步导致日志交错
	if !syncMu.TryLock() {
		fmt.Println("已有同步任务正在执行，请等待完成后再试")
		return
	}
	defer syncMu.Unlock()

	// 同步超时保护：单次同步最多执行 2 小时
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	// 先创建 channel（由 runSync 统一管理生命周期）
	// HTTP handler 中的 subscribe 会复用此 channel
	progressHub.subscribe(taskID)
	defer progressHub.unsubscribe(taskID)

	// 重定向底层 fmt.Printf 的输出到 SSE
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// 读取 pipe 并分发到 SSE
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				msg := string(buf[:n])
				progressHub.send(taskID, msg)
			}
			if err != nil {
				break
			}
		}
	}()

	defer func() {
		w.Close()
		wg.Wait()
		os.Stdout = oldStdout
	}()

	// 同步前先校验数据库结构：缺表/缺列直接中止，避免同步中途才报 Unknown column / table doesn't exist
	if err := models.CheckSchemaReady("scripts/init_db.sql"); err != nil {
		fmt.Println("【同步中止】" + err.Error())             // → SSE / 前端同步进度面板
		fmt.Fprintln(os.Stderr, "【同步中止】"+err.Error())   // → 服务端日志(journal)
		return
	}

	// 加载账号列表（accountIDs 非空时只同步指定账号；为空则同步全部启用账号）
	var accounts []models.CloudAccount
	query := models.DB.Model(&models.CloudAccount{})
	filterByAccount := len(accountIDs) > 0
	if filterByAccount {
		query = query.Where("account_id IN (?)", accountIDs)
	}
	query.Find(&accounts)

	if len(accounts) == 0 {
		if filterByAccount {
			fmt.Printf("未找到指定账号 %v\n", accountIDs)
		} else {
			fmt.Println("没有找到任何云账号")
		}
		return
	}

	syncStartTime := time.Now().Add(-time.Second)

	// ===== 第一步：逐账号逐资源同步 =====
	for _, account := range accounts {
		// 检查是否超时
		select {
		case <-ctx.Done():
			fmt.Println("\n========== 同步超时，任务中止 ==========")
			return
		default:
		}

		if !account.Enabled {
			if filterByAccount {
				// 指定账号同步：账号被禁用直接跳过，不做下线标记（避免误伤，仅提示）
				fmt.Printf("账号已禁用，跳过同步: %s (平台: %s)\n", account.Name, account.Platform)
				continue
			}
			// 全量同步：禁用账号的全部资产标记下线（原有行为）
			fmt.Printf("  账号已禁用，全部标记下线: %s (平台: %s)\n", account.Name, account.Platform)
			now := time.Now()
			if containsResource(resources, "domains") {
				result := models.DB.Model(&models.Domain{}).
					Where("account_id = ? AND is_offline = 0", account.AccountID).
					Updates(map[string]interface{}{"is_offline": 1, "updated_at": now})
				if result.RowsAffected > 0 {
					fmt.Printf("    域名下线: %d 个\n", result.RowsAffected)
				}
			}
			if containsResource(resources, "ecs") {
				result := models.DB.Model(&models.EcsInstance{}).
					Where("account_id = ? AND is_offline = 0", account.AccountID).
					Updates(map[string]interface{}{"is_offline": 1, "updated_at": now})
				if result.RowsAffected > 0 {
					fmt.Printf("    ECS下线: %d 个\n", result.RowsAffected)
				}
			}
			continue
		}

		// 解密 AccessSecret（各同步器需要明文凭证）
		accessKeySecret, err := utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			continue
		}
		account.AccessSecret = accessKeySecret

		for _, resourceType := range resources {
			// 每种资源同步前都检查超时
			select {
			case <-ctx.Done():
				fmt.Println("\n========== 同步超时，任务中止 ==========")
				return
			default:
			}
			// 单账号单资源独立执行：用 recover 兜底，某个账号/资源的同步 panic 只影响自己，
			// 不影响其它账号/资源（“那个出问题跳那个”），避免一次崩溃中断整轮同步。
			func() {
				defer func() {
					if r := recover(); r != nil {
						msg := fmt.Sprintf("账号 %s 资源 %s 同步异常，已跳过", account.Name, resourceType)
						fmt.Printf("  [panic] %s: %v\n", msg, r)
						logger.Error("sync", msg, "account", account.Name, "resource", resourceType, "panic", fmt.Sprint(r))
					}
				}()
				syncAccountResource(account, resourceType)
			}()
		}
	}

	// ===== 第二步：标记下线（domains/ecs 有本地 is_offline 概念；dns/cdn 为整账号刷新，无需标记）=====
	fmt.Println("\n========== 所有账号同步完成，开始统一标记下线 ==========")
	for _, account := range accounts {
		if !account.Enabled {
			continue // 禁用账号已在上面处理
		}
		if containsResource(resources, "domains") {
			result := models.DB.Model(&models.Domain{}).
				Where("account_id = ? AND is_offline = 0 AND updated_at < ?", account.AccountID, syncStartTime).
				Update("is_offline", 1)
			if result.RowsAffected > 0 {
				fmt.Printf("  账号 %s 域名标记下线: %d 个\n", account.Name, result.RowsAffected)
			}
		}
		if containsResource(resources, "ecs") {
			result := models.DB.Model(&models.EcsInstance{}).
				Where("account_id = ? AND is_offline = 0 AND updated_at < ?", account.AccountID, syncStartTime).
				Update("is_offline", 1)
			if result.RowsAffected > 0 {
				fmt.Printf("  账号 %s ECS标记下线: %d 个\n", account.Name, result.RowsAffected)
			}
		}
	}

	// ===== 第三步：统计 =====
	fmt.Println("\n========================================")
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if containsResource(resources, "domains") {
			var onlineCount, offlineCount int64
			models.DB.Model(&models.Domain{}).Where("account_id = ? AND is_offline = 0", account.AccountID).Count(&onlineCount)
			models.DB.Model(&models.Domain{}).Where("account_id = ? AND is_offline = 1", account.AccountID).Count(&offlineCount)
			fmt.Printf("账号 %s 域名: 在线 %d 个, 已下线 %d 个\n", account.Name, onlineCount, offlineCount)
		}
		if containsResource(resources, "ecs") {
			var onlineCount, offlineCount int64
			models.DB.Model(&models.EcsInstance{}).Where("account_id = ? AND is_offline = 0", account.AccountID).Count(&onlineCount)
			models.DB.Model(&models.EcsInstance{}).Where("account_id = ? AND is_offline = 1", account.AccountID).Count(&offlineCount)
			fmt.Printf("账号 %s ECS: 在线 %d 个, 已下线 %d 个\n", account.Name, onlineCount, offlineCount)
		}
	}
	fmt.Println("===== 同步全部完成 =====")
	fmt.Println("========================================")
}

// ==================== HTTP Handler ====================

func SyncDomainsHandler(c *gin.Context) {
	taskID := fmt.Sprintf("sync-domains-%d", time.Now().UnixNano())
	var req struct {
		AccountIDs []string `json:"account_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	// channel 由 runSync 统一管理，Handler 不直接 subscribe
	SyncAllResources("domains", taskID, req.AccountIDs)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "域名同步任务已触发", "task_id": taskID})
}

func SyncDnsDomainsHandler(c *gin.Context) {
	taskID := fmt.Sprintf("sync-dns-%d", time.Now().UnixNano())
	var req struct {
		AccountIDs []string `json:"account_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	SyncAllResources("dns", taskID, req.AccountIDs)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "DNS域名同步任务已触发", "task_id": taskID})
}

func SyncEcsHandler(c *gin.Context) {
	taskID := fmt.Sprintf("sync-ecs-%d", time.Now().UnixNano())
	var req struct {
		AccountIDs []string `json:"account_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	SyncAllResources("ecs", taskID, req.AccountIDs)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ECS同步任务已触发", "task_id": taskID})
}

// SyncCdnHandler 同步所有账号的 CDN 加速域名 POST /api/sync/cdn
func SyncCdnHandler(c *gin.Context) {
	taskID := fmt.Sprintf("sync-cdn-%d", time.Now().UnixNano())
	var req struct {
		AccountIDs []string `json:"account_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	SyncAllResources("cdn", taskID, req.AccountIDs)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "CDN同步任务已触发", "task_id": taskID})
}

func SyncAllHandler(c *gin.Context) {
	taskID := fmt.Sprintf("sync-all-%d", time.Now().UnixNano())
	var req struct {
		AccountIDs []string `json:"account_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	SyncAllResources("all", taskID, req.AccountIDs)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "全量同步任务已触发", "task_id": taskID})
}

// SyncProgressHandler SSE 进度推送 GET /api/sync/progress?task_id=xxx
func SyncProgressHandler(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 task_id"})
		return
	}

	ch := progressHub.subscribe(taskID)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	c.Stream(func(w io.Writer) bool {
		select {
		case msg, ok := <-ch:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				return false
			}
			// SSE data 中不能有裸换行，用 JSON 编码
			fmt.Fprintf(w, "data: %q\n\n", msg)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
