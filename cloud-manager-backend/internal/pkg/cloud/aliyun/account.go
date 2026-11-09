package aliyun

import (
	"fmt"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
)

// GetAccountID 通过 STS GetCallerIdentity 获取阿里云主账号 UID（account_id）
// STS 为全局服务，endpoint 固定 sts.aliyuncs.com，与站点（国内/国际）无关。
// 该接口无需任何参数与特殊权限，普通 AK 即可调用，是"我是谁"的标准查询方式；
// 不用 RAM GetUser（其 API 版本号易失效，且必须传 UserName 参数）。
func GetAccountID(accessKey, secret string) (string, error) {
	params := CreateCommonAPIParams("GetCallerIdentity", "2015-04-01")
	request := &openapi.OpenApiRequest{}
	jsondata, err := RequestCommonAPI(accessKey, secret, "sts.aliyuncs.com", params, request)
	if err != nil {
		return "", fmt.Errorf("获取阿里云账号ID失败: %w", err)
	}
	accountId := jsondata.Get("body.AccountId").String()
	if accountId == "" {
		return "", fmt.Errorf("阿里云 STS 未返回账号ID（AccountId 为空）")
	}
	return accountId, nil
}
