package dns

// 删除DNS解析记录
// API: DELETE /zones/{zone_id}/dns_records/{record_id}
// 文档: https://developers.cloudflare.com/api/operations/dns-records-for-a-zone-delete-dns-record

import (
	"fmt"
	"net/http"

	"cloud-manager-backend/internal/pkg/cloud/cloudflare"

	"github.com/gin-gonic/gin"
)

// ==================== 请求结构体 ====================

// DeleteRecordRequest 删除DNS解析记录请求参数
type DeleteRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID（必填）
}

// DeleteRecordHandler DELETE /api/domains/:domain/dns
// 删除一条DNS解析记录
func DeleteRecordHandler(c *gin.Context) {
	var req DeleteRecordRequest
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

	fmt.Printf("  [DNS删除] Zone=%s, RecordId=%s\n", zoneID, req.RecordID)

	if _, err := client.Delete("/zones/"+zoneID+"/dns_records/"+req.RecordID, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除DNS记录失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS] 删除记录成功: RecordId=%s\n", req.RecordID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}
