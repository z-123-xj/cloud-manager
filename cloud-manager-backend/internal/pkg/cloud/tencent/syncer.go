package tencent

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"
	"fmt"
)

// Syncer 腾讯云同步器，实现 cloud.CloudSyncer 接口
type Syncer struct{}

// SyncDomains 同步腾讯云账号下的域名注册列表
func (s *Syncer) SyncDomains(account models.CloudAccount) {
	accessKeySecret := account.AccessSecret
	if accessKeySecret == "" {
		var err error
		accessKeySecret, err = utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			return
		}
	}

	fmt.Printf("查询 %s 账户下的域名列表\n", account.Name)

	pageSize := uint64(20)
	totalPages := DescribeDomainList(account.AccessKey, accessKeySecret,
		account.AccountID, account.Name, account.Platform, account.Site, 0, pageSize)

	for i := uint64(1); i < uint64(totalPages); i++ {
		offset := i * pageSize
		DescribeDomainList(account.AccessKey, accessKeySecret,
			account.AccountID, account.Name, account.Platform, account.Site, offset, pageSize)
	}
}

// SyncDnsDomains 同步腾讯云账号下的DNS可管理域名列表（DNSPod API → dns_domains 表）
func (s *Syncer) SyncDnsDomains(account models.CloudAccount) {
	accessKeySecret := account.AccessSecret
	if accessKeySecret == "" {
		var err error
		accessKeySecret, err = utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			return
		}
	}

	fmt.Printf("查询 %s 账户下DNSPod可管理的域名列表\n", account.Name)
	QueryDnsDomainList(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
}

// SyncEcs 同步腾讯云账号下的ECS实例（暂未实现）
func (s *Syncer) SyncEcs(account models.CloudAccount) {
	fmt.Printf("腾讯云 ECS 同步暂未实现，跳过账号: %s\n", account.Name)
}

// SyncCdn 同步腾讯云账号下的CDN加速域名（暂未实现）
func (s *Syncer) SyncCdn(account models.CloudAccount) {
	fmt.Printf("腾讯云 CDN 同步暂未实现，跳过账号: %s\n", account.Name)
}
