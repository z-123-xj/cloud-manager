package dns

// 修改DNS解析记录备注（UpdateDomainRecordRemark API）
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

// UpdateRemarkRequest 修改DNS解析记录备注请求参数
type UpdateRemarkRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID（必填）
	Remark   string `json:"remark"`                       // 备注内容
}

// UpdateRemarkHandler PUT /api/domains/:domain/dns/remark
// 单独修改一条DNS解析记录的备注
func UpdateRemarkHandler(c *gin.Context) {
	var req UpdateRemarkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	if err := updateRemark(accessKeyID, accessKeySecret, req.RecordID, req.Remark); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改DNS记录备注失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS] 修改备注成功: RecordId=%s\n", req.RecordID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "修改备注成功",
	})
}

// updateRemark 调用阿里云 UpdateDomainRecordRemark API 修改备注
// 此函数供 AddRecordHandler 和 UpdateRecordHandler 内部复用
func updateRemark(accessKeyID, accessKeySecret, recordID, remark string) error {
	fmt.Printf("  [DNS修改备注] RecordId=%s, Remark=%s\n", recordID, remark)

	// 配置 API 请求参数
	params := CreateCommonAPIParams("UpdateDomainRecordRemark", dnsVersion)

	// 设置查询参数
	queries := map[string]any{
		"RecordId": tea.String(recordID),
		"Remark":   tea.String(remark),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	// 调用阿里云通用API
	_, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		return fmt.Errorf("调用UpdateDomainRecordRemark失败: %w", err)
	}

	return nil
}
