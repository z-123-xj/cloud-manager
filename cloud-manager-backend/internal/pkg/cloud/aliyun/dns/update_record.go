package dns

// 修改DNS解析记录
// 后端先查询当前记录实际值，与前端提交的新值比对，按需调用对应API：
//   - 主机记录/RR、记录类型/Type、记录值/Value、TTL、线路/Line、权重/Weight、优先级/Priority
//     → 调用 UpdateDomainRecord API
//   - 备注/Remark → 调用 UpdateDomainRecordRemark API
//   - 状态/Status → 调用 SetDomainRecordStatus API
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

// UpdateRecordRequest 修改DNS解析记录请求参数
// 前端只需传新值，后端自行查询原始值做比对
type UpdateRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"` // 解析记录ID（必填）
	RR       string `json:"rr"`                           // 主机记录（新值），如 www、@、*
	Type     string `json:"type"`                         // 记录类型（新值），如 A、CNAME、MX、TXT
	Value    string `json:"value"`                        // 记录值（新值）
	TTL      int64  `json:"ttl"`                          // 生存时间（新值），默认600
	Priority int64  `json:"priority"`                     // MX记录优先级（新值）
	Line     string `json:"line"`                         // 解析线路（新值），默认 default
	Weight   int    `json:"weight"`                       // 权重值（新值）
	Remark   string `json:"remark"`                       // 备注（新值）
	Status   string `json:"status"`                       // 状态（新值）：ENABLE / DISABLE
}

// ==================== 返回给前端的完整结构体 ====================

// UpdateRecordResponse 修改DNS解析记录响应
type UpdateRecordResponse struct {
	RecordID string `json:"record_id"` // 修改成功后的记录ID
}

// UpdateRecordHandler PUT /api/domains/:domain/dns
// 智能修改DNS解析记录：先查询当前值，再比对变更，按需调用对应API
func UpdateRecordHandler(c *gin.Context) {
	var req UpdateRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	domainName := c.Param("domain")
	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	// ===== 第一步：查询当前记录的实际值（从阿里云获取最新数据） =====
	current, err := describeSingleRecord(accessKeyID, accessKeySecret, req.RecordID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询当前记录失败: " + err.Error()})
		return
	}

	fmt.Printf("  [DNS修改] RecordId=%s\n", req.RecordID)
	fmt.Printf("    当前值: RR=%s, Type=%s, Value=%s, TTL=%d, Line=%s, Weight=%d, Priority=%d, Remark=%s, Status=%s\n",
		current.RR, current.Type, current.Value, current.TTL, current.Line, current.Weight, current.Priority, current.Remark, current.Status)
	fmt.Printf("    新值:   RR=%s, Type=%s, Value=%s, TTL=%d, Line=%s, Weight=%d, Priority=%d, Remark=%s, Status=%s\n",
		req.RR, req.Type, req.Value, req.TTL, req.Line, req.Weight, req.Priority, req.Remark, req.Status)

	// 设置默认值：前端未传的字段，沿用当前值
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
	// Weight 不设默认值回填：0 是合法的轮询模式，前端总是会传
	if req.Priority == 0 {
		req.Priority = current.Priority
	}

	// ===== 第二步：判断记录主体是否有变更 =====
	// 区分"标识字段"(RR/Type/Value)和"属性字段"(TTL/Line/Weight/Priority)
	identityChanged := req.RR != current.RR ||
		req.Type != current.Type ||
		req.Value != current.Value

	attrChanged := req.TTL != current.TTL ||
		req.Line != current.Line ||
		req.Weight != current.Weight ||
		req.Priority != current.Priority

	bodyChanged := identityChanged || attrChanged

	remarkChanged := req.Remark != current.Remark
	statusChanged := req.Status != "" && req.Status != current.Status

	recordID := req.RecordID

	if bodyChanged {
		if identityChanged {
			// 标识字段变了，直接调用 UpdateDomainRecord
			fmt.Printf("  [DNS] 检测到记录标识变更，调用 UpdateDomainRecord\n")
			recordParams := CreateCommonAPIParams("UpdateDomainRecord", dnsVersion)
			recordQueries := map[string]any{
				"RecordId": tea.String(req.RecordID),
				"RR":       tea.String(req.RR),
				"Type":     tea.String(req.Type),
				"Value":    tea.String(req.Value),
				"TTL":      tea.Int64(req.TTL),
				"Line":     tea.String(req.Line),
				"Weight":   tea.Int(req.Weight),
			}
			if req.Priority > 0 {
				recordQueries["Priority"] = tea.Int64(req.Priority)
			}

			recordRequest := &openapi.OpenApiRequest{
				Query: openapiutil.Query(recordQueries),
			}

			jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, recordParams, recordRequest)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改DNS记录主体失败: " + err.Error()})
				return
			}

			recordID = jsondata.Get("body.RecordId").String()
			fmt.Printf("  [DNS] 修改记录主体成功: RecordId=%s\n", recordID)
		} else {
			// 仅属性字段变了（TTL/Line/Weight/Priority），标识未变
			// 阿里云 UpdateDomainRecord 在 RR/Type/Value 不变时可能报 DomainRecordDuplicate
			// 采用"先删除旧记录，再添加新记录"的策略绕过此限制
			fmt.Printf("  [DNS] 仅属性字段变更（标识未变），采用删除+添加策略\n")

			// 1) 先删除旧记录
			if err := deleteSingleRecord(accessKeyID, accessKeySecret, req.RecordID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除旧记录失败: " + err.Error()})
				return
			}
			fmt.Printf("  [DNS] 已删除旧记录: RecordId=%s\n", req.RecordID)

			// 2) 再添加新记录
			newRecordID, err := addSingleRecord(accessKeyID, accessKeySecret, domainName, req.RR, req.Type, req.Value, req.TTL, req.Line, req.Weight, req.Priority)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "添加新记录失败: " + err.Error()})
				return
			}
			recordID = newRecordID
			fmt.Printf("  [DNS] 新记录添加成功: RecordId=%s\n", recordID)
		}
	} else {
		fmt.Printf("  [DNS] 记录主体无变更，跳过 UpdateDomainRecord\n")
	}

	// ===== 第三步：如果备注发生了变化，调用 UpdateDomainRecordRemark =====
	if remarkChanged {
		fmt.Printf("  [DNS] 检测到备注变更 [%s] → [%s]，调用 UpdateDomainRecordRemark\n", current.Remark, req.Remark)
		if err := updateRemark(accessKeyID, accessKeySecret, req.RecordID, req.Remark); err != nil {
			fmt.Printf("  [DNS] 修改备注失败: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改备注失败: " + err.Error()})
			return
		}
		fmt.Printf("  [DNS] 修改备注成功: RecordId=%s, Remark=%s\n", req.RecordID, req.Remark)
	}

	// ===== 第四步：如果状态发生了变化，调用 SetDomainRecordStatus =====
	if statusChanged {
		fmt.Printf("  [DNS] 检测到状态变更 [%s] → [%s]，调用 SetDomainRecordStatus\n", current.Status, req.Status)
		if err := setRecordStatus(accessKeyID, accessKeySecret, req.RecordID, req.Status); err != nil {
			fmt.Printf("  [DNS] 修改状态失败: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改状态失败: " + err.Error()})
			return
		}
		fmt.Printf("  [DNS] 修改状态成功: RecordId=%s, Status=%s\n", req.RecordID, req.Status)
	}

	// 如果没有任何变更
	if !bodyChanged && !remarkChanged && !statusChanged {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "无变更", "data": UpdateRecordResponse{RecordID: recordID}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "修改成功",
		"data": UpdateRecordResponse{RecordID: recordID},
	})
}

// describeSingleRecord 查询单条DNS解析记录的详细信息
// 调用阿里云 DescribeDomainRecordInfo API
func describeSingleRecord(accessKeyID, accessKeySecret, recordID string) (*DnsRecordItem, error) {
	params := CreateCommonAPIParams("DescribeDomainRecordInfo", dnsVersion)

	queries := map[string]any{
		"RecordId": tea.String(recordID),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		return nil, fmt.Errorf("调用 DescribeDomainRecordInfo 失败: %w", err)
	}

	record := &DnsRecordItem{
		RecordID: jsondata.Get("body.RecordId").String(),
		RR:       jsondata.Get("body.RR").String(),
		Type:     jsondata.Get("body.Type").String(),
		Value:    jsondata.Get("body.Value").String(),
		TTL:      jsondata.Get("body.TTL").Int(),
		Priority: jsondata.Get("body.Priority").Int(),
		Line:     jsondata.Get("body.Line").String(),
		Weight:   int(jsondata.Get("body.Weight").Int()),
		Status:   jsondata.Get("body.Status").String(),
		Remark:   jsondata.Get("body.Remark").String(),
	}

	fmt.Printf("  [DNS查询单条] RecordId=%s, RR=%s, Type=%s, Value=%s\n", record.RecordID, record.RR, record.Type, record.Value)
	return record, nil
}

// deleteSingleRecord 删除单条DNS记录（内部辅助函数）
func deleteSingleRecord(accessKeyID, accessKeySecret, recordID string) error {
	params := CreateCommonAPIParams("DeleteDomainRecord", dnsVersion)
	queries := map[string]any{
		"RecordId": tea.String(recordID),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}
	_, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		return fmt.Errorf("调用 DeleteDomainRecord 失败: %w", err)
	}
	return nil
}

// addSingleRecord 添加单条DNS记录（内部辅助函数），返回新记录ID
func addSingleRecord(accessKeyID, accessKeySecret, domainName, rr, recordType, value string, ttl int64, line string, weight int, priority int64) (string, error) {
	params := CreateCommonAPIParams("AddDomainRecord", dnsVersion)
	queries := map[string]any{
		"DomainName": tea.String(domainName),
		"RR":         tea.String(rr),
		"Type":       tea.String(recordType),
		"Value":      tea.String(value),
		"TTL":        tea.Int64(ttl),
		"Line":       tea.String(line),
		"Weight":     tea.Int(weight),
	}
	if priority > 0 {
		queries["Priority"] = tea.Int64(priority)
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}
	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		return "", fmt.Errorf("调用 AddDomainRecord 失败: %w", err)
	}
	return jsondata.Get("body.RecordId").String(), nil
}
