package models

import "time"

// EcsInstance ECS实例模型（对应 ecs_instances 表）
type EcsInstance struct {
	ID                uint64     `json:"id" gorm:"primaryKey"`                                                          // 主键自增
	AccountID         string     `json:"account_id" gorm:"type:varchar(50);not null;uniqueIndex:idx_account_instance"`  // 云账号业务ID
	Platform          string     `json:"platform" gorm:"type:varchar(20);not null;default:aliyun"`                      // 云平台: aliyun/tencent/aws
	Site              string     `json:"site" gorm:"type:varchar(10);not null;default:cn"`                              // 站点: cn=国内站, intl=国际站
	AccountName       string     `json:"account_name" gorm:"type:varchar(100);not null"`                                // 账号别名
	InstanceId        string     `json:"instance_id" gorm:"type:varchar(50);not null;uniqueIndex:idx_account_instance"` // 实例ID (阿里云: i-bpxxx)
	InstanceName      string     `json:"instance_name" gorm:"type:varchar(200)"`                                        // 实例名称
	Description       string     `json:"description" gorm:"type:varchar(500)"`                                          // 实例描述
	Status            string     `json:"status" gorm:"type:varchar(20);not null;index"`                                 // 实例状态: Running/Stopped/Starting/Stopping
	Cpu               int64      `json:"cpu"`                                                                           // CPU核数
	Memory            int64      `json:"memory"`                                                                        // 内存(GB)
	InstanceType      string     `json:"instance_type" gorm:"type:varchar(50)"`                                         // 实例规格: ecs.g7.xlarge
	OSName            string     `json:"os_name" gorm:"type:varchar(200)"`                                              // 操作系统名称
	PublicIpAddress   string     `json:"public_ip_address" gorm:"type:varchar(50)"`                                     // 公网IP (第一块网卡的公网IP)
	EipAddress        string     `json:"eip_address" gorm:"type:varchar(50)"`                                           // 弹性公网IP (EIP绑定地址)
	PrivateIpAddress  string     `json:"private_ip_address" gorm:"type:varchar(50)"`                                    // 私网IP (第一块网卡的私网IP)
	SSHPort           int        `json:"ssh_port" gorm:"column:ssh_port;type:int;not null;default:22"`                  // SSH端口(堡垒机连接用)，云同步默认22，平台可手动维护
	RegionName        string     `json:"region_name" gorm:"type:varchar(50);index"`                                     // 地域中文名: 香港
	ZoneName          string     `json:"zone_name" gorm:"type:varchar(50)"`                                             // 可用区中文名: 香港 可用区B
	CreationTime      *time.Time `json:"creation_time"`                                                                 // ECS实例创建时间 (阿里云API返回)
	IsOffline         int        `json:"is_offline" gorm:"type:tinyint(1);not null;default:0;index"`                    // 是否下线: 0=在线, 1=已下线
	Source            string     `json:"source" gorm:"type:varchar(20);not null;default:sync"`                          // 来源: sync(云API同步)/manual(手动添加)
	CreatedAt         time.Time  `json:"created_at" gorm:"autoCreateTime"`                                              // 记录入库时间 (GORM自动)
	UpdatedAt         time.Time  `json:"updated_at" gorm:"autoUpdateTime"`                                              // 记录更新时间 (GORM自动)
}

func (EcsInstance) TableName() string {
	return "ecs_instances"
}
