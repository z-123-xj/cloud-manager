package dns

// 查询DNS解析记录列表（DescribeDomainRecords API）
// https://api.aliyun.com/product/Alidns

import (
	"cloud-manager-backend/pkg/utils"
	"fmt"
	"net/http"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gin-gonic/gin"
)

// ==================== 返回给前端的完整结构体 ====================

// DnsRecordItem DNS解析记录条目（返回给前端）
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

	pageNumber := 1
	pageSize := 100

	var allRecords []DnsRecordItem

	// 循环分页获取所有记录
	for {
		records, totalCount, err := describeDomainRecords(accessKeyID, accessKeySecret, domainName, pageNumber, pageSize)
		// 打印原始返回结果
		fmt.Println(utils.PrintJsonData(records))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询DNS记录失败: " + err.Error()})
			return
		}
		allRecords = append(allRecords, records...)

		// 判断是否还有下一页
		totalPages := (totalCount + int64(pageSize) - 1) / int64(pageSize)
		if int64(pageNumber) >= totalPages {
			break
		}
		pageNumber++
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

// describeDomainRecords 调用阿里云 DescribeDomainRecords API 获取单页DNS记录
func describeDomainRecords(accessKeyID, accessKeySecret, domainName string, pageNumber, pageSize int) ([]DnsRecordItem, int64, error) {
	// 配置 API 请求参数
	params := CreateCommonAPIParams("DescribeDomainRecords", dnsVersion)

	// 设置查询参数
	queries := map[string]any{
		"DomainName": tea.String(domainName),
		"PageNumber": tea.Int64(int64(pageNumber)),
		"PageSize":   tea.Int64(int64(pageSize)),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	// 调用阿里云通用API
	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		return nil, 0, err
	}

	// 解析返回结果
	totalCount := jsondata.Get("body.TotalCount").Int()
	records := jsondata.Get("body.DomainRecords.Record").Array()

	var result []DnsRecordItem
	for _, r := range records {
		// 阿里云返回的是毫秒时间戳，需要转换为格式化的时间字符串
		createTs := r.Get("CreateTimestamp").Int()
		updateTs := r.Get("UpdateTimestamp").Int()

		createTime := ""
		updateTime := ""
		if createTs > 0 {
			createTime = time.Unix(createTs/1000, (createTs%1000)*int64(time.Millisecond)).Format("2006-01-02 15:04:05")
		}
		if updateTs > 0 {
			updateTime = time.Unix(updateTs/1000, (updateTs%1000)*int64(time.Millisecond)).Format("2006-01-02 15:04:05")
		}

		result = append(result, DnsRecordItem{
			RecordID:   r.Get("RecordId").String(),
			RR:         r.Get("RR").String(),
			Type:       r.Get("Type").String(),
			Value:      r.Get("Value").String(),
			TTL:        r.Get("TTL").Int(),
			Priority:   r.Get("Priority").Int(),
			Line:       r.Get("Line").String(),
			Weight:     int(r.Get("Weight").Int()),
			Status:     r.Get("Status").String(),
			Locked:     r.Get("Locked").Bool(),
			Remark:     r.Get("Remark").String(),
			CreateTime: createTime,
			UpdateTime: updateTime,
		})
	}

	fmt.Printf("  [DNS查询] 域名=%s, 第%d页, 获取 %d 条记录, 总计 %d 条\n", domainName, pageNumber, len(result), totalCount)

	return result, totalCount, nil
}
