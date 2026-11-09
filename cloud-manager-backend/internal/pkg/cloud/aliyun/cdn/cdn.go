package cdn

// 查询阿里云CDN加速域名列表并同步到 cdn_domains 表
// API: DescribeUserDomains（2018-05-10）
// 文档: https://help.aliyun.com/document_detail/91173.html
//
// 字段说明（DescribeUserDomains 为列表接口，字段较精简）：
//   - DomainName 加速域名 / Cname 平台分配的 .kunlunar.com 域名 / DomainStatus 运行状态
//   - Sources.Source 源站列表（可能多个主备源，拼接存储）
//   - ResourceType 业务类型（部分账号不返回，为空则留空）
//   - HTTPS 状态与证书到期时间列表接口不返回，逐域名调 DescribeDomainCertificateInfo 补全

import (
	"fmt"
	"strings"

	"cloud-manager-backend/internal/models"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

const cdnEndpoint = "cdn.aliyuncs.com"
const cdnVersion = "2018-05-10" // 阿里云CDN API版本

// cdnStatusMap 阿里云CDN域名状态 → 中文
var cdnStatusMap = map[string]string{
	"online":             "运行中",
	"offline":            "已停止",
	"configuring":        "配置中",
	"configure failed":   "配置失败",
	"configuring failed": "配置失败",
}

// QueryCdnDomainList 分页查询账号下全部CDN加速域名并同步到 cdn_domains 表
// site: cn=国内站 / intl=国际站（决定接入点与账号归属）
func QueryCdnDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, site string) {
	pageNumber := 1
	pageSize := 50

	var allItems []models.UnifiedCdnItem

	for {
		params := CreateCommonAPIParams("DescribeUserDomains", cdnVersion)
		queries := map[string]any{
			"PageNumber": tea.Int(pageNumber),
			"PageSize":   tea.Int(pageSize),
		}
		request := &openapi.OpenApiRequest{
			Query: openapiutil.Query(queries),
		}

		jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, cdnEndpoint, params, request)
		// 调试：打印阿里云 CDN 接口原始响应，便于排查“无数据”问题（response body 完整输出）
		logCdn("[CDN调试] 账号=%s site=%s endpoint=%s 第%d页 原始响应: %s", accountName, site, cdnEndpoint, pageNumber, jsondata.String())
		if err != nil {
			logCdn("  [跳过] CDN域名API调用异常 [账号: %s, 第%d页]: %v", accountName, pageNumber, err)
			return
		}

		totalCount := jsondata.Get("body.TotalCount").Int()
		domains := jsondata.Get("body.Domains.PageData").Array()

		for _, d := range domains {
			// 源站：Sources.Source 数组（可能多个主备源），逗号拼接
			var origins []string
			for _, s := range d.Get("Sources.Source").Array() {
				if content := s.Get("Content").String(); content != "" {
					origins = append(origins, content)
				}
			}

			// 状态归一化为中文（未知状态原样保留）
			status := d.Get("DomainStatus").String()
			if zh, ok := cdnStatusMap[status]; ok {
				status = zh
			}

			// 逐域名查询绑定证书信息：补全 HTTPS 真实状态与证书到期时间（列表接口不返回）
			httpsOn, certExpire, certName, certType := describeDomainCertInfo(
				accessKeyID, accessKeySecret, cdnEndpoint, cdnVersion,
				"DescribeDomainCertificateInfo", d.Get("DomainName").String(),
			)

			allItems = append(allItems, models.UnifiedCdnItem{
				DomainName:     d.Get("DomainName").String(),
				Cname:          d.Get("Cname").String(),
				Product:        "cdn", // CDN 产品（区别于 DCDN 全站加速）
				Status:         status,
				BizType:        d.Get("ResourceType").String(), // 部分账号不返回，为空则留空
				OriginDomain:   strings.Join(origins, ", "),
				HttpsOn:        httpsOn,
				CertName:       certName,
				CertType:       certType,
				CertExpireTime: certExpire,
			})
		}

		fmt.Printf("  [CDN] 阿里云账号 %s 第%d页: %d 个加速域名（累计 %d）\n", accountName, pageNumber, len(domains), len(allItems))

		// 判断是否还有下一页
		totalPages := (totalCount + int64(pageSize) - 1) / int64(pageSize)
		if len(domains) == 0 || int64(pageNumber) >= totalPages {
			break
		}
		pageNumber++
	}

	models.SyncCdnToDB(allItems, accountID, accountName, platform)
}
