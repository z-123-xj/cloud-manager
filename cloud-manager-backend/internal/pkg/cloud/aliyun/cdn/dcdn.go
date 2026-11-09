package cdn

// 查询阿里云 DCDN（全站加速）域名列表并同步到 cdn_domains 表
// API: DescribeDcdnUserDomains（2018-01-15） endpoint: dcdn.aliyuncs.com
// 注意：DCDN 的域名列表接口名为 DescribeDcdnUserDomains（非 DescribeDcdnDomains），
// 用错动作名会返回 404 InvalidAction.NotFound。
//
// 背景：阿里云的加速类产品中，除了 CDN（DescribeUserDomains）外，还有 DCDN（全站加速，
// 原名“全站加速”，endpoint 为 dcdn.aliyuncs.com）。两者为独立产品、独立 API，需在同步时分别拉取。
// 为区分归属，统一结构中使用 Product 字段：cdn=CDN，dcdn=全站加速。
//
// 说明：DCDN 国内站与国际站均尝试拉取（不再按 site 跳过），国际站若不可用会在调用时报错并跳过，
// 由上层 recover 隔离，不影响其它账号/资源。如国际站需不同端点，调整 dcdnEndpointForSite 即可。

import (
	"fmt"
	"strings"

	"cloud-manager-backend/internal/models"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

const dcdnEndpoint = "dcdn.aliyuncs.com"
const dcdnVersion = "2018-01-15" // 阿里云DCDN API版本（全站加速）

// dcdnEndpointForSite 根据站点选择 DCDN 接入点。
// 当前国内站与国际站接入点一致（均为 dcdn.aliyuncs.com，按 AK 区分账号）；
// 若国际站后续需独立端点，在此处扩展即可。
func dcdnEndpointForSite(site string) string {
	_ = site // 预留按站点区分的扩展点
	return dcdnEndpoint
}

// createDcdnParams 构造 DCDN 请求参数（与 CDN 的 CreateCommonAPIParams 一致：RPC + POST + json）。
func createDcdnParams(action, version string) *openapi.Params {
	return &openapi.Params{
		Action:      tea.String(action),
		Version:     tea.String(version),
		Protocol:    tea.String("HTTPS"),
		Method:      tea.String("POST"),
		AuthType:    tea.String("AK"),
		Style:       tea.String("RPC"),
		Pathname:    tea.String("/"),
		ReqBodyType: tea.String("json"),
		BodyType:    tea.String("json"),
	}
}

// dcdnStatusMap 阿里云DCDN域名状态 → 中文
var dcdnStatusMap = map[string]string{
	"online":           "运行中",
	"offline":          "已停止",
	"configuring":      "配置中",
	"configure_failed": "配置失败",
}

// QueryDcdnDomainList 分页查询账号下全部 DCDN（全站加速）域名并同步到 cdn_domains 表
// site: cn=国内站 / intl=国际站（决定接入点与账号归属）
func QueryDcdnDomainList(accessKeyID, accessKeySecret, accountID, accountName, platform, site string) {
	pageNumber := 1
	pageSize := 50
	endpoint := dcdnEndpointForSite(site)

	var allItems []models.UnifiedCdnItem

	for {
		params := createDcdnParams("DescribeDcdnUserDomains", dcdnVersion)
		queries := map[string]any{
			"PageNumber": tea.Int(pageNumber),
			"PageSize":   tea.Int(pageSize),
		}
		request := &openapi.OpenApiRequest{
			Query: openapiutil.Query(queries),
		}

		jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, endpoint, params, request)
		// 调试：打印阿里云 DCDN 接口原始响应，便于排查“无数据”问题（response body 完整输出）
		logCdn("[DCDN调试] 账号=%s site=%s endpoint=%s 第%d页 原始响应: %s", accountName, site, endpoint, pageNumber, jsondata.String())
		if err != nil {
			logCdn("  [跳过] DCDN域名API调用异常 [账号: %s (site=%s), 第%d页]: %v", accountName, site, pageNumber, err)
			return
		}

		totalCount := jsondata.Get("body.TotalCount").Int()
		// 域名列表路径：DescribeDcdnUserDomains 通常为 Domains.PageData；兼容个别变体 Domains.Domain
		domains := jsondata.Get("body.Domains.PageData").Array()
		if len(domains) == 0 {
			domains = jsondata.Get("body.Domains.Domain").Array()
		}

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
			if zh, ok := dcdnStatusMap[status]; ok {
				status = zh
			}

			// 逐域名查询绑定证书信息：补全 HTTPS 真实状态与证书到期时间（列表接口不返回）
			httpsOn, certExpire, certName, certType := describeDomainCertInfo(
				accessKeyID, accessKeySecret, endpoint, dcdnVersion,
				"DescribeDcdnDomainCertificateInfo", d.Get("DomainName").String(),
			)

			allItems = append(allItems, models.UnifiedCdnItem{
				DomainName:     d.Get("DomainName").String(),
				Cname:          d.Get("Cname").String(),
				Product:        "dcdn", // 全站加速产品
				Status:         status,
				BizType:        d.Get("ResourceType").String(), // 业务类型（原样保留，取值与CDN不同）
				OriginDomain:   strings.Join(origins, ", "),
				HttpsOn:        httpsOn,
				CertName:       certName,
				CertType:       certType,
				CertExpireTime: certExpire,
			})
		}

		fmt.Printf("  [DCDN] 阿里云账号 %s 第%d页: %d 个全站加速域名（累计 %d）\n", accountName, pageNumber, len(domains), len(allItems))

		// 判断是否还有下一页
		totalPages := (totalCount + int64(pageSize) - 1) / int64(pageSize)
		if len(domains) == 0 || int64(pageNumber) >= totalPages {
			break
		}
		pageNumber++
	}

	models.SyncCdnToDB(allItems, accountID, accountName, platform)
}
