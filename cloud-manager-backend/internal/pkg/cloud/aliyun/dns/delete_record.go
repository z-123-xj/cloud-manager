package dns

// 删除DNS解析记录（DeleteDomainRecord API）
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

	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	fmt.Printf("  [DNS删除] RecordId=%s\n", req.RecordID)

	// 配置 API 请求参数
	params := CreateCommonAPIParams("DeleteDomainRecord", dnsVersion)

	// 设置查询参数
	queries := map[string]any{
		"RecordId": tea.String(req.RecordID),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	// 调用阿里云通用API
	_, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除DNS记录失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS] 删除记录成功: RecordId=%s\n", req.RecordID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}
