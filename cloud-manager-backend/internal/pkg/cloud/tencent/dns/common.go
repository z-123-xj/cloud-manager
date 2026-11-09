package dns

// DNS操作公共模块：常量、凭证获取、客户端创建

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ==================== 常量定义 ====================

const dnsEndpoint = "dnspod.tencentcloudapi.com" // 腾讯云DNSPod服务端点

// ==================== 凭证获取 ====================

// getCredentialFromContext 从 gin context 获取DNS操作的AK/SK凭证
// 凭证由上层 cloud.dns_handler.go 在调用前设置
func getCredentialFromContext(c *gin.Context) (string, string) {
	akID, _ := c.Get("dns_access_key_id")
	akSecret, _ := c.Get("dns_access_key_secret")
	return fmt.Sprintf("%v", akID), fmt.Sprintf("%v", akSecret)
}

// ==================== 客户端创建 ====================

// createClient 创建腾讯云DNSPod API客户端
func createClient(secretID, secretKey string) (*dnspod.Client, error) {
	if secretID == "" || secretKey == "" {
		return nil, fmt.Errorf("AccessKey 不能为空")
	}

	credential := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = dnsEndpoint

	client, err := dnspod.NewClient(credential, "", cpf)
	if err != nil {
		return nil, fmt.Errorf("创建DNSPod客户端失败: %w", err)
	}

	return client, nil
}

// ==================== 线路类型映射 ====================

// 前端使用阿里云风格的线路标识（default/internal/oversea）
// DNSPod 使用中文线路名称，需要做双向映射

// lineToDNSPod 将前端线路标识映射为DNSPod的中文线路名
func lineToDNSPod(line string) string {
	switch line {
	case "default":
		return "默认"
	case "internal":
		return "境内"
	case "oversea":
		return "境外"
	default:
		return "默认"
	}
}

// lineFromDNSPod 将DNSPod的中文线路名映射为前端线路标识
func lineFromDNSPod(line string) string {
	switch line {
	case "默认":
		return "default"
	case "境内":
		return "internal"
	case "境外":
		return "oversea"
	default:
		// 其他未知线路统一返回 original
		if line == "" {
			return "default"
		}
		return line
	}
}

// ==================== 辅助函数 ====================

// safeString 安全获取字符串指针的值
func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// safeUint64 安全获取 uint64 指针的值，返回 int64
func safeUint64(v *uint64) int64 {
	if v == nil {
		return 0
	}
	return int64(*v)
}
