package aliyun

// https://next.api.alibabacloud.com/api/
import (
	"encoding/json"
	"fmt"
	"os"

	"cloud-manager-backend/pkg/logger"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/darabonba-openapi/v2/models"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
	"github.com/tidwall/gjson"
)

// 创建阿里云API客户端
func createClient(accessKeyID, accessKeySecret, endpoint string) (*openapi.Client, error) {
	// 检查AK是否存在
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey 不能为空")
	}

	// 配置凭证参数
	credConfig := &credential.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     &accessKeyID,
		AccessKeySecret: &accessKeySecret,
	}

	// 创建凭证实例
	cred, err := credential.NewCredential(credConfig)
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

	// 解析响应
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return gjson.Result{}, fmt.Errorf("响应序列化失败: %w", err)
	}
	jsondata := gjson.Parse(string(jsonBytes))

	// 阿里云 RPC 接口的业务错误以 HTTP 200 + body.Code 形式返回，darabonba 不会视为 error，
	// 若不手动识别会被当成“成功但 0 条数据”静默吞掉。这里统一拦截，让上层打印真实原因。
	if code := jsondata.Get("body.Code").String(); code != "" && code != "Success" && code != "OK" {
		msg := jsondata.Get("body.Message").String()
		reqID := jsondata.Get("body.RequestId").String()
		return jsondata, fmt.Errorf("阿里云返回错误 Code=%s Message=%s RequestId=%s", code, msg, reqID)
	}

	return jsondata, nil
}

// logCdn 把同步调试日志同时输出到三处，确保排查“无数据”时一定能看到：
//   - fmt.Println   : 进入 SSE 进度通道（前端同步进度面板可见）
//   - logger.Info   : 业务结构化日志（与后端常态 INFO 日志同一出口，便于从同一日志文件查看）
//   - os.Stderr     : runSync 会把 os.Stdout 重定向到 SSE 管道，stderr 不受影响，保证后端控制台/日志必定可见
func logCdn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Println(msg)
	logger.Info("aliyun_cdn", msg)
	fmt.Fprintln(os.Stderr, msg)
}
