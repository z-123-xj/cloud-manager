package tencent

// 查询DNSPod可管理的域名列表（DescribeDomainList API）
// https://cloud.tencent.com/document/product/1427/93722

import (
	"fmt"
	"strings"

	"cloud-manager-backend/internal/models"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// QueryDnsDomainList 查询DNSPod控制台可管理的域名列表并同步到 dns_domains 表
func QueryDnsDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	credential := common.NewCredential(accessKeyID, accessKeySecret)
	cpf := createDNSPodProfile()
	client, err := dnspod.NewClient(credential, "", cpf)
	if err != nil {
		fmt.Printf("  [跳过] 创建DNSPod客户端失败 [账号: %s]: %v\n", accountName, err)
		return
	}

	offset := int64(0)
	limit := int64(100)

	var allDomains []models.UnifiedDnsDomainItem

	for {
		request := dnspod.NewDescribeDomainListRequest()
		request.Offset = common.Int64Ptr(offset)
		request.Limit = common.Int64Ptr(limit)

		response, err := client.DescribeDomainList(request)
		if err != nil {
			fmt.Printf("  [跳过] DNSPod域名列表API调用异常 [账号: %s, Offset=%d]: %v\n", accountName, offset, err)
			return
		}

		totalCount := safeUint64Ptr(response.Response.DomainCountInfo.DomainTotal)
		domains := response.Response.DomainList

		if len(domains) == 0 {
			break
		}

		for _, d := range domains {
			// DNSPod的DNSStatus: "DNSERROR" 表示未使用当前账号NS
			aliDomain := true
			if d.DNSStatus != nil && *d.DNSStatus == "DNSERROR" {
				aliDomain = false
			}

			// IsVip 是字符串类型："YES" 表示付费版（注意大写）
			starmark := false
			if d.IsVip != nil && strings.EqualFold(*d.IsVip, "yes") {
				starmark = true
			}

			// 提取DNS服务器列表（EffectiveDNS 是 []*string）
			var serverList []string
			for _, s := range d.EffectiveDNS {
				serverList = append(serverList, safeString(s))
			}

			item := models.UnifiedDnsDomainItem{
				DomainName:  safeString(d.Name),
				AliDomain:   aliDomain,
				Starmark:    starmark,
				RecordCount: int(safeUint64Ptr(d.RecordCount)),
				DnsServers:  strings.Join(serverList, ","),
				CreateTime:  safeString(d.CreatedOn),
			}
			fmt.Printf("  [DNS域名-腾讯云] %s AliDomain=%v Starmark=%v RecordCount=%d\n",
				item.DomainName, item.AliDomain, item.Starmark, item.RecordCount)
			allDomains = append(allDomains, item)
		}

		totalPages := int64((totalCount + uint64(limit) - 1) / uint64(limit))
		offset += limit
		if offset/limit >= totalPages {
			break
		}
	}

	fmt.Printf("DNSPod域名列表查询完成: 账号=%s, 共 %d 条\n", accountName, len(allDomains))

	// 同步到 dns_domains 表
	models.SyncDnsDomainsToDB(allDomains, accountID, accountName, platform)
}
