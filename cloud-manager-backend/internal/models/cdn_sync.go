package models

import (
	"fmt"
)

// UnifiedCdnItem 统一CDN域名结构体（阿里云 CDN / DCDN、AWS CloudFront 共用）
type UnifiedCdnItem struct {
	DomainName     string // 加速域名（用户访问的域名；AWS 无别名时用 xxx.cloudfront.net）
	Cname          string // 平台分配的加速域名（阿里云 .cdn.dnsv1.com / AWS xxx.cloudfront.net）
	Product        string // 产品类型: cdn=CDN / dcdn=全站加速（阿里云独有）
	Status         string // 运行状态（已归一化为中文）
	BizType        string // 业务类型
	OriginDomain   string // 源站地址
	HttpsOn        bool   // 是否开启 HTTPS（由证书 ServerCertificateStatus 赋值）
	CertName       string // 绑定证书名称（阿里云）
	CertType       string // 证书类型：free/paid 等（阿里云）
	CertExpireTime string // 证书到期时间（原始字符串）
	Aliases        string // AWS 全部备用域名（逗号分隔）
}

// SyncCdnToDB 将统一结构的CDN域名同步到数据库
// 策略：CDN 域名是云控制台的实时快照（无本地维护字段），采用"先删后插"整账号刷新，
// 与 dns_domains 的同步策略一致——云上删除的域名本地也随之消失，无需 is_offline 标记。
// 删除与插入放在同一事务，避免中途失败留下空窗期。
func SyncCdnToDB(domainList []UnifiedCdnItem, accountID, accountName, platform string) {
	tx := DB.Begin()
	if tx.Error != nil {
		fmt.Printf("CDN同步事务开启失败 [账号: %s]: %v\n", accountName, tx.Error)
		return
	}

	// 收集本次同步涉及的产品类型，仅清理这些产品的旧记录，
	// 避免同账号下不同产品（如 CDN 与 DCDN 全站加速）相互覆盖/清空
	productSet := map[string]bool{}
	for _, item := range domainList {
		if item.Product != "" {
			productSet[item.Product] = true
		}
	}
	if len(productSet) == 0 {
		tx.Rollback()
		fmt.Printf("CDN同步无有效产品类型，跳过清理 [账号: %s]\n", accountName)
		return
	}
	products := make([]string, 0, len(productSet))
	for p := range productSet {
		products = append(products, p)
	}

	// 先删除该账号该平台下、本次涉及产品的全部旧记录（整产品刷新）
	if err := tx.Where("account_id = ? AND platform = ? AND product IN ?", accountID, platform, products).Delete(&CdnDomain{}).Error; err != nil {
		tx.Rollback()
		fmt.Printf("CDN同步清理旧数据失败 [账号: %s]: %v\n", accountName, err)
		return
	}

	for _, item := range domainList {
		record := CdnDomain{
			AccountID:      accountID,
			Platform:       platform,
			AccountName:    accountName,
			DomainName:     item.DomainName,
			Cname:          item.Cname,
			Product:        item.Product,
			Status:         item.Status,
			BizType:        item.BizType,
			OriginDomain:   item.OriginDomain,
			HttpsOn:        item.HttpsOn,
			CertName:       item.CertName,
			CertType:       item.CertType,
			CertExpireTime: item.CertExpireTime,
			Aliases:        item.Aliases,
		}
		if err := tx.Create(&record).Error; err != nil {
			tx.Rollback()
			fmt.Printf("CDN同步写入失败 [账号: %s, 域名: %s]: %v\n", accountName, item.DomainName, err)
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		fmt.Printf("CDN同步事务提交失败 [账号: %s]: %v\n", accountName, err)
	}
}
