package aliyun

// 修改「域名」的备注（云解析 DNS 的 UpdateDomainRemark API）
// 文档: https://help.aliyun.com/zh/dns/api-alidns-2015-01-09-updatedomainremark
//
// 说明：阿里云「域名注册」服务（Domain 2018-01-29）只在 QueryDomainList 里返回备注，
// 未开放写入接口；可编程写入的域名备注在云解析侧（Alidns 2015-01-09），
// 因此本平台同步域名备注到云端走 UpdateDomainRemark。
//
// 限制：
//   - 仅对「云解析中已存在的域名」生效（域名需在该账号的阿里云解析控制台里）；
//     未使用阿里云 DNS 的域名调用会失败，此时本平台只落库不同步（上层会降级处理）。
//   - Remark 不超过 50 个字符。

import (
	"fmt"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

// UpdateDomainRemark 调用阿里云云解析 UpdateDomainRemark 修改域名备注
func UpdateDomainRemark(accessKeyID, accessKeySecret, domainName, remark string) error {
	fmt.Printf("  [域名备注] 调用 UpdateDomainRemark: DomainName=%s, Remark=%s\n", domainName, remark)

	// 配置 API 请求参数（云解析 Alidns 2015-01-09）
	params := CreateCommonAPIParams("UpdateDomainRemark", dnsVersion)

	// 设置查询参数
	queries := map[string]any{
		"DomainName": tea.String(domainName),
		"Remark":     tea.String(remark),
		"Lang":       tea.String("zh"),
	}
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	// 调用阿里云通用API
	if _, err := RequestCommonAPI(accessKeyID, accessKeySecret, dnsEndpoint, params, request); err != nil {
		return fmt.Errorf("调用UpdateDomainRemark失败: %w", err)
	}

	return nil
}
