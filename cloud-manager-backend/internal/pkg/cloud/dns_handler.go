package cloud

import (
	"fmt"
	"net/http"

	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun"
	awsdns "cloud-manager-backend/internal/pkg/cloud/aws/dns"
	cloudflaredns "cloud-manager-backend/internal/pkg/cloud/cloudflare/dns"
	"cloud-manager-backend/internal/pkg/cloud/tencent"
	"cloud-manager-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// resolveDnsCredential 根据前端显式传入的 platform + account（account_id 或 account_name）解析账号 AK/SK。
//
// 彻底取代原先按域名反查 dns_domains/domains 表「猜平台」的逻辑——该逻辑在一个域名被多平台
// 同时纳入（如注册在阿里云、NS 转入 Cloudflare）时，会因命中哪一行不同而「随机横跳」。
// 现在点击「解析」时前端已明确知道该域名属于哪个平台、哪个站点（账号），直接按显式值取凭证即可，
// 后端不再做任何平台猜测，各平台各自走各自的接口。
func resolveDnsCredential(platform, account string) (accessKeyID, accessKeySecret, platformName string, err error) {
	if platform == "" || account == "" {
		return "", "", "", fmt.Errorf("缺少 platform 或 account 参数（前端需显式传入域名所属平台与账号）")
	}

	var accountModel models.CloudAccount
	// 匹配顺序（注意：cloud_accounts 的账号别名字段是 name，表里没有 account_name 列）：
	//   1) account_id：业务唯一键，前端传了就用它精确命中，账号改名也不受影响
	//   2) name + platform：前端只传了账号别名时按「别名+平台」命中（别名可能跨平台重复，故带 platform）
	//   3) 仅 name：兜底，避免 platform 取值不一致（大小写/别称）时直接失败
	if err := models.DB.Where("account_id = ?", account).First(&accountModel).Error; err == nil {
		// 命中 account_id
	} else if err2 := models.DB.Where("name = ? AND platform = ?", account, platform).First(&accountModel).Error; err2 == nil {
		// 命中 name + platform
	} else if err3 := models.DB.Where("name = ?", account).First(&accountModel).Error; err3 != nil {
		return "", "", "", fmt.Errorf("未找到账号 %s（平台 %s）对应的凭证", account, platform)
	}

	secret, err := utils.Decrypt(accountModel.AccessSecret)
	if err != nil {
		return "", "", "", err
	}
	return accountModel.AccessKey, secret, platform, nil
}

// dnsPlatformAccount 从请求中读取前端显式传入的 platform + account。
// 统一走 query 参数（GET/POST/PUT/DELETE/status 均从 URL query 读取），
// 这样无需改动各云厂商 DNS handler 的请求结构体即可拿到平台与账号。
func dnsPlatformAccount(c *gin.Context) (platform, account string) {
	return c.Query("platform"), c.Query("account")
}

// GetDomainDnsRecords GET /api/domains/:domain/dns?platform=&account=
// 查询域名的DNS解析记录（实时调用云API，按前端显式传入的 platform 路由到对应平台）
func GetDomainDnsRecords(c *gin.Context) {
	domainName := c.Param("domain")
	if domainName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "域名不能为空"})
		return
	}

	platform, account := dnsPlatformAccount(c)
	accessKeyID, accessKeySecret, platform, err := resolveDnsCredential(platform, account)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到域名对应的账号信息: " + err.Error()})
		return
	}

	c.Set("dns_access_key_id", accessKeyID)
	c.Set("dns_access_key_secret", accessKeySecret)

	switch platform {
	case "aliyun":
		aliyun.DescribeDomainRecordsHandler(c)
	case "tencent":
		tencent.DescribeDomainRecordsHandler(c)
	case "cloudflare":
		cloudflaredns.DescribeRecordsHandler(c)
	case "aws":
		awsdns.DescribeRecordsHandler(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前仅支持阿里云、腾讯云、Cloudflare 和 AWS 的DNS解析操作"})
	}
}

// AddDomainDnsRecord POST /api/domains/:domain/dns?platform=&account=
// 添加DNS解析记录（按前端显式传入的 platform 路由到对应平台）
func AddDomainDnsRecord(c *gin.Context) {
	platform, account := dnsPlatformAccount(c)
	accessKeyID, accessKeySecret, platform, err := resolveDnsCredential(platform, account)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到域名对应的账号信息: " + err.Error()})
		return
	}

	c.Set("dns_access_key_id", accessKeyID)
	c.Set("dns_access_key_secret", accessKeySecret)

	switch platform {
	case "aliyun":
		aliyun.AddDomainRecordHandler(c)
	case "tencent":
		tencent.AddDomainRecordHandler(c)
	case "cloudflare":
		cloudflaredns.AddRecordHandler(c)
	case "aws":
		awsdns.AddRecordHandler(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前仅支持阿里云、腾讯云、Cloudflare 和 AWS 的DNS解析操作"})
	}
}

// UpdateDomainDnsRecord PUT /api/domains/:domain/dns?platform=&account=
// 修改DNS解析记录（按前端显式传入的 platform 路由到对应平台）
func UpdateDomainDnsRecord(c *gin.Context) {
	platform, account := dnsPlatformAccount(c)
	accessKeyID, accessKeySecret, platform, err := resolveDnsCredential(platform, account)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到域名对应的账号信息: " + err.Error()})
		return
	}

	c.Set("dns_access_key_id", accessKeyID)
	c.Set("dns_access_key_secret", accessKeySecret)

	switch platform {
	case "aliyun":
		aliyun.UpdateDomainRecordHandler(c)
	case "tencent":
		tencent.UpdateDomainRecordHandler(c)
	case "cloudflare":
		cloudflaredns.UpdateRecordHandler(c)
	case "aws":
		awsdns.UpdateRecordHandler(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前仅支持阿里云、腾讯云、Cloudflare 和 AWS 的DNS解析操作"})
	}
}

// DeleteDomainDnsRecord DELETE /api/domains/:domain/dns?platform=&account=
// 删除DNS解析记录（按前端显式传入的 platform 路由到对应平台）
func DeleteDomainDnsRecord(c *gin.Context) {
	platform, account := dnsPlatformAccount(c)
	accessKeyID, accessKeySecret, platform, err := resolveDnsCredential(platform, account)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到域名对应的账号信息: " + err.Error()})
		return
	}

	c.Set("dns_access_key_id", accessKeyID)
	c.Set("dns_access_key_secret", accessKeySecret)

	switch platform {
	case "aliyun":
		aliyun.DeleteDomainRecordHandler(c)
	case "tencent":
		tencent.DeleteDomainRecordHandler(c)
	case "cloudflare":
		cloudflaredns.DeleteRecordHandler(c)
	case "aws":
		awsdns.DeleteRecordHandler(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前仅支持阿里云、腾讯云、Cloudflare 和 AWS 的DNS解析操作"})
	}
}

// SetDomainDnsRecordStatus PUT /api/domains/:domain/dns/status?platform=&account=
// 设置DNS记录启用/禁用状态（按前端显式传入的 platform 路由到对应平台）
func SetDomainDnsRecordStatus(c *gin.Context) {
	platform, account := dnsPlatformAccount(c)
	accessKeyID, accessKeySecret, platform, err := resolveDnsCredential(platform, account)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到域名对应的账号信息: " + err.Error()})
		return
	}

	c.Set("dns_access_key_id", accessKeyID)
	c.Set("dns_access_key_secret", accessKeySecret)

	switch platform {
	case "aliyun":
		aliyun.SetDomainRecordStatusHandler(c)
	case "tencent":
		tencent.SetDomainRecordStatusHandler(c)
	case "cloudflare":
		cloudflaredns.SetStatusHandler(c)
	case "aws":
		awsdns.SetStatusHandler(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前仅支持阿里云、腾讯云、Cloudflare 和 AWS 的DNS解析操作"})
	}
}
