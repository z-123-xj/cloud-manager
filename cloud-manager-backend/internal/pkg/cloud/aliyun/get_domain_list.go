package aliyun

// 分页查询自己账户下的域名列表（域名注册信息）
// 文档: https://next.api.alibabacloud.com/api/Domain/2018-01-29/QueryDomainList?tab=DEBUG&params={%22PageNum%22:1,%22PageSize%22:10}
//
// 国内站(cn) / 国际站(intl) 分别走 QueryDomainListCN / QueryDomainListIntl 入口，
// 当前两者共用同一端点 domain.aliyuncs.com（仅代码结构拆分，行为不变）；
// 若国际站需独立端点（如 domain-intl.aliyuncs.com），在 queryDomainList 内按 site 调整 endpoint 即可。

import (
	"fmt"

	"cloud-manager-backend/internal/models"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

type AliyunDomainItem struct {
	DomainName             string `json:"DomainName"`             // 域名
	Ccompany               string `json:"Ccompany"`               // 所有者
	AutoRenewEnabled       bool   `json:"AutoRenewEnabled"`       // 自动续费
	RegistrationDate       string `json:"RegistrationDate"`       // 注册日期
	ExpirationDate         string `json:"ExpirationDate"`         // 到期日期
	ExpirationCurrDateDiff int    `json:"ExpirationCurrDateDiff"` // 剩余天数
	ExpirationDateStatus   string `json:"ExpirationDateStatus"`   // 到期状态码
	Remark                 string `json:"Remark"`                 // 备注
}

// QueryDomainListCN 国内站域名注册列表同步（单页，返回总页数）
func QueryDomainListCN(accessKeyID, accessKeySecret, accountID, accountName, platform string, pageNumber, pageSize int) int64 {
	return queryDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, "cn", pageNumber, pageSize)
}

// QueryDomainListIntl 国际站域名注册列表同步（单页，返回总页数）
func QueryDomainListIntl(accessKeyID, accessKeySecret, accountID, accountName, platform string, pageNumber, pageSize int) int64 {
	return queryDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, "intl", pageNumber, pageSize)
}

// queryDomainList 查询单页域名并写入/更新数据库（不做下线标记）
// 返回: totalPageNum（总页数）
func queryDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, site string, pageNumber, pageSize int) int64 {
	endpoint := "domain.aliyuncs.com" // 国内站/国际站当前共用端点；国际站独立端点在此按 site 调整

	// 配置 API 请求参数
	params := CreateCommonAPIParams("QueryDomainList", "2018-01-29")

	// 设置查询参数
	queries := map[string]any{
		"PageNum":  tea.Int(pageNumber), // 页码
		"PageSize": tea.Int(pageSize),   // 每页条数
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, endpoint, params, request)
	if err != nil {
		fmt.Printf("  [跳过] API调用异常 [账号: %s, 第%d页]: %v\n", accountName, pageNumber, err)
		return 0
	}

	totalPageNum := jsondata.Get("body.TotalPageNum").Int()
	totalItemNum := jsondata.Get("body.TotalItemNum").Int()
	currentPageNum := jsondata.Get("body.CurrentPageNum").Int()

	fmt.Printf("总页数: %d, 总条数: %d, 当前页: %d\n", totalPageNum, totalItemNum, currentPageNum)

	// 获取域名列表
	domains := jsondata.Get("body.Data.Domain").Array()

	if len(domains) == 0 {
		fmt.Println("没有查询到域名")
		return totalPageNum
	}

	// 调试：打印第一条域名的所有字段原始JSON，用于排查字段名问题
	// if len(domains) > 0 {
	// 	fmt.Printf("  [DEBUG] 第一条域名原始数据: %s\n", domains[0].String())
	// }

	// 存储所有域名结构体
	var domainList []AliyunDomainItem

	// 遍历并赋值给结构体
	for _, d := range domains {
		item := AliyunDomainItem{
			DomainName:             d.Get("DomainName").String(),
			Ccompany:               d.Get("Ccompany").String(),
			AutoRenewEnabled:       d.Get("AutoRenewEnabled").Bool(),
			RegistrationDate:       d.Get("RegistrationDate").String(),
			ExpirationDate:         d.Get("ExpirationDate").String(),
			ExpirationCurrDateDiff: int(d.Get("ExpirationCurrDateDiff").Int()),
			ExpirationDateStatus:   d.Get("ExpirationDateStatus").String(),
			Remark:                 d.Get("Remark").String(),
		}
		domainList = append(domainList, item)
	}

	// for idx, domain := range domainList {
	// 	fmt.Printf("\n--- 域名 %d ---\n", idx+1)
	// 	fmt.Printf("  域名: %s\n", domain.DomainName)
	// 	fmt.Printf("  所有者: %s\n", domain.Ccompany)
	// 	fmt.Printf("  自动续费: %v\n", domain.AutoRenewEnabled)
	// 	fmt.Printf("  注册日期: %s\n", domain.RegistrationDate)
	// 	fmt.Printf("  到期日期: %s\n", domain.ExpirationDate)
	// 	fmt.Printf("  剩余天数: %d\n", domain.ExpirationCurrDateDiff)
	// 	fmt.Printf("  到期状态码: %s\n", domain.ExpirationDateStatus)
	// 	fmt.Printf("  备注: %s\n", domain.Remark)
	// }

	// 转为统一结构体并同步到数据库
	var unified []models.UnifiedDomainItem
	for _, item := range domainList {
		unified = append(unified, models.UnifiedDomainItem{
			DomainName:             item.DomainName,
			Owner:                  item.Ccompany,
			AutoRenewEnabled:       item.AutoRenewEnabled,
			RegistrationDate:       item.RegistrationDate,
			ExpirationDate:         item.ExpirationDate,
			ExpirationCurrDateDiff: item.ExpirationCurrDateDiff,
			ExpirationDateStatus:   item.ExpirationDateStatus,
			Remark:                 item.Remark,
		})
	}
	models.SyncDomainsToDB(unified, accountID, accountName, platform, site)

	return totalPageNum
}
