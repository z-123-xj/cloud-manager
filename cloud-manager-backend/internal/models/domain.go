package models

import "time"

// Domain 域名模型（对应 domains 表）
type Domain struct {
	ID                   uint64     `json:"id" gorm:"primaryKey"`                                                         // 主键ID
	AccountID            string     `json:"account_id" gorm:"type:varchar(50);not null;uniqueIndex:idx_account_domain"`   // 关联 cloud_accounts.account_id（与 domain_name 联合唯一）
	Platform             string     `json:"platform" gorm:"type:varchar(50);not null;index:idx_platform_account"`         // 云平台（如 aliyun）
	AccountName          string     `json:"account_name" gorm:"type:varchar(100);not null;index:idx_platform_account"`    // 账号名称
	DomainName           string     `json:"domain_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_account_domain"` // 域名（与 account_id 联合唯一）
	Owner                string     `json:"owner" gorm:"type:varchar(100)"`                                               // 域名所有者
	AutoRenewEnabled     bool       `json:"auto_renew_enabled" gorm:"type:tinyint(1);default:0"`                          // 是否开启自动续费
	RegistrationDate     *time.Time `json:"registration_date"`                                                            // 注册日期（可能为空）
	ExpirationDate       *time.Time `json:"expiration_date" gorm:"index"`                                                 // 到期日期（可能为空）
	ExpirationDaysLeft   int        `json:"expiration_days_left" gorm:"index"`                                            // 剩余天数
	ExpirationStatusCode string     `json:"expiration_status_code" gorm:"type:varchar(10)"`                               // 到期状态码
	Remark               string     `json:"remark" gorm:"type:varchar(500)"`                                              // 备注
	IsOffline            int        `json:"is_offline" gorm:"type:tinyint(1);not null;default:0;index"`                   // 0=在线 1=已下线
	CreatedAt            time.Time  `json:"created_at" gorm:"autoCreateTime"`                                             // 创建时间
	UpdatedAt            time.Time  `json:"updated_at" gorm:"autoUpdateTime"`                                             // 更新时间
	Source               string     `json:"source" gorm:"type:varchar(20);not null;default:sync"`                         // 来源: sync(云API同步)/manual(手动添加)
}

func (Domain) TableName() string {
	return "domains"
}

// DnsDomain DNS可管理域名模型（对应 dns_domains 表，来自阿里云 DescribeDomains API）
type DnsDomain struct {
	ID          uint64     `json:"id" gorm:"primaryKey"`                                                         // 主键ID
	AccountID   string     `json:"account_id" gorm:"type:varchar(50);not null;uniqueIndex:idx_account_domain"`   // 关联 cloud_accounts.account_id
	Platform    string     `json:"platform" gorm:"type:varchar(50);not null;index"`                              // 云平台
	AccountName string     `json:"account_name" gorm:"type:varchar(100);not null"`                               // 账号名称
	DomainName  string     `json:"domain_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_account_domain"` // 域名（账号内唯一）
	AliDomain   bool       `json:"ali_domain" gorm:"type:tinyint(1);default:0"`                                  // 是否使用当前账号的NS
	Starmark    bool       `json:"starmark" gorm:"type:tinyint(1);default:0"`                                    // 是否为付费版DNS（true=正式版, false=免费版）
	RecordCount int        `json:"record_count" gorm:"default:0"`                                                // 解析记录数
	DnsServers  string     `json:"dns_servers" gorm:"type:varchar(500)"`                                         // DNS服务器列表(逗号分隔)
	CreateTime  *time.Time `json:"create_time"`                                                                  // 域名创建时间
	CreatedAt   time.Time  `json:"created_at" gorm:"autoCreateTime"`                                             // 记录创建时间
	UpdatedAt   time.Time  `json:"updated_at" gorm:"autoUpdateTime"`                                             // 记录更新时间
}

func (DnsDomain) TableName() string {
	return "dns_domains"
}
