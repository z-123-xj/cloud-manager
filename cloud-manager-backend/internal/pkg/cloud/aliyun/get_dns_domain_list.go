package aliyun

// 查询DNS控制台可管理的域名列表（DescribeDomains API）+ 真实NS状态（DescribeDomainNs API）
// https://api.aliyun.com/product/Alidns
// DescribeDomains 返回所有在DNS控制台可操作的域名（包括从其他注册商转入的）
// DescribeDomainNs 返回域名的真实NS记录，AllAliDns=true 才表示实际使用阿里云DNS

import (
	"fmt"
	"strings"

	"cloud-manager-backend/internal/models"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

const dnsEndpoint = "alidns.aliyuncs.com"
const dnsVersion = "2015-01-09"

// QueryDnsDomainListCN 国内站DNS可管理域名列表同步
func QueryDnsDomainListCN(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	queryDnsDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, "cn")
}

// QueryDnsDomainListIntl 国际站DNS可管理域名列表同步
func QueryDnsDomainListIntl(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	queryDnsDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, "intl")
}

// queryDnsDomainList 查询DNS控制台可管理的域名列表并同步到 dns_domains 表
func queryDnsDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, site string) {
	pageNumber := 1
	pageSize := 100

	var allDomains []models.UnifiedDnsDomainItem

	for {
		params := CreateCommonAPIParams("DescribeDomains", dnsVersion)
		queries := map[string]any{
			"PageNumber": tea.Int64(int64(pageNumber)),
			"PageSize":   tea.Int64(int64(pageSize)),
		}
		request := &openapi.OpenApiRequest{
			Query: openapiutil.Query(queries),
		}

		jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
		if err != nil {
			fmt.Printf("  [跳过] DNS域名API调用异常 [账号: %s, 第%d页]: %v\n", accountName, pageNumber, err)
			return
		}

		totalCount := jsondata.Get("body.TotalCount").Int()
		domains := jsondata.Get("body.Domains.Domain").Array()

		if len(domains) == 0 {
			break
		}

		for _, d := range domains {
			domainName := d.Get("DomainName").String()

			// 提取DescribeDomains返回的DNS服务器列表
			dnsServers := d.Get("DnsServers.DnsServer").Array()
			var serverList []string
			for _, s := range dnsServers {
				serverList = append(serverList, s.String())
			}

			// 查询真实NS状态（DescribeDomainNs API）
			// AllAliDns=true 表示域名实际使用阿里云DNS解析，而非仅仅注册在万网
			// AliDomain 字段仅表示域名在阿里云系统内（如万网注册），不代表使用阿里云DNS
			allAliDns := isUsingAliyunDns(accessKeyID, accessKeySecret, domainName, accountName)

			item := models.UnifiedDnsDomainItem{
				DomainName:  domainName,
				AliDomain:   allAliDns, // 改为真实的NS状态，而非 AliDomain 字段
				Starmark:    d.Get("Starmark").Bool(),
				RecordCount: int(d.Get("RecordCount").Int()),
				DnsServers:  strings.Join(serverList, ","),
				CreateTime:  d.Get("CreateTime").String(),
			}
			fmt.Printf("  [DNS域名] %s AliDomain=%v(NS=%s) Starmark=%v RecordCount=%d\n",
				item.DomainName, item.AliDomain, item.DnsServers, item.Starmark, item.RecordCount)
			allDomains = append(allDomains, item)
		}

		totalPages := (totalCount + int64(pageSize) - 1) / int64(pageSize)
		if int64(pageNumber) >= totalPages {
			break
		}
		pageNumber++
	}

	fmt.Printf("DNS域名列表查询完成: 账号=%s, 共 %d 条\n", accountName, len(allDomains))

	// 同步到 dns_domains 表
	models.SyncDnsDomainsToDB(allDomains, accountID, accountName, platform)
}

// isUsingAliyunDns 调用 DescribeDomainNs API 查询域名是否实际使用阿里云DNS解析
// 返回 true 表示 AllAliDns=true（所有NS都是阿里云的）
func isUsingAliyunDns(accessKeyID, accessKeySecret, domainName, accountName string) bool {
	params := CreateCommonAPIParams("DescribeDomainNs", dnsVersion)
	queries := map[string]any{
		"DomainName": tea.String(domainName),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request)
	if err != nil {
		fmt.Printf("  [DNS-NS] DescribeDomainNs调用失败 [%s]: %v\n", domainName, err)
		return false
	}

	allAliDns := jsondata.Get("body.AllAliDns").Bool()
	includeAliDns := jsondata.Get("body.IncludeAliDns").Bool()

	if !allAliDns {
		// 打印实际指向的NS，便于排查
		actualNs := jsondata.Get("body.DnsServers.DnsServer").Array()
		var nsList []string
		for _, ns := range actualNs {
			nsList = append(nsList, ns.String())
		}
		fmt.Printf("  [DNS-NS] %s 未使用阿里云DNS, 实际NS=%v (AllAliDns=%v, IncludeAliDns=%v)\n",
			domainName, nsList, allAliDns, includeAliDns)
	}

	return allAliDns
}
