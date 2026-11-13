package bastion

// ============================================================================
// 批量命令/脚本执行
//
// 复用堡垒机的 ECS 资产、用户组授权与 SSH 凭证；提交时逐台完成授权校验并
// 组装连接信息，后台按并发上限连接执行，每台 stdout/stderr/退出码落库可审计。
// 进度通过前端轮询获取（不使用 SSE：server.write_timeout 会掐断长任务）。
// ============================================================================

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud-manager-backend/internal/config"
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// batchCancelStore 运行中任务的取消句柄：taskID(uint64) -> context.CancelFunc
var batchCancelStore sync.Map

type batchRunReq struct {
	Title        string `json:"title"`
	EcsIDs       []int  `json:"ecs_ids"`
	CredentialID int    `json:"credential_id"`
	Network      string `json:"network"`
	Script       string `json:"script"`
	Shell        string `json:"shell"`
	Concurrency  int               `json:"concurrency"`
	TimeoutSec   int               `json:"timeout_sec"`
	ScriptID     string            `json:"script_id"` // 脚本库模式：服务端按 ID 读文件并渲染
	Params       map[string]string `json:"params"`    // 模板参数值，服务端校验后替换 {{KEY}}
}

type batchTarget struct {
	resultID uint64
	spec     *logSSHSpec
}

func batchOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
}

func batchFail(c *gin.Context, format string, args ...interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 400, "msg": fmt.Sprintf(format, args...)})
}

func batchIdentity(c *gin.Context) (int, string) {
	uidStr, _ := c.Get("user_id")
	uid, _ := strconv.Atoi(fmt.Sprint(uidStr))
	role, _ := c.Get("role")
	roleStr, _ := role.(string)
	return uid, roleStr
}

// GetBatchOptions GET /api/bastion/batch/options —— 前端表单的上限与可选项
func GetBatchOptions(c *gin.Context) {
	cfg := config.GetBatchConfig()
	batchOK(c, gin.H{
		"shells":           supportedShells(),
		"networks":         []string{"public", "eip", "private"},
		"max_concurrency":  cfg.MaxConcurrency,
		"max_timeout_sec":  cfg.MaxTimeoutSec,
		"max_script_bytes": cfg.MaxScriptBytes,
		"max_output_bytes": cfg.MaxOutputBytes,
		"max_assets":       cfg.MaxAssets,
	})
}

// RunBatchTask POST /api/bastion/batch/tasks —— 校验并创建任务，立即返回 task_id
func RunBatchTask(c *gin.Context) {
	cfg := config.GetBatchConfig()
	var req batchRunReq
	if err := c.ShouldBindJSON(&req); err != nil {
		batchFail(c, "请求参数错误: %s", err.Error())
		return
	}

	// 脚本库模式：正文由服务端从脚本目录读取并渲染，前端只传 script_id + params，
	// 避免前端绕过版本与参数校验直接提交任意脚本；手写模式仍使用 req.Script。
	if strings.TrimSpace(req.ScriptID) != "" {
		sf, err := loadScript(req.ScriptID)
		if err != nil {
			batchFail(c, "%s", err.Error())
			return
		}
		rendered, err := renderScript(sf, req.Params)
		if err != nil {
			batchFail(c, "%s", err.Error())
			return
		}
		req.Script = rendered
		if sf.Meta.Shell != "" {
			req.Shell = sf.Meta.Shell
		}
		if strings.TrimSpace(req.Title) == "" {
			req.Title = sf.Meta.Name
		}
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		req.Title = "批量执行 " + time.Now().Format("2006-01-02 15:04:05")
	}
	if len(req.Title) > 200 {
		batchFail(c, "任务标题不能超过 200 字符")
		return
	}
	// 去重 + 过滤非法 ID
	seen := make(map[int]bool)
	ids := make([]int, 0, len(req.EcsIDs))
	for _, id := range req.EcsIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		batchFail(c, "请至少选择一台目标主机")
		return
	}
	if len(ids) > cfg.MaxAssets {
		batchFail(c, "单次最多执行 %d 台，当前选择 %d 台", cfg.MaxAssets, len(ids))
		return
	}
	if strings.TrimSpace(req.Script) == "" {
		batchFail(c, "命令或脚本内容不能为空")
		return
	}
	if len(req.Script) > cfg.MaxScriptBytes {
		batchFail(c, "脚本内容超过 %d 字节上限", cfg.MaxScriptBytes)
		return
	}
	if req.Shell == "" {
		req.Shell = "bash"
	}
	command, err := shellCommand(req.Shell)
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	if req.Network == "" {
		req.Network = "public"
	}
	if req.Network != "public" && req.Network != "eip" && req.Network != "private" {
		batchFail(c, "不支持的连接网络: %s", req.Network)
		return
	}
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	if concurrency > cfg.MaxConcurrency {
		concurrency = cfg.MaxConcurrency
	}
	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	if timeoutSec > cfg.MaxTimeoutSec {
		timeoutSec = cfg.MaxTimeoutSec
	}

	uid, role := batchIdentity(c)

	// 非管理员显式指定凭证时，校验该凭证确实授权给了所在用户组
	if role != "admin" && req.CredentialID > 0 {
		var groupIDs []uint
		models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
		var cnt int64
		if len(groupIDs) > 0 {
			models.DB.Model(&models.BastionGroupCredential{}).
				Where("credential_id = ? AND group_id IN ?", req.CredentialID, groupIDs).Count(&cnt)
		}
		if cnt == 0 {
			batchFail(c, "无权使用该凭证")
			return
		}
	}

	createdName := fmt.Sprintf("user#%d", uid)
	var user models.User
	if err := models.DB.Select("id", "username", "email").First(&user, uid).Error; err == nil {
		if user.Username != "" {
			createdName = user.Username
		} else if user.Email != "" {
			createdName = user.Email
		}
	}
	credName := "自动选择"
	if req.CredentialID > 0 {
		var cred models.ServerCredential
		if err := models.DB.Select("id", "name").First(&cred, req.CredentialID).Error; err == nil {
			credName = cred.Name
		}
	}

	// 提交前逐台校验授权并组装连接信息；任何一台不合法则整体拒绝，不留半截任务
	targets := make([]batchTarget, 0, len(ids))
	results := make([]models.BatchTaskResult, 0, len(ids))
	for _, id := range ids {
		spec, err := resolveSSHSpec(uid, role, id, req.Network, req.CredentialID)
		if err != nil {
			batchFail(c, "资产 %d 校验失败: %s", id, err.Error())
			return
		}
		results = append(results, models.BatchTaskResult{
			EcsID:     spec.EcsID,
			AssetName: spec.AssetName,
			Host:      spec.Host,
			Port:      spec.Port,
			Status:    models.BatchResultPending,
		})
		targets = append(targets, batchTarget{spec: spec})
	}

	task := models.BatchTask{
		Title:          req.Title,
		Script:         req.Script,
		Shell:          req.Shell,
		Network:        req.Network,
		CredentialID:   uint(req.CredentialID),
		CredentialName: credName,
		Concurrency:    concurrency,
		TimeoutSec:     timeoutSec,
		Status:         models.BatchTaskRunning,
		Total:          len(targets),
		CreatedBy:      uint(uid),
		CreatedName:    createdName,
		ClientIP:       c.ClientIP(),
		StartedAt:      time.Now(),
	}
	if err := models.DB.Create(&task).Error; err != nil {
		logger.Error("batch", "创建任务失败", "error", err.Error())
		batchFail(c, "创建任务失败: %s", err.Error())
		return
	}
	for i := range results {
		results[i].TaskID = task.ID
	}
	if err := models.DB.Create(&results).Error; err != nil {
		models.DB.Delete(&models.BatchTask{}, task.ID)
		logger.Error("batch", "创建任务结果失败", "task_id", task.ID, "error", err.Error())
		batchFail(c, "创建任务失败: %s", err.Error())
		return
	}
	for i := range targets {
		targets[i].resultID = results[i].ID
	}

	ctx, cancel := context.WithCancel(context.Background())
	batchCancelStore.Store(task.ID, cancel)
	logger.Info("batch", "任务已提交", "task_id", task.ID, "user", createdName,
		"assets", len(targets), "shell", req.Shell, "concurrency", concurrency, "timeout_sec", timeoutSec)

	go runBatchTask(ctx, cancel, task.ID, targets, req.Script, command, concurrency,
		time.Duration(timeoutSec)*time.Second, cfg)

	batchOK(c, gin.H{"task_id": task.ID})
}

// runBatchTask 后台并发执行，结束后汇总状态
func runBatchTask(ctx context.Context, cancel context.CancelFunc, taskID uint64,
	targets []batchTarget, script, command string, concurrency int, timeout time.Duration, cfg config.BatchConfig) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("batch", "任务执行异常", "task_id", taskID, "panic", fmt.Sprint(r))
		}
		cancel()
		batchCancelStore.Delete(taskID)
	}()

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(target batchTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			executeOne(ctx, taskID, target, script, command, timeout, cfg)
		}(t)
	}
	wg.Wait()
	// 取消时 executeOne 会保持 pending 直接返回，无论任务是否已全部派发都统一收尾
	markPendingCanceled(taskID, "任务已取消")
	finalizeBatchTask(taskID)
}

func executeOne(ctx context.Context, taskID uint64, t batchTarget, script, command string,
	timeout time.Duration, cfg config.BatchConfig) {
	if ctx.Err() != nil {
		return // 保持 pending，由 markPendingCanceled 统一收尾
	}
	start := time.Now()
	models.DB.Model(&models.BatchTaskResult{}).Where("id = ?", t.resultID).
		Updates(map[string]interface{}{
			"status":     models.BatchResultRunning,
			"started_at": start,
		})

	var stdout, stderr string
	var runErr error
	client, err := t.spec.dial()
	if err != nil {
		runErr = fmt.Errorf("SSH 连接失败: %w", err)
	} else {
		stdout, stderr, runErr = runRemoteCtx(ctx, client, script, command, timeout)
		client.Close()
	}
	finish := time.Now()

	status := models.BatchResultSuccess
	errMsg := ""
	exitCode := exitCodeOf(runErr)
	if ctx.Err() != nil {
		status = models.BatchResultCanceled
		errMsg = "任务已取消"
		exitCode = nil
	} else if runErr != nil {
		status = models.BatchResultFailed
		errMsg = truncateBytes(runErr.Error(), 900)
	}

	if err := models.DB.Model(&models.BatchTaskResult{}).Where("id = ?", t.resultID).
		Updates(map[string]interface{}{
			"status":      status,
			"exit_code":   exitCode,
			"stdout":      truncateBytes(stdout, cfg.MaxOutputBytes),
			"stderr":      truncateBytes(stderr, cfg.MaxOutputBytes),
			"err_msg":     errMsg,
			"duration_ms": finish.Sub(start).Milliseconds(),
			"finished_at": finish,
		}).Error; err != nil {
		logger.Error("batch", "写入执行结果失败", "task_id", taskID,
			"result_id", t.resultID, "error", err.Error())
	} else {
		logger.Info("batch", "主机执行完成", "task_id", taskID, "host", t.spec.Host,
			"status", status, "duration_ms", finish.Sub(start).Milliseconds())
	}
}

func markPendingCanceled(taskID uint64, reason string) {
	models.DB.Model(&models.BatchTaskResult{}).
		Where("task_id = ? AND status = ?", taskID, models.BatchResultPending).
		Updates(map[string]interface{}{
			"status":      models.BatchResultCanceled,
			"err_msg":     reason,
			"finished_at": time.Now(),
		})
}

// taskStats 从结果表汇总任务状态
func taskStats(taskID uint64) (status string, succeeded, failed int) {
	var results []models.BatchTaskResult
	models.DB.Select("status").Where("task_id = ?", taskID).Find(&results)
	canceled := 0
	for _, r := range results {
		switch r.Status {
		case models.BatchResultSuccess:
			succeeded++
		case models.BatchResultCanceled:
			canceled++
		default:
			failed++
		}
	}
	switch {
	case canceled > 0:
		status = models.BatchTaskCanceled
	case failed > 0 && succeeded > 0:
		status = models.BatchTaskPartial
	case failed > 0:
		status = models.BatchTaskFailed
	default:
		status = models.BatchTaskSucceeded
	}
	return
}

func finalizeBatchTask(taskID uint64) {
	status, succeeded, failed := taskStats(taskID)
	if err := models.DB.Model(&models.BatchTask{}).Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      status,
			"succeeded":   succeeded,
			"failed":      failed,
			"finished_at": time.Now(),
		}).Error; err != nil {
		logger.Error("batch", "汇总任务状态失败", "task_id", taskID, "error", err.Error())
		return
	}
	logger.Info("batch", "任务结束", "task_id", taskID, "status", status,
		"succeeded", succeeded, "failed", failed)
}

// CancelBatchTask POST /api/bastion/batch/tasks/:id/cancel
func CancelBatchTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		batchFail(c, "任务 ID 无效")
		return
	}
	uid, role := batchIdentity(c)
	var task models.BatchTask
	if err := models.DB.First(&task, id).Error; err != nil {
		batchFail(c, "任务不存在")
		return
	}
	if role != "admin" && task.CreatedBy != uint(uid) {
		batchFail(c, "无权操作该任务")
		return
	}
	if task.Status != models.BatchTaskRunning {
		batchFail(c, "任务已结束，无需取消")
		return
	}

	v, ok := batchCancelStore.Load(id)
	if !ok {
		// 进程重启后内存句柄丢失，直接把僵尸任务收尾
		markPendingCanceled(id, "服务重启，任务中断")
		models.DB.Model(&models.BatchTask{}).Where("id = ?", id).
			Updates(map[string]interface{}{
				"status":      models.BatchTaskCanceled,
				"err_msg":     "服务重启，任务中断",
				"finished_at": time.Now(),
			})
		batchOK(c, gin.H{"task_id": id})
		return
	}
	v.(context.CancelFunc)()
	logger.Info("batch", "任务已请求取消", "task_id", id, "user", task.CreatedName)
	batchOK(c, gin.H{"task_id": id})
}

// recoverStaleBatchTasks 服务重启后，把内存中已不存在执行协程的 running 任务收尾
func recoverStaleBatchTasks() {
	var tasks []models.BatchTask
	models.DB.Select("id").Where("status = ?", models.BatchTaskRunning).Find(&tasks)
	for _, t := range tasks {
		if _, ok := batchCancelStore.Load(t.ID); ok {
			continue
		}
		logger.Warn("batch", "发现服务重启后中断的任务，标记为失败", "task_id", t.ID)
		models.DB.Model(&models.BatchTaskResult{}).
			Where("task_id = ? AND status IN ?", t.ID,
				[]string{models.BatchResultPending, models.BatchResultRunning}).
			Updates(map[string]interface{}{
				"status":      models.BatchResultCanceled,
				"err_msg":     "服务重启，任务中断",
				"finished_at": time.Now(),
			})
		_, succeeded, failed := taskStats(t.ID)
		models.DB.Model(&models.BatchTask{}).Where("id = ?", t.ID).
			Updates(map[string]interface{}{
				"status":      models.BatchTaskFailed,
				"succeeded":   succeeded,
				"failed":      failed,
				"err_msg":     "服务重启，任务中断",
				"finished_at": time.Now(),
			})
	}
}

// ListBatchTasks GET /api/bastion/batch/tasks —— 历史任务（列表不返回脚本正文）
func ListBatchTasks(c *gin.Context) {
	recoverStaleBatchTasks()
	uid, role := batchIdentity(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	countQ := models.DB.Model(&models.BatchTask{})
	listQ := models.DB.Model(&models.BatchTask{})
	if role != "admin" {
		countQ = countQ.Where("created_by = ?", uid)
		listQ = listQ.Where("created_by = ?", uid)
	}
	var total int64
	countQ.Count(&total)
	var list []models.BatchTask
	listQ.Select("id", "title", "shell", "network", "credential_name", "concurrency",
		"timeout_sec", "status", "total", "succeeded", "failed", "created_by", "created_name",
		"client_ip", "err_msg", "started_at", "finished_at", "created_at").
		Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list)
	if list == nil {
		list = []models.BatchTask{}
	}
	batchOK(c, gin.H{"list": list, "total": total, "page": page, "size": size})
}

// GetBatchTask GET /api/bastion/batch/tasks/:id —— 任务详情 + 每台结果
func GetBatchTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		batchFail(c, "任务 ID 无效")
		return
	}
	uid, role := batchIdentity(c)
	var task models.BatchTask
	if err := models.DB.First(&task, id).Error; err != nil {
		batchFail(c, "任务不存在")
		return
	}
	if role != "admin" && task.CreatedBy != uint(uid) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权查看该任务"})
		return
	}
	var results []models.BatchTaskResult
	models.DB.Where("task_id = ?", id).Order("id ASC").Find(&results)
	if results == nil {
		results = []models.BatchTaskResult{}
	}
	batchOK(c, gin.H{"task": task, "results": results})
}
