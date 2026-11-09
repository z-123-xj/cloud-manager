package aliyun

// 查询阿里云资源包实例（计费服务 BssOpenApi 的 QueryResourcePackageInstances）
// 文档: https://api.aliyun.com/document/BssOpenApi/2017-12-14/QueryResourcePackageInstances
//
// 说明：
//   - 资源包属于账号级（非地域级）数据，使用计费服务 API，请求参数国内站与国际站完全一致。
//   - 但 endpoint 必须按站点分开：国内站 business.aliyuncs.com，
//     国际站 business.ap-southeast-1.aliyuncs.com——国际站 AK 在国内站网关会直接被拒。
//   - 该接口仅返回「有效」的资源包实例（已过期/已关闭的不返回）。

import (
	"fmt"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

// ResourcePackageItem 统一后的资源包实例结构
type ResourcePackageItem struct {
	InstanceID      string `json:"instance_id"`      // 资源包实例ID
	ProductCode     string `json:"product_code"`     // 产品代码，如 ossbag / cdnbag / dcdn
	CommodityCode   string `json:"commodity_code"`   // 商品码（部分资源包 ProductCode 为空，以此为准），如 dcdn / rds
	PackageType     string `json:"package_type"`     // 资源包类型编码
	Remark          string `json:"remark"`           // 商品名称/说明（云上资源包商品的说明文字）
	Status          string `json:"status"`           // Available（有效）/ Expired（失效）
	EffectiveTime   string `json:"effective_time"`   // 生效时间（ISO8601 UTC）
	ExpiryTime      string `json:"expiry_time"`      // 失效时间（ISO8601 UTC）
	TotalAmount     string `json:"total_amount"`     // 总量
	TotalUnit       string `json:"total_unit"`       // 总量单位（次/TB/GB）
	RemainingAmount string `json:"remaining_amount"` // 剩余量
	RemainingUnit   string `json:"remaining_unit"`   // 剩余量单位
	Region          string `json:"region"`           // 地域（部分资源包有）
	AccountID       string `json:"account_id"`       // 所属账号（本平台附加）
	AccountName     string `json:"account_name"`     // 账号别名（本平台附加）
	Site            string `json:"site"`             // cn=国内站 intl=国际站（本平台附加）
}

// bssEndpoint 计费服务（BssOpenApi）endpoint，国内站与国际站各用各的接入点。
func bssEndpoint(site string) string {
	if site == "intl" {
		return "business.ap-southeast-1.aliyuncs.com"
	}
	return "business.aliyuncs.com"
}

// QueryResourcePackages 查询单个阿里云账号下的全部资源包实例（自动翻页）
func QueryResourcePackages(accessKeyID, accessKeySecret, site, accountID, accountName string) ([]ResourcePackageItem, error) {
	endpoint := bssEndpoint(site)
	pageNumber := 1
	pageSize := 300 // 该接口单页最大值，减少翻页次数

	var all []ResourcePackageItem
	for {
		params := CreateCommonAPIParams("QueryResourcePackageInstances", "2017-12-14")
		queries := map[string]any{
			"PageNum":  tea.Int(pageNumber),
			"PageSize": tea.Int(pageSize),
		}
		request := &openapi.OpenApiRequest{Query: openapiutil.Query(queries)}

		fmt.Printf("  [资源包] 查询账号 %s（site=%s, endpoint=%s）资源包，第 %d 页\n", accountName, site, endpoint, pageNumber)
		jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, endpoint, params, request)
		if err != nil {
			return all, fmt.Errorf("调用 QueryResourcePackageInstances 失败: %w", err)
		}

		totalCount := jsondata.Get("body.TotalCount").Int()
		instances := jsondata.Get("body.Data.Instances.Instance").Array()
		for _, inst := range instances {
			totalUnit := inst.Get("TotalAmountUnit").String()
			if totalUnit == "" {
				totalUnit = inst.Get("CapacityUnit").String()
			}
			remainUnit := inst.Get("RemainingAmountUnit").String()
			if remainUnit == "" {
				remainUnit = totalUnit
			}
			// 调试：仅打印首个实例的原始字段（原始 JSON），方便对照「产品名称」到底该取哪个字段。
			// 阿里云返回 Instance 对象的候选字段：ProductCode / CommodityCode / ProductType / Remark / PackageType ...
			// 想看真实返回时，运行后查后端日志里 [资源包][调试] 这一行，把内容发回来即可确定正确字段。
			if len(all) == 0 {
				fmt.Printf("[资源包][调试] 原始实例字段:\n%s\n", inst.Raw)
			}
			all = append(all, ResourcePackageItem{
				InstanceID:      inst.Get("InstanceId").String(),
				ProductCode:     inst.Get("ProductCode").String(),
				CommodityCode:   inst.Get("CommodityCode").String(),
				PackageType:     inst.Get("PackageType").String(),
				Remark:          inst.Get("Remark").String(),
				Status:          inst.Get("Status").String(),
				EffectiveTime:   inst.Get("EffectiveTime").String(),
				ExpiryTime:      inst.Get("ExpiryTime").String(),
				TotalAmount:     inst.Get("TotalAmount").String(),
				TotalUnit:       totalUnit,
				RemainingAmount: inst.Get("RemainingAmount").String(),
				RemainingUnit:   remainUnit,
				Region:          inst.Get("Region").String(),
				AccountID:       accountID,
				AccountName:     accountName,
				Site:            site,
			})
		}

		if len(instances) < pageSize || int64(len(all)) >= totalCount {
			break
		}
		pageNumber++
	}
	return all, nil
}
