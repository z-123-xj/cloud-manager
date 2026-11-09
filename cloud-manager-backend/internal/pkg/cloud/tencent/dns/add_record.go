package dns

// 添加DNS解析记录（CreateRecord API）
// https://cloud.tencent.com/document/product/1427/56180

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ==================== 请求结构体 ====================

// AddRecordRequest 添加DNS解析记录请求参数（与阿里云接口一致）
type AddRecordRequest struct {
	DomainName string `json:"domain_name" binding:"required"` // 域名
	RR         string `json:"rr" binding:"required"`          // 主机记录，如 www、@、*
	Type       string `json:"type" binding:"required"`        // 记录类型
	Value      string `json:"value" binding:"required"`       // 记录值
	TTL        int64  `json:"ttl"`                            // 生存时间（秒），默认600
	Priority   int64  `json:"priority"`                       // MX记录优先级
	Line       string `json:"line"`                           // 解析线路，默认 default
	Weight     int    `json:"weight"`                         // 权重值
	Remark     string `json:"remark"`                         // 备注
}

// ==================== 响应结构体 ====================

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

	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	// 设置默认值
	if req.TTL == 0 {
		req.TTL = 600
	}
	if req.Line == "" || req.Line == "default" {
		req.Line = "default"
	}
	if req.Weight == 0 {
		req.Weight = 1
	}

	fmt.Printf("  [DNS添加-腾讯云] DomainName=%s, RR=%s, Type=%s, Value=%s, TTL=%d, Line=%s, Weight=%d\n",
		req.DomainName, req.RR, req.Type, req.Value, req.TTL, req.Line, req.Weight)

	client, err := createClient(accessKeyID, accessKeySecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建客户端失败: " + err.Error()})
		return
	}

	request := dnspod.NewCreateRecordRequest()
	request.Domain = common.StringPtr(req.DomainName)
	request.SubDomain = common.StringPtr(req.RR)
	request.RecordType = common.StringPtr(req.Type)
	request.RecordLine = common.StringPtr(lineToDNSPod(req.Line))
	request.Value = common.StringPtr(req.Value)
	request.TTL = common.Uint64Ptr(uint64(req.TTL))
	request.Weight = common.Uint64Ptr(uint64(req.Weight))

	// MX记录需要设置优先级
	if req.Type == "MX" && req.Priority > 0 {
		request.MX = common.Uint64Ptr(uint64(req.Priority))
	}

	// 备注信息
	if req.Remark != "" {
		request.Remark = common.StringPtr(req.Remark)
	}

	response, err := client.CreateRecord(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "添加DNS记录失败: " + err.Error()})
		return
	}

	recordID := fmt.Sprintf("%d", *response.Response.RecordId)
	fmt.Printf("  [DNS-腾讯云] 添加记录成功: %s.%s → %s (RecordId: %s)\n", req.RR, req.DomainName, req.Value, recordID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": AddRecordResponse{RecordID: recordID},
	})
}
