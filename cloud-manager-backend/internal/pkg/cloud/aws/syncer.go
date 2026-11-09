package aws

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"
	"fmt"
)

// Syncer AWS同步器，实现 cloud.CloudSyncer 接口
type Syncer struct{}

// SyncDomains 同步AWS账号下的域名注册列表（Route53 Domains → domains 表）
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
	fmt.Printf("查询 %s 账户下的域名注册列表\n", account.Name)
	ListRegisteredDomains(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, account.Site)
}

// SyncDnsDomains 同步AWS账号下的DNS可管理域名列表（Route53 托管区 → dns_domains 表）
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
	fmt.Printf("查询 %s 账户下的Route53托管区列表\n", account.Name)
	ListHostedZones(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
}

// SyncEcs 同步AWS账号下的EC2实例（使用AWS SDK）
func (s *Syncer) SyncEcs(account models.CloudAccount) {
	accessKeySecret := account.AccessSecret
	if accessKeySecret == "" {
		var err error
		accessKeySecret, err = utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			return
		}
	}

	fmt.Printf("查询 %s 账户下的EC2实例列表\n", account.Name)

	DescribeEC2InstancesAll(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, account.Site)
}

// SyncCdn 同步AWS账号下的CloudFront分配（ListDistributions API → cdn_domains 表）
func (s *Syncer) SyncCdn(account models.CloudAccount) {
	accessKeySecret := account.AccessSecret
	if accessKeySecret == "" {
		var err error
		accessKeySecret, err = utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			return
		}
	}

	fmt.Printf("查询 %s 账户下的CloudFront分配列表\n", account.Name)
	ListCloudFrontDistributions(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
}
