package models

import "time"

// ServerCredential SSH 连接凭证（密码或私钥，AES加密存储）
type ServerCredential struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	Name       string    `json:"name" gorm:"type:varchar(100);not null;comment:凭证别名（唯一约束见 init_db.sql）"`
	AuthType   string    `json:"auth_type" gorm:"type:varchar(20);not null;default:password;comment:认证方式:password/private_key"`
	Username   string    `json:"username" gorm:"type:varchar(100);not null;comment:SSH用户名"`
	Password   string    `json:"-" gorm:"type:varchar(500);comment:密码(AES加密)"`
	PrivateKey string    `json:"-" gorm:"type:text;comment:SSH私钥(AES加密)"`
	Passphrase string    `json:"-" gorm:"type:varchar(200);comment:私钥密码(AES加密)"`
	CreatedBy  int       `json:"created_by" gorm:"comment:创建者用户ID"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (ServerCredential) TableName() string {
	return "bastion_credentials"
}
