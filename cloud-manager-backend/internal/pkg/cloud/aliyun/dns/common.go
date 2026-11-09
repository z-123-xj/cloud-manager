package dns

// DNS操作公共模块：常量、凭证获取、API参数构建、API调用

import (
	"encoding/json"
	"fmt"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/darabonba-openapi/v2/models"
	"github.com/alibabacloud-go/tea/tea"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/aliyun/credentials-go/credentials"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ==================== 常量定义 ====================

const dnsEndpoint = "alidns.aliyuncs.com" // 阿里云DNS服务端点
const dnsVersion = "2015-01-09"           // 阿里云DNS API版本号

// ==================== 凭证获取 ====================

// getCredentialFromContext 从 gin context 获取DNS操作的AK/SK凭证
// 凭证由上层 cloud.dns_handler.go 在调用前设置
func getCredentialFromContext(c *gin.Context) (string, string) {
	akID, _ := c.Get("dns_access_key_id")
	akSecret, _ := c.Get("dns_access_key_secret")
	return fmt.Sprintf("%v", akID), fmt.Sprintf("%v", akSecret)
}

// ==================== API参数构建 ====================

// CreateCommonAPIParams 创建阿里云通用API请求参数
// action: API操作名称，如 "DescribeDomainRecords"
// version: API版本号，如 "2015-01-09"
func CreateCommonAPIParams(action, version string) *openapi.Params {
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

// ==================== API调用 ====================

// createClient 创建阿里云API客户端
func createClient(accessKeyID, accessKeySecret, endpoint string) (*openapi.Client, error) {
	// 检查AK是否存在
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey 不能为空")
	}

	// 配置凭证参数
	credConfig := &credentials.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     &accessKeyID,
		AccessKeySecret: &accessKeySecret,
	}

	// 创建凭证实例
	cred, err := credentials.NewCredential(credConfig)
	if err != nil {
		return nil, fmt.Errorf("创建凭证失败: %w", err)
	}

	// 配置客户端
	config := &openapi.Config{
		Credential: cred,
		Endpoint:   tea.String(endpoint),
	}

	// 创建客户端实例
	client, err := openapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	return client, nil
}

// RequestCommonAPI 调用阿里云通用API
// 统一封装：创建客户端 → 调用API → 解析JSON响应
func RequestCommonAPI(accessKeyID, accessKeySecret, endpoint string, params *openapi.Params, request *models.OpenApiRequest) (gjson.Result, error) {
	// 创建客户端
	client, err := createClient(accessKeyID, accessKeySecret, endpoint)
	if err != nil {
		return gjson.Result{}, fmt.Errorf("客户端初始化失败: %w", err)
	}

	// 调用API
	response, err := client.CallApi(params, request, &util.RuntimeOptions{})
	if err != nil {
		return gjson.Result{}, fmt.Errorf("API调用失败: %w", err)
	}

	// 解析响应为JSON
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return gjson.Result{}, fmt.Errorf("响应序列化失败: %w", err)
	}
	jsondata := gjson.Parse(string(jsonBytes))

	return jsondata, nil
}
