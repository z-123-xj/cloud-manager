package dns

// 修改DNS解析记录
// API: PUT /zones/{zone_id}/dns_records/{record_id}
// 文档: https://developers.cloudflare.com/api/operations/dns-records-for-a-zone-update-dns-record
//
// 注意：Cloudflare 的更新是**全量替换**（PUT），未提交的字段会被清空/重置，
//      因此必须先读取当前记录，把前端未提交的字段（proxied 等）原样带回再 PUT。
//      阿里云那套"标识字段变更走 Update、属性变更走删+加"的策略在 CF 不需要——
//      CF 无重复记录限制，直接 PUT 即可。

import (
	"fmt"
	"net/http"

	"cloud-manager-backend/internal/pkg/cloud/cloudflare"

	"github.com/gin-gonic/gin"
)

// ==================== 请求/响应结构体 ====================

// UpdateRecordRequest 修改DNS解析记录请求参数（字段与阿里云/腾讯云前端一致）
type UpdateRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID（必填）
	RR       string `json:"rr"`                           // 主机记录（新值），如 www、@、*
	Type     string `json:"type"`                         // 记录类型（新值）
	Value    string `json:"value"`                        // 记录值（新值）
	TTL      int64  `json:"ttl"`                          // 生存时间（新值）
	Priority int64  `json:"priority"`                     // MX 优先级（新值）
	Line     string `json:"line"`                         // Cloudflare 无此概念，忽略
	Weight   int    `json:"weight"`                       // Cloudflare 无此概念，忽略
	Remark   string `json:"remark"`                       // 备注（新值，空串=清空）
	Status   string `json:"status"`                       // Cloudflare 无启停概念，忽略
	Proxied  *bool  `json:"proxied"`                      // CDN 代理状态（新值）；nil=保持原值（前端无此字段时的兜底）
}

// UpdateRecordResponse 修改DNS解析记录响应
type UpdateRecordResponse struct {
	RecordID string `json:"record_id"` // 修改成功后的记录ID
}

// UpdateRecordHandler PUT /api/domains/:domain/dns
// 修改一条DNS解析记录（读-合并-全量PUT）
func UpdateRecordHandler(c *gin.Context) {
	var req UpdateRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	domainName := c.Param("domain")
	_, token := getCredentialFromContext(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "未获取到 Cloudflare API Token"})
		return
	}

	client := cloudflare.NewClient(token, "")
	zoneID, err := resolveZoneID(client, domainName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	// ===== 第一步：读取当前记录（PUT 全量替换，必须带回未提交字段） =====
	var current cfRecord
	if _, err := client.Get("/zones/"+zoneID+"/dns_records/"+req.RecordID, nil, &current); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询当前记录失败: " + err.Error()})
		return
	}

	// ===== 第二步：合并字段——前端未传/传空的沿用当前值 =====
	currentItem := current.toDnsRecordItem(domainName)
	rr := req.RR
	if rr == "" {
		rr = currentItem.RR
	}
	recType := req.Type
	if recType == "" {
		recType = current.Type
	}
	value := req.Value
	if value == "" {
		value = current.Content
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = current.TTL
	}
	// Priority：0 沿用当前值（注意 MX 优先级 0 合法，当前值同样按此处理，与阿里云逻辑一致）
	priority := req.Priority
	if priority == 0 {
		priority = currentItem.Priority
	}

	// ===== 校验：TTL / 结构化类型 / 备注长度 =====
	if !validCFTTL(ttl) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "TTL必须是 1（自动）或 60~86400 之间的整数"})
		return
	}
	if cfStructuredTypes[recType] {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": recType + " 为结构化记录类型，请直接在 Cloudflare 面板配置"})
		return
	}
	if len([]rune(req.Remark)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "备注最多 100 个字符"})
		return
	}

	// proxied：前端传了用新值（编辑表单的代理开关），没传保留原值（老客户端兜底）
	proxied := current.Proxied
	if req.Proxied != nil {
		proxied = *req.Proxied
	}

	// 组装 PUT body（全量替换）：proxied 按上述规则携带
	body := map[string]interface{}{
		"type":    recType,
		"name":    buildFQDN(rr, domainName),
		"content": value,
		"ttl":     ttl,
		"proxied": proxied,
		// remark 按"期望值"语义：前端表单预填当前备注，空串即清空
		"comment": req.Remark,
	}
	// priority 仅 MX 需要
	if recType == "MX" && priority > 0 {
		body["priority"] = priority
	}

	fmt.Printf("  [DNS修改] RecordId=%s, RR=%s, Type=%s, Value=%s, TTL=%d\n", req.RecordID, rr, recType, value, ttl)

	if _, err := client.Put("/zones/"+zoneID+"/dns_records/"+req.RecordID, body, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改DNS记录失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS] 修改记录成功: RecordId=%s\n", req.RecordID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "修改成功",
		"data": UpdateRecordResponse{RecordID: req.RecordID},
	})
}
