package dns

// 设置DNS解析记录启用/禁用状态（ModifyRecordStatus API）
// https://cloud.tencent.com/document/product/1427/56166

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ==================== 请求结构体 ====================

// SetStatusRequest 设置DNS解析记录状态请求参数（与阿里云接口一致）
type SetStatusRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID
	Status   string `json:"status" binding:"required"`    // 状态：Enable / Disable
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
	domainName := c.Param("domain")

	if err := setRecordStatus(accessKeyID, accessKeySecret, domainName, req.RecordID, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设置DNS记录状态失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS-腾讯云] 设置状态成功: RecordId=%s, Status=%s\n", req.RecordID, req.Status)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "操作成功",
	})
}

// setRecordStatus 调用腾讯云 ModifyRecordStatus API 启用/禁用记录
func setRecordStatus(accessKeyID, accessKeySecret, domainName, recordID, status string) error {
	fmt.Printf("  [DNS设置状态-腾讯云] RecordId=%s, Status=%s\n", recordID, status)

	client, err := createClient(accessKeyID, accessKeySecret)
	if err != nil {
		return fmt.Errorf("创建客户端失败: %w", err)
	}

	var recordIDUint uint64
	if _, err := fmt.Sscanf(recordID, "%d", &recordIDUint); err != nil {
		return fmt.Errorf("无效的记录ID: %s", recordID)
	}

	request := dnspod.NewModifyRecordStatusRequest()
	request.Domain = common.StringPtr(domainName)
	request.RecordId = common.Uint64Ptr(recordIDUint)
	request.Status = common.StringPtr(strings.ToUpper(status)) // DNSPod API 要求大写 "ENABLE"/"DISABLE"

	_, err = client.ModifyRecordStatus(request)
	if err != nil {
		return fmt.Errorf("调用ModifyRecordStatus失败: %w", err)
	}

	return nil
}
