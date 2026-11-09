package dns

// 添加DNS解析记录（AddDomainRecord API）
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

// AddRecordRequest 添加DNS解析记录请求参数
type AddRecordRequest struct {
	DomainName string `json:"domain_name" binding:"required"` // 域名，如 example.com
	RR         string `json:"rr" binding:"required"`          // 主机记录，如 www、@、*
	Type       string `json:"type" binding:"required"`        // 记录类型，如 A、CNAME、MX、TXT
	Value      string `json:"value" binding:"required"`       // 记录值，如 IP地址、域名
	TTL        int64  `json:"ttl"`                            // 生存时间（秒），默认600
	Priority   int64  `json:"priority"`                       // MX记录优先级，非MX类型可不传
	Line       string `json:"line"`                           // 解析线路，默认 default
	Weight     int    `json:"weight"`                         // 权重值，默认1（轮询时为0）
	Remark     string `json:"remark"`                         // 备注信息
}

// ==================== 返回给前端的完整结构体 ====================

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
	if req.Line == "" {
		req.Line = "default"
	}
	if req.Weight == 0 {
		req.Weight = 1
	}

	fmt.Printf("  [DNS添加] DomainName=%s, RR=%s, Type=%s, Value=%s, TTL=%d, Line=%s, Weight=%d\n",
		req.DomainName, req.RR, req.Type, req.Value, req.TTL, req.Line, req.Weight)

	// 配置 API 请求参数
	params := CreateCommonAPIParams("AddDomainRecord", dnsVersion)

	// 设置查询参数
	queries := map[string]any{
		"DomainName": tea.String(req.DomainName),
		"RR":         tea.String(req.RR),
		"Type":       tea.String(req.Type),
		"Value":      tea.String(req.Value),
		"TTL":        tea.Int64(req.TTL),
		"Line":       tea.String(req.Line),
		"Weight":     tea.Int(req.Weight),
	}
	// Priority 仅在 > 0 时传递（MX记录需要）
	if req.Priority > 0 {
		queries["Priority"] = tea.Int64(req.Priority)
	}
	// 注意：AddDomainRecord API 不支持 Remark 参数，备注需要在添加成功后单独调用 UpdateDomainRecordRemark 设置

	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	// 调用阿里云通用API 添加记录
	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "添加DNS记录失败: " + err.Error()})
		return
	}

	// 解析返回结果，获取新创建记录的ID
	recordID := jsondata.Get("body.RecordId").String()
	fmt.Printf("  [DNS] 添加记录成功: %s.%s → %s (RecordId: %s)\n", req.RR, req.DomainName, req.Value, recordID)

	// 如果备注不为空，单独调用 UpdateDomainRecordRemark API 设置备注
	if req.Remark != "" {
		if err := updateRemark(accessKeyID, accessKeySecret, recordID, req.Remark); err != nil {
			// 备注设置失败仅打印警告，不影响主流程（记录已添加成功）
			fmt.Printf("  [DNS] 警告：添加记录后设置备注失败: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": AddRecordResponse{RecordID: recordID},
	})
}
