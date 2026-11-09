package cloudflare

// Syncer Cloudflare 同步器，实现 cloud.CloudSyncer 接口
//
// 认证模型：AccessKey = Cloudflare Account ID（用于按账户过滤 zones）
//           AccessSecret = API Token（Bearer，需要 Zone:Read 权限）
//
// 数据流向说明：Cloudflare 是 DNS 服务商而非域名注册商——zone 里的域名都是从其他
// 注册商（阿里云/腾讯云/GoDaddy 等）注册、把 NS 改到 Cloudflare 的，没有到期/续费
// 概念。因此同步目标是 DNS 可管理域名列表，而非注册域名列表：
//   - SyncDomains（domains 注册域名列表）：跳过，CF 不卖域名
//   - SyncDnsDomains（dns_domains DNS 可管理域名列表）：执行，zones 即 CF 的 DNS 管理对象
//   - SyncEcs：跳过，CF 无 ECS 概念

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"
	"fmt"
)

type Syncer struct{}

// SyncDomains 跳过：Cloudflare 无注册域名概念（zones 同步至 dns_domains，见 SyncDnsDomains）
func (s *Syncer) SyncDomains(account models.CloudAccount) {
	fmt.Printf("[跳过] Cloudflare 无注册域名概念（域名均在其它注册商注册、NS 转入），账号: %s\n", account.Name)
}

// SyncDnsDomains 同步 Cloudflare 账户下的 Zones 到 dns_domains（DNS 可管理域名列表）
func (s *Syncer) SyncDnsDomains(account models.CloudAccount) {
	// account.AccessSecret 已在调用方（SyncAllResources）解密
	token := account.AccessSecret
	if token == "" {
		// 兼容：如果未解密，自行解密
		var err error
		token, err = utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			return
		}
	}
	if account.AccessKey == "" {
		fmt.Printf("[跳过] Cloudflare 账号未填写 Account ID（AccessKey），无法按账户过滤: %s\n", account.Name)
	}

	fmt.Printf("查询 %s 账户（Cloudflare）下的 DNS 域名列表\n", account.Name)
	ListDnsDomains(token, account.AccessKey, account.AccountID, account.Name, account.Platform)
}

// SyncEcs Cloudflare 无 ECS 实例概念，跳过
func (s *Syncer) SyncEcs(account models.CloudAccount) {
	fmt.Printf("[跳过] Cloudflare 无ECS实例，账号: %s\n", account.Name)
}

// SyncCdn Cloudflare 无独立 CDN 产品概念（代理即 CDN，zone 信息已在 dns_domains 体现），跳过
func (s *Syncer) SyncCdn(account models.CloudAccount) {
	fmt.Printf("[跳过] Cloudflare 无独立CDN概念（代理状态已在DNS域名列表），账号: %s\n", account.Name)
}
