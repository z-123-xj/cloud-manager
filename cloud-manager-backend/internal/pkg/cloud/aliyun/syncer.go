package aliyun

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun/cdn"
	"cloud-manager-backend/pkg/utils"
	"fmt"
)

// Syncer 阿里云同步器，实现 cloud.CloudSyncer 接口
type Syncer struct{}

// SyncDomains 同步阿里云账号下的域名（只做数据拉取和写入，不做下线标记）
func (s *Syncer) SyncDomains(account models.CloudAccount) {
	// account.AccessSecret 已在调用方解密
	accessKeySecret := account.AccessSecret
	if accessKeySecret == "" {
		// 兼容：如果未解密，自行解密
		var err error
		accessKeySecret, err = utils.Decrypt(account.AccessSecret)
		if err != nil {
			fmt.Printf("解密 AccessSecret 失败 [%s]: %v\n", account.Name, err)
			return
		}
	}

	fmt.Printf("查询 %s 账户（site=%s）下的域名列表\n", account.Name, account.Site)

	PageSize := 20
	// 按站点分发到国内站/国际站各自的实现
	var totalPages int
	if account.Site == "intl" {
		totalPages = int(QueryDomainListIntl(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, 1, PageSize))
	} else {
		totalPages = int(QueryDomainListCN(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, 1, PageSize))
	}

	// 查询后面页数数据
	for i := 2; i <= totalPages; i++ {
		if account.Site == "intl" {
			QueryDomainListIntl(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, i, PageSize)
		} else {
			QueryDomainListCN(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, i, PageSize)
		}
	}

}

// SyncDnsDomains 同步阿里云账号下的DNS可管理域名列表（DescribeDomains API → dns_domains 表）
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

	fmt.Printf("查询 %s 账户下DNS可管理的域名列表\n", account.Name)
	if account.Site == "intl" {
		QueryDnsDomainListIntl(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
	} else {
		QueryDnsDomainListCN(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
	}
}

// SyncEcs 同步阿里云账号下的ECS实例（只做数据拉取和写入，不做下线标记）
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

	fmt.Printf("查询 %s 账户下的ECS实例列表\n", account.Name)

	if account.Site == "intl" {
		DescribeInstancesAllIntl(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
	} else {
		DescribeInstancesAllCN(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform)
	}
}

// SyncCdn 同步阿里云账号下的加速域名（CDN + DCDN全站加速，均 → cdn_domains 表）
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

	logCdn("查询 %s 账户（site=%s）下的CDN/DCDN加速域名列表", account.Name, account.Site)
	// CDN（内容分发网络）
	cdn.QueryCdnDomainList(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, account.Site)
	// DCDN（全站加速）：国内站与国际站均尝试拉取。国际站若不可用会在调用时报错并跳过，
	// 由 sync.go 的 recover 隔离，不影响其它账号/资源。
	cdn.QueryDcdnDomainList(account.AccessKey, accessKeySecret, account.AccountID, account.Name, account.Platform, account.Site)
}
