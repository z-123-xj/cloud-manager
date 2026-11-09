package models

import "time"

// CloudRegion 云平台地域管理表（cloud_regions）
type CloudRegion struct {
	ID             uint64    `json:"id" gorm:"primaryKey"`                                       // 主键自增
	Platform       string    `json:"platform" gorm:"type:varchar(50);not null;uniqueIndex:uk_platform_region"` // 平台: aliyun/tencent/aws
	RegionID       string    `json:"region_id" gorm:"type:varchar(100);not null;uniqueIndex:uk_platform_region"` // 地域ID: ap-southeast-1
	RegionName     string    `json:"region_name" gorm:"type:varchar(100);not null"`              // 地域中文名: 新加坡
	RegionEndpoint string    `json:"region_endpoint" gorm:"type:varchar(255)"`                    // API endpoint
	Enabled        bool      `json:"enabled" gorm:"type:tinyint(1);not null;default:1"`          // 是否启用: 0=禁用 1=启用
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime"`                            // 创建时间
	UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime"`                            // 更新时间
}

func (CloudRegion) TableName() string {
	return "cloud_regions"
}

// GetEnabledRegions 获取指定平台全局已启用的地域列表（用于 ECS/EC2 同步及地域反查）
func GetEnabledRegions(platform string) []CloudRegion {
	var regions []CloudRegion
	DB.Where("platform = ? AND enabled = 1", platform).Order("id ASC").Find(&regions)
	return regions
}
