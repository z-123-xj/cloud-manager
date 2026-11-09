package cloudflare

// Cloudflare Zones（域名）拉取
// API: GET /client/v4/zones?page=N&per_page=50[&account.id=xxx]
// 文档: https://developers.cloudflare.com/api/operations/zones-get
//
// 数据流向：Cloudflare 是 DNS 服务商而非域名注册商——zone 里的域名都是从其他注册商
// （阿里云/腾讯云/GoDaddy 等）注册、把 NS 改到 Cloudflare 的，没有到期/续费概念。
// 因此 zones 同步到 dns_domains（DNS 可管理域名列表），而不是 domains（注册域名列表）。
//
// dns_domains 的「解析记录数」「DNS服务器」两列数据来源（zones 列表接口均不返回，需按 zone 补充查询）：
//   - 记录数：GET /zones/{id}/dns_records?per_page=1 → result_info.total_count
//   - NS：   优先用 zone 对象上的 name_servers；为空则 GET /zones/{id} 详情获取

import (
	"fmt"
	"net/url"
	"strings"

	"cloud-manager-backend/internal/models"
)

// cfZone Cloudflare Zone 对象（仅取需要的字段）
type cfZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"` // 域名（不带 www/协议）
	Status string `json:"status"`
	Paused bool   `json:"paused"` // CDN 是否已暂停
	Type   string `json:"type"`   // full / partial / secondary

	CreatedOn         string   `json:"created_on"`         // RFC3339，如 2014-08-15T18:19:22.846Z
	OriginalRegistrar string   `json:"original_registrar"` // 注册商（可能为空，预留）
	NameServers       []string `json:"name_servers"`       // 分配的 NS（列表接口可能不返回，为空时调详情接口补）
}

// ListDnsDomains 分页拉取 Cloudflare 账户下全部 Zone 并同步到 dns_domains 表
// token: API Token（Bearer）；cfAccountID: Cloudflare Account ID（为空则拉取 Token 权限内全部 zones）
func ListDnsDomains(token, cfAccountID, accountID, accountName, platform string) {
	c := NewClient(token, cfAccountID)

	page := 1
	totalPages := 1
	total := 0
	for page <= totalPages {
		query := url.Values{}
		query.Set("page", fmt.Sprintf("%d", page))
		query.Set("per_page", "50")
		if cfAccountID != "" {
			query.Set("account.id", cfAccountID)
		}

		var zones []cfZone
		info, err := c.Get("/zones", query, &zones)
		if err != nil {
			fmt.Printf("  [跳过] 查询Cloudflare域名失败 [账号: %s, 第%d页]: %v\n", accountName, page, err)
			return
		}
		if info != nil && info.TotalPages > 0 {
			totalPages = info.TotalPages
		}

		// 转为统一结构体并同步到 dns_domains（DNS 可管理域名列表）
		var unified []models.UnifiedDnsDomainItem
		for _, z := range zones {
			// 解析记录数：dns_records 列表只取 1 条，用 result_info.total_count 拿总数
			recordCount := 0
			rcQuery := url.Values{}
			rcQuery.Set("page", "1")
			rcQuery.Set("per_page", "1")
			if rcInfo, rcErr := c.Get("/zones/"+z.ID+"/dns_records", rcQuery, nil); rcErr == nil && rcInfo != nil {
				recordCount = rcInfo.TotalCount
			} else if rcErr != nil {
				// 单 zone 补充查询失败不中断整体同步，记录数留 0
				fmt.Printf("  [警告] 查询 %s 解析记录数失败: %v\n", z.Name, rcErr)
			}

			// DNS服务器（NS）：优先 zone 对象自带；为空再调 zone 详情接口
			dnsServers := strings.Join(z.NameServers, ",")
			if dnsServers == "" {
				var detail struct {
					NameServers []string `json:"name_servers"`
				}
				if _, dErr := c.Get("/zones/"+z.ID, nil, &detail); dErr == nil {
					dnsServers = strings.Join(detail.NameServers, ",")
				}
			}

			unified = append(unified, models.UnifiedDnsDomainItem{
				DomainName: z.Name,
				// zone 在 Cloudflare 上即"使用 Cloudflare DNS"（对应阿里云 AllAliDns 的真实 NS 语义）
				AliDomain: true,
				// Cloudflare 无付费版/免费版 DNS 之分
				Starmark: false,
				// 解析记录数（dns_records?per_page=1 的 total_count）
				RecordCount: recordCount,
				// 分配的 NS（如 ada.ns.cloudflare.com）
				DnsServers: dnsServers,
				CreateTime: z.CreatedOn,
			})
		}
		models.SyncDnsDomainsToDB(unified, accountID, accountName, platform)
		total += len(zones)
		page++
	}
	fmt.Printf("Cloudflare DNS域名同步完成: 账号=%s, 共 %d 个\n", accountName, total)
}
