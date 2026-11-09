package dns

// 查询DNS解析记录列表（DescribeRecordList API）
// https://cloud.tencent.com/document/product/1427/56166

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ==================== 返回给前端的完整结构体 ====================

// DnsRecordItem DNS解析记录条目（返回给前端，与阿里云格式一致）
type DnsRecordItem struct {
	RecordID   string `json:"record_id"`   // 解析记录ID
	RR         string `json:"rr"`          // 主机记录，如 www、@、*
	Type       string `json:"type"`        // 记录类型，如 A、CNAME、MX、TXT
	Value      string `json:"value"`       // 记录值
	TTL        int64  `json:"ttl"`         // 生存时间（秒）
	Priority   int64  `json:"priority"`    // MX记录优先级
	Line       string `json:"line"`        // 解析线路，如 default、internal、oversea
	Weight     int    `json:"weight"`      // 权重值（负载均衡轮询时为0）
	Status     string `json:"status"`      // 状态：ENABLE（启用）/ DISABLE（禁用）
	Locked     bool   `json:"locked"`      // 是否被锁定
	Remark     string `json:"remark"`      // 备注
	CreateTime string `json:"create_time"` // 创建时间，格式：2006-01-02 15:04:05
	UpdateTime string `json:"update_time"` // 更新时间，格式：2006-01-02 15:04:05
}

// DescribeRecordsHandler GET /api/domains/:domain/dns
// 查询指定域名下的所有DNS解析记录（支持分页遍历）
func DescribeRecordsHandler(c *gin.Context) {
	domainName := c.Param("domain")
	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	offset := uint64(0)
	limit := uint64(100)

	var allRecords []DnsRecordItem

	// 循环分页获取所有记录
	for {
		records, totalCount, err := describeRecordList(accessKeyID, accessKeySecret, domainName, offset, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询DNS记录失败: " + err.Error()})
			return
		}
		allRecords = append(allRecords, records...)

		// 判断是否还有下一页（DNSPod使用Offset分页）
		if uint64(len(allRecords)) >= totalCount || len(records) == 0 {
			break
		}
		offset += limit
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":  allRecords,
			"total": len(allRecords),
		},
	})
}

// describeRecordList 调用腾讯云DNSPod DescribeRecordList API 获取单页DNS记录
func describeRecordList(accessKeyID, accessKeySecret, domainName string, offset, limit uint64) ([]DnsRecordItem, uint64, error) {
	client, err := createClient(accessKeyID, accessKeySecret)
	if err != nil {
		return nil, 0, err
	}

	request := dnspod.NewDescribeRecordListRequest()
	request.Domain = common.StringPtr(domainName)
	request.Offset = common.Uint64Ptr(offset)
	request.Limit = common.Uint64Ptr(limit)

	// 带重试的 API 调用（应对偶发 DNS 解析失败等网络问题）
	var response *dnspod.DescribeRecordListResponse
	var lastErr error
	for i := 0; i < 3; i++ {
		response, lastErr = client.DescribeRecordList(request)
		if lastErr == nil {
			break
		}
		// 只对网络类错误重试，业务错误不重试
		if !strings.Contains(lastErr.Error(), "NetworkError") &&
			!strings.Contains(lastErr.Error(), "no such host") &&
			!strings.Contains(lastErr.Error(), "timeout") &&
			!strings.Contains(lastErr.Error(), "connection refused") {
			break
		}
		if i < 2 {
			time.Sleep(time.Duration(500*(i+1)) * time.Millisecond)
		}
	}
	if lastErr != nil {
		return nil, 0, fmt.Errorf("调用DescribeRecordList失败: %w", lastErr)
	}

	totalCount := uint64(safeUint64(response.Response.RecordCountInfo.TotalCount))

	var result []DnsRecordItem
	for _, r := range response.Response.RecordList {
		// DNSPod DescribeRecordList 不返回创建/更新时间，设为空
		result = append(result, DnsRecordItem{
			RecordID:   fmt.Sprintf("%d", safeUint64(r.RecordId)),
			RR:         safeString(r.Name),
			Type:       safeString(r.Type),
			Value:      safeString(r.Value),
			TTL:        safeUint64(r.TTL),
			Priority:   safeUint64(r.MX),
			Line:       lineFromDNSPod(safeString(r.Line)),
			Weight:     int(safeUint64(r.Weight)),
			Status:     safeString(r.Status),
			Locked:     false, // DNSPod 没有锁定字段
			Remark:     safeString(r.Remark),
			CreateTime: "",
			UpdateTime: safeString(r.UpdatedOn),
		})
	}

	fmt.Printf("  [DNS查询-腾讯云] 域名=%s, Offset=%d, 获取 %d 条记录, 总计 %d 条\n",
		domainName, offset, len(result), totalCount)

	return result, totalCount, nil
}
