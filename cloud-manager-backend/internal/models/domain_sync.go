package models

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// UnifiedDomainItem 统一域名结构体，各平台API数据统一转为此结构再写入数据库
type UnifiedDomainItem struct {
	DomainName             string
	Owner                  string
	AutoRenewEnabled       bool
	RegistrationDate       string
	ExpirationDate         string
	ExpirationCurrDateDiff int
	ExpirationDateStatus   string
	Remark                 string
}

// UnifiedDnsDomainItem 统一DNS可管理域名结构体（来自阿里云 DescribeDomains API）
type UnifiedDnsDomainItem struct {
	DomainName  string
	AliDomain   bool
	Starmark    bool // 是否为付费版DNS（true=正式版, false=免费版）
	RecordCount int
	DnsServers  string // 逗号分隔
	CreateTime  string // 域名创建时间
}

// DomainRemarkSupported 判断该平台/站点的域名在云上是否提供「备注」字段。
//
// 只有阿里云国内站（platform=aliyun 且 site=cn）支持：可读取且能写回云端，
// 因此备注以云端为准、本平台修改会同步到云端。
// 阿里云国际站、AWS、Cloudflare、腾讯云等均没有域名备注这个参数：
// 云上没有该字段，本平台改的备注直接落本地库即可，云同步时也不能用空值覆盖它。
func DomainRemarkSupported(platform, site string) bool {
	return platform == "aliyun" && site == "cn"
}

// SyncDomainsToDB 将统一结构的域名同步到数据库（所有平台共用）
// 策略：DomainName + AccountID 作为唯一键
//   - 不存在 → 新增
//   - 存在且字段有变化 → 更新，is_offline 置 0
//   - 存在且字段无变化 → 只更新 updated_at
func SyncDomainsToDB(domainList []UnifiedDomainItem, accountID, accountName, platform, site string) {
	if len(domainList) == 0 {
		return
	}

	for _, item := range domainList {
		var existing Domain
		err := DB.Where("account_id = ? AND domain_name = ?", accountID, item.DomainName).First(&existing).Error

		regDate := ParseDomainTime(item.RegistrationDate)
		expDate := ParseDomainTime(item.ExpirationDate)

		if err != nil {
			// 数据库不存在 → 新增
			domain := Domain{
				AccountID:            accountID,
				Platform:             platform,
				AccountName:          accountName,
				Source:               "sync",
				DomainName:           item.DomainName,
				Owner:                item.Owner,
				AutoRenewEnabled:     item.AutoRenewEnabled,
				RegistrationDate:     regDate,
				ExpirationDate:       expDate,
				ExpirationDaysLeft:   item.ExpirationCurrDateDiff,
				ExpirationStatusCode: item.ExpirationDateStatus,
				Remark:               item.Remark,
				IsOffline:            0,
			}
			if createErr := DB.Create(&domain).Error; createErr != nil {
				fmt.Printf("  插入域名失败 [%s]: %v\n", item.DomainName, createErr)
			} else {
				fmt.Printf("  新增域名: %s\n", item.DomainName)
			}
		} else {
			// 数据库已存在 → 对比字段，有变化才更新
			needUpdate := false
			updates := map[string]interface{}{}

			// account_name / platform 可能因用户重命名云账号而变化，需同步更新
			if existing.AccountName != accountName {
				updates["account_name"] = accountName
				needUpdate = true
			}
			if existing.Platform != platform {
				updates["platform"] = platform
				needUpdate = true
			}
			if existing.Owner != item.Owner {
				updates["owner"] = item.Owner
				needUpdate = true
			}
			if existing.AutoRenewEnabled != item.AutoRenewEnabled {
				updates["auto_renew_enabled"] = item.AutoRenewEnabled
				needUpdate = true
			}
			if !domainTimePtrEqual(existing.RegistrationDate, regDate) {
				updates["registration_date"] = regDate
				needUpdate = true
			}
			if !domainTimePtrEqual(existing.ExpirationDate, expDate) {
				updates["expiration_date"] = expDate
				needUpdate = true
			}
			if existing.ExpirationDaysLeft != item.ExpirationCurrDateDiff {
				updates["expiration_days_left"] = item.ExpirationCurrDateDiff
				needUpdate = true
			}
			if existing.ExpirationStatusCode != item.ExpirationDateStatus {
				updates["expiration_status_code"] = item.ExpirationDateStatus
				needUpdate = true
			}
			// 备注以云平台为准：仅当云平台本身提供「域名备注」字段时才用云端值覆盖本地。
			// AWS / Cloudflare / 阿里云国际站 的域名在云上没有备注概念，同步结果里该字段为空，
			// 若一并覆盖会把本平台维护的备注清成空值，因此这些平台跳过备注同步，以本地为准。
			if DomainRemarkSupported(platform, site) && existing.Remark != item.Remark {
				updates["remark"] = item.Remark
				needUpdate = true
			}
			// 之前标记下线的重新上线
			if existing.IsOffline != 0 {
				updates["is_offline"] = 0
				needUpdate = true
			}

			if needUpdate {
				if updateErr := DB.Model(&existing).Updates(updates).Error; updateErr != nil {
					fmt.Printf("  更新域名失败 [%s]: %v\n", item.DomainName, updateErr)
				} else {
					fmt.Printf("  更新域名: %s\n", item.DomainName)
				}
			} else {
				// 字段无变化，只刷新 updated_at
				DB.Model(&existing).Update("updated_at", time.Now())
			}
		}
	}
}

// EnsureDomainsUnique 清理 domains 表历史重复数据，并补建 (account_id, domain_name) 唯一索引。
//
// 背景：早期版本的 domains 表没有该唯一索引（init_db.sql 的 CREATE TABLE IF NOT EXISTS
// 不会给已存在的表补索引），同步"先查后插"在索引缺失时可能插入重复行，
// 导致前端同一个域名显示两条。
//
// 策略（幂等，可每次启动执行）：
//  1. 同组 (account_id, domain_name) 中若存在手动录入行（source=manual），保留手动行、删其余；
//  2. 其余重复保留 id 最小（最早同步）的一条；
//  3. 按 information_schema 判断索引是否存在，缺失则补建（MySQL 无 CREATE INDEX IF NOT EXISTS）。
func EnsureDomainsUnique() {
	if DB == nil {
		return
	}
	// 表尚未创建（init 导入建表前）时跳过，交由 init 统一建表，避免此处操作报 “table doesn't exist”
	if !DB.Migrator().HasTable(&Domain{}) {
		return
	}
	log.Println("[migrate] 检查 domains 表唯一性（去重 + 补 (account_id,domain_name) 唯一索引）...")

	hasSourceCol := DB.Migrator().HasColumn(&Domain{}, "Source")

	// 1) 手动录入优先保留（仅当表已有 source 列）
	if hasSourceCol {
		if res := DB.Exec(`DELETE d1 FROM domains d1
			JOIN domains d2 ON d1.account_id = d2.account_id AND d1.domain_name = d2.domain_name
			WHERE d1.source <> 'manual' AND d2.source = 'manual'`); res.RowsAffected > 0 {
			log.Printf("[migrate] domains 清理与手动录入重复的同步行 %d 条", res.RowsAffected)
		}
	}

	// 2) 保留每组 id 最小的一条
	if res := DB.Exec(`DELETE d1 FROM domains d1
		JOIN domains d2 ON d1.account_id = d2.account_id AND d1.domain_name = d2.domain_name AND d1.id > d2.id`); res.RowsAffected > 0 {
		log.Printf("[migrate] domains 清理重复数据 %d 行（每组保留最早一条）", res.RowsAffected)
	}

	// 3) 补建唯一索引（MySQL 不支持 CREATE INDEX IF NOT EXISTS，先查 information_schema）
	var idxCount int64
	DB.Raw(`SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = 'domains' AND index_name = 'idx_account_domain'`).Scan(&idxCount)
	if idxCount == 0 {
		if err := DB.Exec(`ALTER TABLE domains ADD UNIQUE KEY idx_account_domain (account_id, domain_name)`).Error; err != nil {
			log.Printf("[migrate] domains 补建唯一索引 idx_account_domain 失败: %v", err)
		} else {
			log.Println("[migrate] 已为 domains 补建唯一索引 idx_account_domain (account_id, domain_name)")
		}
	}
}

// EnsureTablesUtf8mb4 幂等地将核心业务表字符集纠正为 utf8mb4。
//
// 背景：早期建的表可能非 utf8mb4（即使连接 DSN 已指定 charset=utf8mb4），
// 中文在写入时会被 MySQL 替换为 '?'，造成不可逆乱码。
// 仅当表当前 collation 不是 utf8mb4 前缀时才执行 ALTER TABLE CONVERT，避免每次启动都锁表重建。
//
// 注意：CONVERT 只能让表“能存”中文，已损坏成 '?' 的历史数据无法恢复，
// 需在对应用例重新同步（如域名/记录）后由云上真实数据覆盖。
func EnsureTablesUtf8mb4() {
	if DB == nil {
		return
	}
	log.Println("[migrate] 检查核心业务表字符集（非 utf8mb4 将自动 ALTER 转换）...")
	var dbName string
	if err := DB.Raw("SELECT DATABASE()").Scan(&dbName).Error; err != nil || dbName == "" {
		log.Printf("[migrate] 获取当前数据库名失败，跳过字符集检查: %v", err)
		return
	}
	tables := []string{
		"domains", "dns_domains", "ecs_instances", "cdn_domains", "cloud_accounts",
		"bastion_credentials", "bastion_group_assets", "bastion_group_credentials",
		"users", "user_groups", "user_group_members", "cloud_regions",
	}
	for _, t := range tables {
		var coll string
		if err := DB.Raw(`SELECT TABLE_COLLATION FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, dbName, t).Scan(&coll).Error; err != nil || coll == "" {
			continue // 表不存在或查询失败则跳过
		}
		if !strings.HasPrefix(coll, "utf8mb4") {
			if err := DB.Exec(fmt.Sprintf("ALTER TABLE `%s` CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", t)).Error; err != nil {
				log.Printf("[migrate] 表 %s 转 utf8mb4 失败: %v", t, err)
			} else {
				log.Printf("[migrate] 已将表 %s 字符集转为 utf8mb4（原 collation: %s）", t, coll)
			}
		}
	}
}

// ParseDomainTime 解析时间字符串，支持多种格式，空字符串返回 nil
func ParseDomainTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	formats := []string{
		time.RFC3339,                    // 2006-01-02T15:04:05Z07:00
		"2006-01-02T15:04:05.000Z07:00", // 带毫秒和时区
		"2006-01-02T15:04:05Z",          // 不带时区偏移的ISO8601
		"2006-01-02T15:04:05.000Z",      // 带毫秒不带时区偏移
		"2006-01-02T15:04Z",             // 阿里云ECS精简格式：2024-04-09T05:29Z（无秒）
		"Jan 2,2006 15:04:05",           // 阿里云英文月格式：Jul 23,2024 02:44:56
		"2006-01-02 15:04:05.000",       // 腾讯云域名格式：2027-07-04 08:00:00.000
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006-01-02T15:04:05+08:00",
	}
	for _, layout := range formats {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	fmt.Printf("  [WARN] 无法解析时间: %s\n", s)
	return nil
}

// domainTimePtrEqual 比较两个 *time.Time 是否相等（nil == nil 视为相等）
func domainTimePtrEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

// SyncDnsDomainsToDB 将DNS可管理域名同步到 dns_domains 表（全量替换策略）
func SyncDnsDomainsToDB(domainList []UnifiedDnsDomainItem, accountID, accountName, platform string) {
	// 先删除该账号下的所有旧记录
	DB.Where("account_id = ? AND platform = ?", accountID, platform).Delete(&DnsDomain{})

	seen := make(map[string]bool) // 去重：避免同一次同步里 API 返回重复域名导致唯一键冲突
	for _, item := range domainList {
		if seen[item.DomainName] {
			continue
		}
		seen[item.DomainName] = true
		createTime := ParseDomainTime(item.CreateTime)
		dnsDomain := DnsDomain{
			AccountID:   accountID,
			Platform:    platform,
			AccountName: accountName,
			DomainName:  item.DomainName,
			AliDomain:   item.AliDomain,
			Starmark:    item.Starmark,
			RecordCount: item.RecordCount,
			DnsServers:  item.DnsServers,
			CreateTime:  createTime,
		}
		if err := DB.Create(&dnsDomain).Error; err != nil {
			fmt.Printf("  插入DNS域名失败 [%s]: %v\n", item.DomainName, err)
		} else {
			fmt.Printf("  新增DNS域名: %s (AliDomain=%v, Starmark=%v, RecordCount=%d)\n", item.DomainName, item.AliDomain, item.Starmark, item.RecordCount)
		}
	}
	fmt.Printf("DNS域名同步完成: 账号=%s, 共 %d 条\n", accountName, len(domainList))
}
