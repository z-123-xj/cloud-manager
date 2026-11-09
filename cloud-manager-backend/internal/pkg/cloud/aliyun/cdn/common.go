package cdn

// CDN/DCDN 公共模块：凭证获取、API参数构建、API调用、调试日志。
// 与 aliyun/dns 子包保持一致的内部约定：CDN 相关收集代码独立成包，
// 不与其他资源（域名注册、DNS解析等）的收集代码混在同一目录，互不干扰。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"cloud-manager-backend/pkg/logger"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapimodels "github.com/alibabacloud-go/darabonba-openapi/v2/models"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"
	"github.com/tidwall/gjson"
)

// CreateCommonAPIParams 创建阿里云通用API请求参数
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

func createClient(accessKeyID, accessKeySecret, endpoint string) (*openapi.Client, error) {
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey 不能为空")
	}
	credConfig := &credentials.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     &accessKeyID,
		AccessKeySecret: &accessKeySecret,
	}
	cred, err := credentials.NewCredential(credConfig)
	if err != nil {
		return nil, fmt.Errorf("创建凭证失败: %w", err)
	}
	config := &openapi.Config{
		Credential: cred,
		Endpoint:   tea.String(endpoint),
	}
	client, err := openapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}
	return client, nil
}

// RequestCommonAPI 调用阿里云通用API（带 RPC 业务错误拦截）
func RequestCommonAPI(accessKeyID, accessKeySecret, endpoint string, params *openapi.Params, request *openapimodels.OpenApiRequest) (gjson.Result, error) {
	client, err := createClient(accessKeyID, accessKeySecret, endpoint)
	if err != nil {
		return gjson.Result{}, fmt.Errorf("客户端初始化失败: %w", err)
	}
	response, err := client.CallApi(params, request, &util.RuntimeOptions{})
	if err != nil {
		return gjson.Result{}, fmt.Errorf("API调用失败: %w", err)
	}
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

// describeDomainCertInfo 查询单个加速域名绑定的证书信息（CDN / DCDN 通用）。
// 调用方传入对应产品的 endpoint、version 与 action：
//   - CDN : endpoint=cdn.aliyuncs.com, version=2018-05-10, action=DescribeDomainCertificateInfo
//   - DCDN: endpoint=dcdn.aliyuncs.com, version=2018-01-15, action=DescribeDcdnDomainCertificateInfo
//
// 返回：HTTPS 开关（ServerCertificateStatus 为开启状态）、证书到期时间(原始字符串)、证书名、证书类型。
// 列表接口本身不返回证书信息，故对每个域名额外调一次该接口以补全 HTTPS 状态与到期时间。
// 查询失败或该域名未配置证书时返回零值，不中断整体同步。
func describeDomainCertInfo(accessKeyID, accessKeySecret, endpoint, version, action, domainName string) (httpsOn bool, certExpireTime, certName, certType string) {
	params := CreateCommonAPIParams(action, version)
	queries := map[string]any{"DomainName": tea.String(domainName)}
	request := &openapimodels.OpenApiRequest{Query: openapiutil.Query(queries)}

	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, endpoint, params, request)
	if err != nil {
		// 单域名证书查询失败（如域名无权限/不存在/接口异常）不影响整体同步，
		// 但记录日志便于排查“HTTPS状态/到期时间未获取到”的问题
		logger.Info("aliyun_cdn_cert", fmt.Sprintf("域名=%s 证书查询失败: %v", domainName, err))
		return false, "", "", ""
	}

	// 兼容多种响应结构：CertInfos.CertInfo[] / CertInfos[] / CertInfo[]，以及单对象形态
	certs := collectCertResults(jsondata.Get("body.CertInfos.CertInfo"))
	if len(certs) == 0 {
		certs = collectCertResults(jsondata.Get("body.CertInfos"))
	}
	if len(certs) == 0 {
		certs = collectCertResults(jsondata.Get("body.CertInfo"))
	}

	var statuses []string
	foundCert := false
	for _, c := range certs {
		status := c.Get("ServerCertificateStatus").String() // CDN 接口字段
		sslProtocol := c.Get("SSLProtocol").String()        // CDN/DCDN 列表与证书接口均可能返回
		statusField := c.Get("Status").String()             // 部分接口的状态字段别名
		expire := c.Get("CertExpireTime").String()
		name := c.Get("CertName").String()
		ctype := c.Get("CertType").String()
		statuses = append(statuses, status, sslProtocol, statusField)
		foundCert = true

		if isCertStatusEnabled(status) || isCertStatusEnabled(sslProtocol) || isCertStatusEnabled(statusField) {
			return true, expire, name, ctype
		}
		// 无明确开启状态字段时，保存最后的字段（httpsOn 保持 false，供前端展示到期时间）
		httpsOn, certExpireTime, certName, certType = false, expire, name, ctype
	}

	if foundCert && !httpsOn && certExpireTime != "" {
		// 证书记录存在且含到期时间，但接口未返回明确的开启状态字段
		// （如 DCDN 的 DescribeDcdnDomainCertificateInfo 只有 CertExpireTime、无 ServerCertificateStatus），
		// 此时以「证书存在」判定 HTTPS 已开启（与列表接口的 SSLProtocol=on 语义一致）。
		logger.Info("aliyun_cdn_cert", fmt.Sprintf("域名=%s 证书存在(到期=%s)但未返回明确开启状态字段, 依据证书存在判定HTTPS已开启", domainName, certExpireTime))
		httpsOn = true
	}
	return httpsOn, certExpireTime, certName, certType
}

// collectCertResults 把 gjson 结果统一转成切片（兼容数组与单对象）
func collectCertResults(r gjson.Result) []gjson.Result {
	if r.IsArray() {
		return r.Array()
	}
	if r.Exists() && r.Type != gjson.Null {
		return []gjson.Result{r}
	}
	return nil
}

// isCertStatusEnabled 判断证书状态是否代表 HTTPS 已开启
func isCertStatusEnabled(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "enable", "enabled", "open", "yes", "true", "1":
		return true
	}
	return false
}

// logCdn 把同步调试日志同时输出到三处，确保排查“无数据”时一定能看到：
//   - fmt.Println   : 进入 SSE 进度通道（前端同步进度面板可见）
//   - logger.Info   : 业务结构化日志（与后端常态 INFO 日志同一出口）
//   - os.Stderr     : runSync 会把 os.Stdout 重定向到 SSE 管道，stderr 不受影响，保证后端控制台/日志必定可见
func logCdn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Println(msg)
	logger.Info("aliyun_cdn", msg)
	fmt.Fprintln(os.Stderr, msg)
}
