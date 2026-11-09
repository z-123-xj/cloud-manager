package dns

// 设置DNS解析记录启用/禁用状态（SetDomainRecordStatus API）
// https://api.aliyun.com/product/Alidns

import (
	"fmt"
	"net/http"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-gonic/gin"
)

// ==================== 请求结构体 ====================

// SetStatusRequest 设置DNS解析记录状态请求参数
type SetStatusRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID（必填）
	Status   string `json:"status" binding:"required"`    // 状态：Enable（启用）/ Disable（禁用）
}

// SetStatusHandler PUT /api/domains/:domain/dns/status
// 启用或禁用一条DNS解析记录
func SetStatusHandler(c *gin.Context) {
	var req SetStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	if err := setRecordStatus(accessKeyID, accessKeySecret, req.RecordID, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设置DNS记录状态失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS] 设置状态成功: RecordId=%s, Status=%s\n", req.RecordID, req.Status)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "操作成功",
	})
}

// setRecordStatus 调用阿里云 SetDomainRecordStatus API 启用/禁用记录
// 此函数供 SetStatusHandler 和 UpdateRecordHandler 内部复用
func setRecordStatus(accessKeyID, accessKeySecret, recordID, status string) error {
	fmt.Printf("  [DNS设置状态] RecordId=%s, Status=%s\n", recordID, status)

	// 配置 API 请求参数
	params := CreateCommonAPIParams("SetDomainRecordStatus", dnsVersion)

	// 设置查询参数
	queries := map[string]any{
		"RecordId": tea.String(recordID),
		"Status":   tea.String(status),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	// 调用阿里云通用API
	_, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		return fmt.Errorf("调用SetDomainRecordStatus失败: %w", err)
	}

	return nil
}
