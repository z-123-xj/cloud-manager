package aws

// 同步 AWS Route53 托管区（Hosted Zone）到 dns_domains 表
// API: ListHostedZones（Route53 为全球服务，SDK 端点固定 us-east-1）

import (
	"context"
	"fmt"
	"strings"

	"cloud-manager-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

// newRoute53Client 创建 Route53 客户端（全球服务，region 固定 us-east-1）
func newRoute53Client(accessKeyID, accessKeySecret string) (*route53.Client, error) {
	creds := credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("无法加载AWS配置: %w", err)
	}
	return route53.NewFromConfig(cfg), nil
}

// ListHostedZones 分页查询账号下所有 Route53 托管区并同步到 dns_domains 表
func ListHostedZones(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	client, err := newRoute53Client(accessKeyID, accessKeySecret)
	if err != nil {
		fmt.Printf("  [跳过] 创建Route53客户端异常 [账号: %s]: %v\n", accountName, err)
		return
	}

	fmt.Printf("查询 %s 账户下的Route53托管区列表\n", accountName)

	var items []models.UnifiedDnsDomainItem
	var marker *string
	for {
		out, err := client.ListHostedZones(context.TODO(), &route53.ListHostedZonesInput{
			Marker:   marker,
			MaxItems: aws.Int32(100),
		})
		if err != nil {
			fmt.Printf("  [跳过] Route53查询托管区异常 [账号: %s]: %v\n", accountName, err)
			return
		}
		for _, z := range out.HostedZones {
			// 托管区名称带结尾点（example.com.），去掉
			name := strings.TrimSuffix(aws.ToString(z.Name), ".")
			if name == "" {
				continue
			}
			// 打印 ListHostedZones 原始数据，便于排查
			fmt.Printf("  [DNS域名-AWS] 托管区原始数据: Name=%s Id=%s RecordCount=%d\n",
				aws.ToString(z.Name), aws.ToString(z.Id), aws.ToInt64(z.ResourceRecordSetCount))

			// 权威名称服务器（NS）：ListHostedZones 不返回，需逐个 GetHostedZone 取 DelegationSet.NameServers
			nsServers := ""
			if z.Id != nil {
				hz, herr := client.GetHostedZone(context.TODO(), &route53.GetHostedZoneInput{Id: z.Id})
				if herr != nil {
					fmt.Printf("  [DNS域名-AWS] 获取 %s 的 NS 失败: %v\n", name, herr)
				} else if hz.DelegationSet != nil {
					nsServers = strings.Join(hz.DelegationSet.NameServers, ",")
					// 打印 GetHostedZone 返回的原始 NS，确认是否取到
					fmt.Printf("  [DNS域名-AWS] %s (Id=%s) 原始NS=%v\n", name, aws.ToString(z.Id), hz.DelegationSet.NameServers)
				} else {
					fmt.Printf("  [DNS域名-AWS] %s (Id=%s) GetHostedZone 返回的 DelegationSet 为空\n", name, aws.ToString(z.Id))
				}
			} else {
				fmt.Printf("  [DNS域名-AWS] %s 的托管区 Id 为空，跳过取 NS\n", name)
			}
			items = append(items, models.UnifiedDnsDomainItem{
				DomainName: name,
				// AWS 无"阿里云域名"返回字段，用 NS 是否在 Route53 判断解析是否托管在 AWS
				AliDomain: strings.Contains(nsServers, "awsdns"),
				Starmark:  false, // AWS 无付费/免费DNS版本概念
				RecordCount: int(aws.ToInt64(z.ResourceRecordSetCount)),
				DnsServers:  nsServers,
				CreateTime:  "", // Route53 托管区不在列表接口暴露创建时间
			})
		}
		if out.NextMarker == nil || aws.ToString(out.NextMarker) == "" {
			break
		}
		marker = out.NextMarker
	}

	fmt.Printf("  [DNS域名-AWS] 账号 %s 共 %d 个托管区\n", accountName, len(items))
	models.SyncDnsDomainsToDB(items, accountID, accountName, platform)
}
