package models

import "time"

// BastionGroupCredential 用户组凭证授权（用户组能使用哪些SSH凭证）
type BastionGroupCredential struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	GroupID      uint      `json:"group_id" gorm:"not null;uniqueIndex:idx_group_cred;comment:用户组ID"`
	CredentialID uint      `json:"credential_id" gorm:"not null;uniqueIndex:idx_group_cred;comment:凭证ID"`
	CreatedAt    time.Time `json:"created_at"`
}

func (BastionGroupCredential) TableName() string {
	return "bastion_group_credentials"
}
