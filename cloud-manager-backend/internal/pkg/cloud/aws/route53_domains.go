package aws

// 同步 AWS Route53 Domains 已注册域名到 domains 表
// API: ListDomains（Route53 Domains 为全球服务，SDK 端点固定 us-east-1）

import (
	"context"
	"fmt"
	"time"

	"cloud-manager-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/route53domains"
)

// newRoute53DomainsClient 创建 Route53 Domains 客户端（全球服务，region 固定 us-east-1）
func newRoute53DomainsClient(accessKeyID, accessKeySecret string) (*route53domains.Client, error) {
	creds := credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("无法加载AWS配置: %w", err)
	}
	return route53domains.NewFromConfig(cfg), nil
}

// ListRegisteredDomains 分页查询账号下所有已注册域名（Route53 Domains）并同步到 domains 表
func ListRegisteredDomains(accessKeyID, accessKeySecret, accountID, accountName, platform, site string) {
	client, err := newRoute53DomainsClient(accessKeyID, accessKeySecret)
	if err != nil {
		fmt.Printf("  [跳过] 创建Route53Domains客户端异常 [账号: %s]: %v\n", accountName, err)
		return
	}

	fmt.Printf("查询 %s 账户下的域名注册列表（Route53 Domains）\n", accountName)

	var items []models.UnifiedDomainItem
	var marker *string
	for {
		out, err := client.ListDomains(context.TODO(), &route53domains.ListDomainsInput{
			Marker: marker,
		})
		if err != nil {
			fmt.Printf("  [跳过] Route53Domains查询异常 [账号: %s]: %v\n", accountName, err)
			return
		}
		for _, d := range out.Domains {
			name := aws.ToString(d.DomainName)
			if name == "" {
				continue
			}
			item := models.UnifiedDomainItem{
				DomainName:       name,
				Owner:            "", // Route53 Domains 对注册人信息隐私保护，列表摘要不返回
				AutoRenewEnabled: aws.ToBool(d.AutoRenew),
			}
			if d.Expiry != nil {
				exp := *d.Expiry
				item.ExpirationDate = exp.Format("2006-01-02")
				days := int(time.Until(exp).Hours() / 24)
				item.ExpirationCurrDateDiff = days
				switch {
				case days < 0:
					item.ExpirationDateStatus = "expired"
				case days <= 30:
					item.ExpirationDateStatus = "expiring"
				default:
					item.ExpirationDateStatus = "normal"
				}
			}
			items = append(items, item)
		}
		if out.NextPageMarker == nil || aws.ToString(out.NextPageMarker) == "" {
			break
		}
		marker = out.NextPageMarker
	}

	fmt.Printf("  [域名-AWS] 账号 %s 共 %d 个注册域名\n", accountName, len(items))
	models.SyncDomainsToDB(items, accountID, accountName, platform, site)
}
