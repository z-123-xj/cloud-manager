package models

import "time"

// LoginAttempt 登录尝试记录（用于登录频率限制；验证码错误不计入，由外层拦截）
type LoginAttempt struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	IP        string    `gorm:"size:64;index" json:"-"`
	Username  string    `gorm:"size:64;index" json:"-"`
	Success   bool      `json:"-"`
	CreatedAt time.Time `gorm:"index" json:"-"`
}

func (LoginAttempt) TableName() string { return "login_attempts" }
