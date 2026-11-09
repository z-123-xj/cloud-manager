package cloud

// 统一数据库写入函数，所有云平台的 ECS 和域名都通过此文件代理到 models 包
// 各平台只需将 API 数据转为 models 中的统一结构体，然后调用 models 中的 Sync* 函数

import (
	"cloud-manager-backend/internal/models"
)

// ==================== 域名同步（代理到 models.SyncDomainsToDB） ====================

// SyncDomainsToDB 将统一结构的域名同步到数据库（代理到 models.SyncDomainsToDB）
// 所有平台统一调用此函数
func SyncDomainsToDB(domainList []models.UnifiedDomainItem, accountID, accountName, platform, site string) {
	models.SyncDomainsToDB(domainList, accountID, accountName, platform, site)
}

// SyncDnsDomainsToDB 将DNS可管理域名同步到 dns_domains 表（代理到 models.SyncDnsDomainsToDB）
func SyncDnsDomainsToDB(domainList []models.UnifiedDnsDomainItem, accountID, accountName, platform string) {
	models.SyncDnsDomainsToDB(domainList, accountID, accountName, platform)
}

// ==================== ECS同步（代理到 models.SyncEcsToDB） ====================

// SyncEcsToDB 将统一结构的ECS实例同步到数据库（代理到 models.SyncEcsToDB）
// 所有平台统一调用此函数
func SyncEcsToDB(ecsList []models.UnifiedEcsItem, accountID, accountName, platform, site string) {
	models.SyncEcsToDB(ecsList, accountID, accountName, platform, site)
}
