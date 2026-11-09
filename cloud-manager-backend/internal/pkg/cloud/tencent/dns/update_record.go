package dns

// 修改DNS解析记录（ModifyRecord API）
// 参考阿里云实现：先查询当前值，按需调用对应API
// https://cloud.tencent.com/document/product/1427/56157

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ==================== 请求结构体 ====================

// UpdateRecordRequest 修改DNS解析记录请求参数（与阿里云接口一致）
type UpdateRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID
	RR       string `json:"rr"`                           // 主机记录（新值）
	Type     string `json:"type"`                         // 记录类型（新值）
	Value    string `json:"value"`                        // 记录值（新值）
	TTL      int64  `json:"ttl"`                          // 生存时间（新值）
	Priority int64  `json:"priority"`                     // MX记录优先级（新值）
	Line     string `json:"line"`                         // 解析线路（新值）
	Weight   int    `json:"weight"`                       // 权重值（新值）
	Remark   string `json:"remark"`                       // 备注（新值）
	Status   string `json:"status"`                       // 状态（新值）
}

// ==================== 响应结构体 ====================

// UpdateRecordResponse 修改DNS解析记录响应
type UpdateRecordResponse struct {
	RecordID string `json:"record_id"`
}

// UpdateRecordHandler PUT /api/domains/:domain/dns
// 修改DNS解析记录：前端传新值，后端查询当前值后按需更新
func UpdateRecordHandler(c *gin.Context) {
	var req UpdateRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	domainName := c.Param("domain")
	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	// 从已有记录中查找当前记录（通过查询所有记录来获取当前值）
	currentList, _, err := describeRecordList(accessKeyID, accessKeySecret, domainName, 0, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询当前记录失败: " + err.Error()})
		return
	}

	// 找到当前记录
	var current *DnsRecordItem
	for i := range currentList {
		if currentList[i].RecordID == req.RecordID {
			current = &currentList[i]
			break
		}
	}
	if current == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到指定的DNS记录"})
		return
	}

	fmt.Printf("  [DNS修改-腾讯云] RecordId=%s\n", req.RecordID)
	fmt.Printf("    当前值: RR=%s, Type=%s, Value=%s, TTL=%d, Line=%s, Weight=%d, Priority=%d, Remark=%s, Status=%s\n",
		current.RR, current.Type, current.Value, current.TTL, current.Line, current.Weight, current.Priority, current.Remark, current.Status)
	fmt.Printf("    新值:   RR=%s, Type=%s, Value=%s, TTL=%d, Line=%s, Weight=%d, Priority=%d, Remark=%s, Status=%s\n",
		req.RR, req.Type, req.Value, req.TTL, req.Line, req.Weight, req.Priority, req.Remark, req.Status)

	// 设置默认值：前端未传的字段沿用当前值
	if req.RR == "" {
		req.RR = current.RR
	}
	if req.Type == "" {
		req.Type = current.Type
	}
	if req.Value == "" {
		req.Value = current.Value
	}
	if req.TTL == 0 {
		req.TTL = current.TTL
	}
	if req.Line == "" {
		req.Line = current.Line
	}
	if req.Line == "default" && current.Line != "" {
		// 保持当前线路
	}
	if req.Priority == 0 {
		req.Priority = current.Priority
	}

	// 判断是否有变更
	hasChange := req.RR != current.RR ||
		req.Type != current.Type ||
		req.Value != current.Value ||
		req.TTL != current.TTL ||
		req.Line != current.Line ||
		req.Weight != current.Weight ||
		req.Priority != current.Priority ||
		req.Remark != current.Remark

	statusChanged := req.Status != "" && req.Status != current.Status

	if !hasChange && !statusChanged {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "无变更", "data": UpdateRecordResponse{RecordID: req.RecordID}})
		return
	}

	if hasChange {
		// 调用 ModifyRecord API 更新记录主体
		client, err := createClient(accessKeyID, accessKeySecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建客户端失败: " + err.Error()})
			return
		}

		var recordID uint64
		if _, err := fmt.Sscanf(req.RecordID, "%d", &recordID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的记录ID"})
			return
		}

		request := dnspod.NewModifyRecordRequest()
		request.Domain = common.StringPtr(domainName)
		request.RecordId = common.Uint64Ptr(recordID)
		request.SubDomain = common.StringPtr(req.RR)
		request.RecordType = common.StringPtr(req.Type)
		request.RecordLine = common.StringPtr(lineToDNSPod(req.Line))
		request.Value = common.StringPtr(req.Value)
		request.TTL = common.Uint64Ptr(uint64(req.TTL))
		request.Weight = common.Uint64Ptr(uint64(req.Weight))

		if req.Type == "MX" && req.Priority > 0 {
			request.MX = common.Uint64Ptr(uint64(req.Priority))
		}
		if req.Remark != "" {
			request.Remark = common.StringPtr(req.Remark)
		}

		_, err = client.ModifyRecord(request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改DNS记录失败: " + err.Error()})
			return
		}

		fmt.Printf("  [DNS-腾讯云] 修改记录主体成功: RecordId=%s\n", req.RecordID)
	}

	// 如果状态发生了变化，单独设置状态
	if statusChanged {
		if err := setRecordStatus(accessKeyID, accessKeySecret, domainName, req.RecordID, req.Status); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改状态失败: " + err.Error()})
			return
		}
		fmt.Printf("  [DNS-腾讯云] 修改状态成功: RecordId=%s, Status=%s\n", req.RecordID, req.Status)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "修改成功",
		"data": UpdateRecordResponse{RecordID: req.RecordID},
	})
}
