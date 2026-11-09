package models

import "time"

// BastionGroupAsset 用户组资产授权（用户组能访问哪些ECS）
type BastionGroupAsset struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	GroupID   uint      `json:"group_id" gorm:"not null;uniqueIndex:idx_group_ecs;comment:用户组ID"`
	EcsID     uint64    `json:"ecs_id" gorm:"not null;uniqueIndex:idx_group_ecs;comment:ECS实例ID"`
	CreatedAt time.Time `json:"created_at"`
}

func (BastionGroupAsset) TableName() string {
	return "bastion_group_assets"
}
