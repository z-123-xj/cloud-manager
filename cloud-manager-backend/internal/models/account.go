package models

import "time"

// CloudAccount 云平台账号（多云统一模型）
type CloudAccount struct {
	ID           int       `json:"id" gorm:"primaryKey"`                                             // 主键自增
	AccountID    string    `json:"account_id" gorm:"type:varchar(50);not null;uniqueIndex"`           // 账号唯一标识，业务主键
	Platform     string    `json:"platform" gorm:"type:varchar(20);not null;default:aliyun"`         // 云平台：aliyun/aws/tencent
	Site         string    `json:"site" gorm:"type:varchar(10);not null;default:cn"`                 // 站点：cn=国内站 intl=国际站
	Name         string    `json:"name" gorm:"type:varchar(100);not null"`                           // 账号别名，运维可读
	AccessKey    string    `json:"access_key" gorm:"type:varchar(100);not null;uniqueIndex"`         // AccessKey ID（明文存储，唯一）
	AccessSecret string    `json:"-" gorm:"type:varchar(255);not null"`                              // AccessKey Secret（AES加密存储，json:"-" 禁止序列化）
	Enabled      bool      `json:"enabled" gorm:"type:tinyint(1);not null;default:1"`                // 启用开关，false=跳过同步
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`                                 // 创建时间（GORM自动维护）
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`                                 // 更新时间（GORM自动维护）
}

func (CloudAccount) TableName() string {
    return "cloud_accounts"
}