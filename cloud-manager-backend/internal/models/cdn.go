package models

import "time"

// CdnDomain CDN加速域名（对应 cdn_domains 表）
// 数据来源：阿里云 CDN（DescribeUserDomains）/ AWS CloudFront（ListDistributions）
type CdnDomain struct {
	ID             uint64    `json:"id" gorm:"primaryKey"`                                       // 主键自增
	AccountID      string    `json:"account_id" gorm:"type:varchar(50);not null;index"`          // 云账号业务ID
	Platform       string    `json:"platform" gorm:"type:varchar(20);not null;index"`            // 云平台: aliyun/aws
	AccountName    string    `json:"account_name" gorm:"type:varchar(100);not null"`             // 账号别名
	DomainName     string    `json:"domain_name" gorm:"type:varchar(200);not null;index"`        // 加速域名（用户最终访问的域名）
	Cname          string    `json:"cname" gorm:"type:varchar(200)"`                             // 加速域名（阿里云分配的 .cdn.dnsv1.com；AWS 为 xxx.cloudfront.net）
	Product        string    `json:"product" gorm:"type:varchar(20);not null;default:cdn;index"` // 产品类型: cdn=CDN / dcdn=全站加速（阿里云独有）
	Status         string    `json:"status" gorm:"type:varchar(20);not null"`                    // 运行状态：运行中/配置中/已停止等
	BizType        string    `json:"biz_type" gorm:"type:varchar(50)"`                           // 业务类型（阿里云: 图片小文件/大文件下载等；AWS 多为空）
	OriginDomain   string    `json:"origin_domain" gorm:"type:varchar(500)"`                     // 源站地址（回源域名/IP）
	HttpsOn        bool      `json:"https_on" gorm:"type:tinyint(1);not null;default:0"`         // 是否开启 HTTPS（由证书 ServerCertificateStatus 赋值）
	CertName       string    `json:"cert_name" gorm:"type:varchar(200)"`                         // 绑定证书名称（阿里云）
	CertType       string    `json:"cert_type" gorm:"type:varchar(50)"`                          // 证书类型：free/paid 等（阿里云）
	CertExpireTime string    `json:"cert_expire_time" gorm:"type:varchar(50)"`                   // 证书到期时间（原始字符串，避免解析失败）
	Aliases        string    `json:"aliases" gorm:"type:varchar(1000)"`                          // AWS CloudFront 全部备用域名（逗号分隔；阿里云为空）
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime"`                           // 记录入库时间 (GORM自动)
	UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime"`                           // 记录更新时间 (GORM自动)
}

func (CdnDomain) TableName() string {
	return "cdn_domains"
}
