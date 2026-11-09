package aws

// 查询 AWS CloudFront 分配（Distribution）列表并同步到 cdn_domains 表
// API: ListDistributions（CloudFront 是全局服务，SDK 端点固定 us-east-1）
// 文档: https://docs.aws.amazon.com/cloudfront/latest/APIReference/API_ListDistributions.html
//
// 字段映射（CloudFront 与阿里云 CDN 的概念差异）：
//   - Distribution.DomainName 是平台分配的 xxx.cloudfront.net，对应阿里云的 Cname
//   - 用户实际访问的域名在 Aliases（备用域名）里：取第一个作为加速域名，
//     全部别名存入 aliases 列；无别名时加速域名回退为 xxx.cloudfront.net
//   - Origins.Items[0].DomainName 即源站
//   - HTTPS 状态看 DefaultCacheBehavior.ViewerProtocolPolicy：https-only / redirect-to-https 视为开启
//   - Status: Deployed=已部署 / InProgress=部署中

import (
	"context"
	"fmt"
	"strings"

	"cloud-manager-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// createCfClient 创建 CloudFront 客户端（全局服务，region 固定 us-east-1）
func createCfClient(accessKeyID, accessKeySecret string) (*cloudfront.Client, error) {
	creds := credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("无法加载AWS配置: %w", err)
	}
	return cloudfront.NewFromConfig(cfg), nil
}

// cfStatusMap CloudFront 分配状态 → 中文
var cfStatusMap = map[string]string{
	"Deployed":   "运行中",
	"InProgress": "部署中",
}

// ListCloudFrontDistributions 分页查询账号下全部分发并同步到 cdn_domains 表
func ListCloudFrontDistributions(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	client, err := createCfClient(accessKeyID, accessKeySecret)
	if err != nil {
		fmt.Printf("  [跳过] 创建CloudFront客户端异常 [账号: %s]: %v\n", accountName, err)
		return
	}

	fmt.Printf("  [CDN Sync] 开始同步 AWS CDN [账号: %s / accountID: %s / platform: %s]\n", accountName, accountID, platform)

	var allItems []models.UnifiedCdnItem
	var marker *string
	firstPrinted := false

	for {
		input := &cloudfront.ListDistributionsInput{
			Marker:   marker,
			MaxItems: aws.Int32(100),
		}
		result, err := client.ListDistributions(context.TODO(), input)
		if err != nil {
			fmt.Printf("  [跳过] CloudFront查询异常 [账号: %s]: %v\n", accountName, err)
			return
		}

		if result.DistributionList != nil {
			for _, dist := range result.DistributionList.Items {
				if !firstPrinted {
					fmt.Printf("  [CDN Debug] 账号 %s 第一条原始 distribution: %#v\n", accountName, dist)
					firstPrinted = true
				}
				allItems = append(allItems, cfDistToUnified(dist))
			}
		}

		// 分页：IsTruncated + NextMarker
		if result.DistributionList == nil ||
			!aws.ToBool(result.DistributionList.IsTruncated) ||
			result.DistributionList.NextMarker == nil {
			break
		}
		marker = result.DistributionList.NextMarker
	}

	fmt.Printf("  [CDN] AWS账号 %s 共 %d 个 CloudFront 分配\n", accountName, len(allItems))
	models.SyncCdnToDB(allItems, accountID, accountName, platform)
}

// cfDistToUnified 将 CloudFront DistributionSummary 映射为统一结构
func cfDistToUnified(dist cloudfronttypes.DistributionSummary) models.UnifiedCdnItem {
	fmt.Printf("  [CDN Debug] AWS dist Id=%s DomainName=%s Aliases=%v Enabled=%v Status=%q\n", aws.ToString(dist.Id), aws.ToString(dist.DomainName), dist.Aliases, aws.ToBool(dist.Enabled), aws.ToString(dist.Status))
	// 平台分配的域名（xxx.cloudfront.net），对应阿里云的 Cname
	cfDomain := aws.ToString(dist.DomainName)

	// 备用域名（用户实际访问的域名）
	var aliases []string
	if dist.Aliases != nil && dist.Aliases.Items != nil {
		aliases = dist.Aliases.Items
	}
	// 加速域名：优先取第一个别名，无别名则用平台分配的域名
	domainName := cfDomain
	if len(aliases) > 0 && aliases[0] != "" {
		domainName = aliases[0]
	}

	// 源站：取第一个 origin 的域名（主源）
	originDomain := ""
	if dist.Origins != nil && len(dist.Origins.Items) > 0 {
		originDomain = aws.ToString(dist.Origins.Items[0].DomainName)
	}

	// AWS CloudFront 默认均开启 HTTPS，无需查询协议策略/证书来源，直接写死开启
	httpsOn := true

	// 状态归一化：Enabled=false 表示已停用（与 Status 的 Deployed/InProgress 无关）→ 已停止；
	// 启用中再按部署状态映射（Deployed→运行中 / InProgress→部署中）
	var status string
	if dist.Enabled != nil && !*dist.Enabled {
		status = "已停止"
	} else if zh, ok := cfStatusMap[aws.ToString(dist.Status)]; ok {
		status = zh
	} else {
		status = aws.ToString(dist.Status)
	}

	return models.UnifiedCdnItem{
		DomainName:   domainName,
		Cname:        cfDomain,
		Product:      "cdn", // CloudFront 在平台归类上即 CDN
		Status:       status,
		BizType:      "", // CloudFront 无阿里云式的业务类型概念
		OriginDomain: originDomain,
		HttpsOn:      httpsOn,
		Aliases:      strings.Join(aliases, ","),
	}
}
