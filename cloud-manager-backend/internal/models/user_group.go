package models

import "time"

// UserGroup 用户组
type UserGroup struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"type:varchar(100);not null;uniqueIndex;comment:用户组名称"`
	Description string    `json:"description" gorm:"type:varchar(500);comment:描述"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (UserGroup) TableName() string { return "user_groups" }

// UserGroupMember 用户组成员
type UserGroupMember struct {
	ID      uint `json:"id" gorm:"primaryKey"`
	GroupID uint `json:"group_id" gorm:"not null;index;comment:用户组ID"`
	UserID  uint `json:"user_id" gorm:"not null;index;comment:用户ID"`
}

func (UserGroupMember) TableName() string { return "user_group_members" }
