package models

import (
	"cloud-manager-backend/pkg/utils"
	"fmt"
)

type User struct {
	ID            int    `json:"id" gorm:"primaryKey"`
	Username      string `json:"username" gorm:"type:varchar(50);not null;uniqueIndex"`
	Password      string `json:"-" gorm:"type:varchar(255);not null"`
	Email         string `json:"email" gorm:"type:varchar(100);not null;uniqueIndex"`
	Role          string `json:"role" gorm:"type:varchar(20);not null;default:user"`
	RealName      string `json:"real_name" gorm:"type:varchar(50)"`
	LoginAttempts int    `json:"-" gorm:"type:int;not null;default:0;comment:连续登录失败次数"`
	Locked        bool   `json:"locked" gorm:"type:tinyint(1);not null;default:0;comment:账号是否被锁定"`
	AddTime       string `json:"add_time" gorm:"type:datetime;autoCreateTime"`
}

// TableName 指定表名
func (u *User) TableName() string {
	return "users"
}

// 角色常量
const (
	RoleAdmin    = "admin"    // 管理员
	RoleReadonly = "readonly" // 只读
	RoleUser     = "user"     // 普通用户
)

// MaxLoginAttempts 最大连续登录失败次数，超出后锁定账号
const MaxLoginAttempts = 5

// ResetUserPassword 应急重置用户密码（用当前 AES 密钥加密）并解锁，用于运维排障。
// 当账号被锁定、或数据库中密码密文与当前 AES 密钥不一致（解密持续失败）导致无法登录时，
// 用此函数以“当前代码使用的密钥”重新加密新密码写回，可恢复登录。
func ResetUserPassword(username, newPassword string) error {
	var user User
	if err := DB.Where("username = ?", username).First(&user).Error; err != nil {
		return fmt.Errorf("用户不存在: %w", err)
	}
	enc, err := utils.Encrypt(newPassword)
	if err != nil {
		return fmt.Errorf("加密失败: %w", err)
	}
	return DB.Model(&user).Updates(map[string]interface{}{
		"password":       enc,
		"locked":         false,
		"login_attempts": 0,
	}).Error
}

