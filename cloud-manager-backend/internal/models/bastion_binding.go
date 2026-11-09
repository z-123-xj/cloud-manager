package models

import "time"

// BastionAssetBinding 堡垒机资产授权（ECS实例 → 用户组 → 凭证）
type BastionAssetBinding struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	EcsID        uint64    `json:"ecs_id" gorm:"not null;index;comment:ECS实例ID(ecs_instances.id)"`
	GroupID      uint      `json:"group_id" gorm:"not null;index;comment:用户组ID"`
	CredentialID uint      `json:"credential_id" gorm:"not null;comment:凭证ID"`
	CreatedAt    time.Time `json:"created_at"`
}

func (BastionAssetBinding) TableName() string {
	return "bastion_asset_bindings"
}
