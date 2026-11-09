package dns

// 添加DNS解析记录
// API: POST /zones/{zone_id}/dns_records
// 文档: https://developers.cloudflare.com/api/operations/dns-records-for-a-zone-create-dns-record

import (
	"fmt"
	"net/http"

	"cloud-manager-backend/internal/pkg/cloud/cloudflare"

	"github.com/gin-gonic/gin"
)

// ==================== 请求/响应结构体 ====================

// AddRecordRequest 添加DNS解析记录请求参数
// 字段与阿里云/腾讯云前端表单保持一致（line/weight 为阿里云概念，Cloudflare 忽略）
type AddRecordRequest struct {
	DomainName string `json:"domain_name" binding:"required"` // 域名，如 example.com
	RR         string `json:"rr" binding:"required"`          // 主机记录，如 www、@、*
	Type       string `json:"type" binding:"required"`        // 记录类型，如 A、CNAME、MX、TXT
	Value      string `json:"value" binding:"required"`       // 记录值，如 IP地址、域名
	TTL        int64  `json:"ttl"`                            // 生存时间（秒），默认600；1=自动
	Priority   int64  `json:"priority"`                       // MX 优先级，其他类型忽略
	Line       string `json:"line"`                           // Cloudflare 无解析线路概念，忽略
	Weight     int    `json:"weight"`                         // Cloudflare 无权重概念，忽略
	Remark     string `json:"remark"`                         // 备注（Cloudflare comment，最多100字符，不影响解析）
	Proxied    bool   `json:"proxied"`                        // 是否开启 Cloudflare CDN 代理（橙云），仅 A/AAAA/CNAME 有效
}

// AddRecordResponse 添加DNS解析记录响应
type AddRecordResponse struct {
	RecordID string `json:"record_id"` // 创建成功后返回的记录ID
}

// AddRecordHandler POST /api/domains/:domain/dns
// 添加一条DNS解析记录
func AddRecordHandler(c *gin.Context) {
	var req AddRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	// 以 URL 中的域名为准（与 req.DomainName 一致，URL 是权威来源）
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

	// TTL 默认 600（Cloudflare 合法值：1=自动 或 60~86400）
	if req.TTL == 0 {
		req.TTL = 600
	}
	if !validCFTTL(req.TTL) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "TTL必须是 1（自动）或 60~86400 之间的整数"})
		return
	}
	// 结构化记录类型（SRV/LOC/HTTPS/SVCB/NAPTR）需要 data 对象，平台暂不支持
	if cfStructuredTypes[req.Type] {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": req.Type + " 为结构化记录类型，请直接在 Cloudflare 面板配置"})
		return
	}
	// 备注（comment）Cloudflare 上限 100 字符
	if len([]rune(req.Remark)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "备注最多 100 个字符"})
		return
	}

	// 组装 Cloudflare 记录体
	// name 必须是 FQDN（RR + "." + 域名）；proxied 默认 false（仅 DNS，不走 CDN）
	body := map[string]interface{}{
		"type":    req.Type,
		"name":    buildFQDN(req.RR, domainName),
		"content": req.Value,
		"ttl":     req.TTL,
	}
	// proxied 仅对 A/AAAA/CNAME 有效，其他类型传了 Cloudflare 会报错，故按类型决定是否携带
	if cfProxiableType(req.Type) {
		body["proxied"] = req.Proxied
	}
	// Priority 仅 MX 需要（其他类型传了 Cloudflare 会报错）
	if req.Type == "MX" && req.Priority > 0 {
		body["priority"] = req.Priority
	}
	// 备注直接随创建提交（comment）
	if req.Remark != "" {
		body["comment"] = req.Remark
	}

	fmt.Printf("  [DNS添加] Zone=%s, RR=%s, Type=%s, Value=%s, TTL=%d\n", zoneID, req.RR, req.Type, req.Value, req.TTL)

	var created cfRecord
	if _, err := client.Post("/zones/"+zoneID+"/dns_records", body, &created); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "添加DNS记录失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS] 添加记录成功: %s → %s (RecordId: %s)\n", buildFQDN(req.RR, domainName), req.Value, created.ID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": AddRecordResponse{RecordID: created.ID},
	})
}
