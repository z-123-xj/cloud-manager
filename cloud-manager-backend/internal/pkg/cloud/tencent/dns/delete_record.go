package dns

// 删除DNS解析记录（DeleteRecord API）
// https://cloud.tencent.com/document/product/1427/56173

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ==================== 请求结构体 ====================

// DeleteRecordRequest 删除DNS解析记录请求参数（与阿里云接口一致）
type DeleteRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID
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
	domainName := c.Param("domain")

	fmt.Printf("  [DNS删除-腾讯云] RecordId=%s, Domain=%s\n", req.RecordID, domainName)

	client, err := createClient(accessKeyID, accessKeySecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建客户端失败: " + err.Error()})
		return
	}

	// 解析 recordID 为 uint64（DNSPod要求uint64类型）
	var recordID uint64
	if _, err := fmt.Sscanf(req.RecordID, "%d", &recordID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的记录ID"})
		return
	}

	request := dnspod.NewDeleteRecordRequest()
	request.Domain = common.StringPtr(domainName)
	request.RecordId = common.Uint64Ptr(recordID)

	_, err = client.DeleteRecord(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除DNS记录失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS-腾讯云] 删除记录成功: RecordId=%s\n", req.RecordID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}
