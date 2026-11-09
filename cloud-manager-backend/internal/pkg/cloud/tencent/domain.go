package tencent

import (
	"cloud-manager-backend/internal/models"
	"fmt"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	domain "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/domain/v20180808"
)

// DescribeDomainList 查询腾讯云账号下的域名列表（分页）
// 返回: totalPageNum（总页数）
func DescribeDomainList(secretID, secretKey, accountID, accountName, platform, site string, offset, limit uint64) int64 {
	credential := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "domain.tencentcloudapi.com"

	client, err := domain.NewClient(credential, "", cpf)
	if err != nil {
		fmt.Printf("  [跳过] 创建腾讯云域名客户端失败 [账号: %s]: %v\n", accountName, err)
		return 0
	}

	request := domain.NewDescribeDomainNameListRequest()
	request.Offset = common.Uint64Ptr(offset)
	request.Limit = common.Uint64Ptr(limit)

	response, err := client.DescribeDomainNameList(request)
	if err != nil {
		fmt.Printf("  [跳过] 查询域名列表失败 [账号: %s, offset=%d]: %v\n", accountName, offset, err)
		return 0
	}

	totalCount := uint64(0)
	if response.Response.TotalCount != nil {
		totalCount = *response.Response.TotalCount
	}
	totalPages := int64((totalCount + limit - 1) / limit)
	if limit == 0 {
		totalPages = 0
	}

	domains := response.Response.DomainSet
	if len(domains) == 0 {
		fmt.Println("  没有查询到域名")
		return totalPages
	}

	fmt.Printf("  总条数: %d, 总页数: %d, 当前offset: %d\n", totalCount, totalPages, offset)

	// 转为统一结构体并同步到数据库
	var unified []models.UnifiedDomainItem
	for _, d := range domains {
		item := models.UnifiedDomainItem{
			DomainName: safeString(d.DomainName),
		}

		// AutoRenew: 0=未设置, 1=已设置自动续费, 2=已关闭
		if d.AutoRenew != nil {
			item.AutoRenewEnabled = (*d.AutoRenew == 1)
		}

		// CreationDate 格式: YYYY-MM-DD HH:mm:ss
		if d.CreationDate != nil && *d.CreationDate != "" {
			item.RegistrationDate = *d.CreationDate
		}

		// ExpirationDate 格式: YYYY-MM-DD HH:mm:ss（腾讯云返回北京时间 UTC+8）
		if d.ExpirationDate != nil && *d.ExpirationDate != "" {
			item.ExpirationDate = *d.ExpirationDate
			expDate := models.ParseDomainTime(*d.ExpirationDate)
			if expDate != nil {
				item.ExpirationCurrDateDiff = int(time.Until(*expDate).Hours() / 24)
			}
		}

		unified = append(unified, item)
	}

	models.SyncDomainsToDB(unified, accountID, accountName, platform, site)

	return totalPages
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// safeUint64Ptr 安全获取 uint64 指针的值
func safeUint64Ptr(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}

// createDNSPodProfile 创建DNSPod专用客户端配置
func createDNSPodProfile() *profile.ClientProfile {
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	return cpf
}

// joinStrings 将字符串切片用分隔符连接
func joinStrings(list []string, sep string) string {
	return strings.Join(list, sep)
}
