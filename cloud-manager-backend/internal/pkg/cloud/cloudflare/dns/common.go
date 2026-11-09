package dns

// Cloudflare DNS 记录操作公共模块：凭证获取、zone_id 解析、记录结构体与字段映射
// API 文档: https://developers.cloudflare.com/api/operations/dns-records-for-a-zone-list-dns-records
//
// 与阿里云/腾讯云 dns 包的对应关系：
//   - 前端请求/响应字段保持完全一致（DnsRecordItem 结构），前端零改动
//   - 差异点：Cloudflare 用 zone_id（记录操作必须先按域名解析出 zone_id）；
//     无解析线路(line)/权重(weight)概念；无记录级启用/禁用状态

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"cloud-manager-backend/internal/pkg/cloud/cloudflare"

	"github.com/gin-gonic/gin"
)

// ==================== 凭证获取 ====================

// getCredentialFromContext 从 gin context 获取凭证
// 凭证由上层 cloud.dns_handler.go 在调用前设置。
// Cloudflare 语义：dns_access_key_id = Account ID（可空，仅用于按账户过滤），
//
//	dns_access_key_secret = API Token（Bearer 认证，DNS 记录操作真正使用的凭证）
func getCredentialFromContext(c *gin.Context) (accountID, token string) {
	akID, _ := c.Get("dns_access_key_id")
	akSecret, _ := c.Get("dns_access_key_secret")
	return fmt.Sprintf("%v", akID), fmt.Sprintf("%v", akSecret)
}

// ==================== zone_id 解析 ====================

// resolveZoneID 通过主域名查找 Cloudflare Zone ID
// Cloudflare 的记录操作接口必须用 zone_id（域名本身不能直接当路径参数）
func resolveZoneID(client *cloudflare.Client, domainName string) (string, error) {
	query := url.Values{}
	query.Set("name", domainName)
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if _, err := client.Get("/zones", query, &zones); err != nil {
		return "", fmt.Errorf("查询 Cloudflare Zone 失败: %w", err)
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("Cloudflare 账户下未找到域名 %s 对应的 Zone（请确认已加入 Cloudflare 且 Token 有 Zone:Read 权限）", domainName)
	}
	// name 参数已精确过滤，取第一个（仍做一次大小写不敏感匹配兜底）
	for _, z := range zones {
		if strings.EqualFold(z.Name, domainName) {
			return z.ID, nil
		}
	}
	return zones[0].ID, nil
}

// ==================== 记录结构体与映射 ====================

// cfRecord Cloudflare DNS 记录对象（仅取需要的字段）
type cfRecord struct {
	ID        string `json:"id"`
	ZoneID    string `json:"zone_id"`
	ZoneName  string `json:"zone_name"`
	Name      string `json:"name"`     // FQDN，如 www.example.com
	Type      string `json:"type"`     // A / AAAA / CNAME / MX / TXT / NS / SRV ...
	Content   string `json:"content"`  // 记录值
	TTL       int64  `json:"ttl"`      // 秒；1 = 自动
	Priority  *int64 `json:"priority"` // MX/SRV 专用，其余类型为 null
	Proxied   bool   `json:"proxied"`  // 是否走 Cloudflare CDN（橙云）
	Proxiable bool   `json:"proxiable"`
	Locked    bool   `json:"locked"`
	Comment   string `json:"comment"` // 备注
	CreatedOn string `json:"created_on"`
	ModifiedOn string `json:"modified_on"`
}

// DnsRecordItem DNS解析记录条目（返回给前端，字段与阿里云/腾讯云保持统一）
type DnsRecordItem struct {
	RecordID   string `json:"record_id"`   // 解析记录ID
	RR         string `json:"rr"`          // 主机记录，如 www、@、*
	Type       string `json:"type"`        // 记录类型
	Value      string `json:"value"`       // 记录值
	TTL        int64  `json:"ttl"`         // 生存时间（秒）
	Priority   int64  `json:"priority"`    // MX/SRV 优先级，其余为 0
	Line       string `json:"line"`        // 解析线路（Cloudflare 无此概念，恒为空）
	Weight     int    `json:"weight"`      // 权重（Cloudflare 无此概念，恒为 0）
	Status     string `json:"status"`      // 状态（Cloudflare 记录无启停概念，恒为 ENABLE）
	Locked     bool   `json:"locked"`      // 是否被锁定
	Remark     string `json:"remark"`      // 备注
	CreateTime string `json:"create_time"` // 创建时间，格式：2006-01-02 15:04:05
	UpdateTime string `json:"update_time"` // 更新时间，格式：2006-01-02 15:04:05
	// Cloudflare 特有：CDN 代理（橙云）状态与可代理性（其他平台恒为 false）
	Proxied   bool `json:"proxied"`   // 是否已开启 Cloudflare CDN 代理
	Proxiable bool `json:"proxiable"` // 该记录类型是否支持代理（仅 A/AAAA/CNAME）
}

// cfProxiableType Cloudflare 仅 A/AAAA/CNAME 记录支持 CDN 代理（橙云）
func cfProxiableType(recordType string) bool {
	return recordType == "A" || recordType == "AAAA" || recordType == "CNAME"
}

// cfStructuredTypes 需要结构化 data 对象（而非 content 字符串）的记录类型，
// 平台表单暂不支持，直接返回明确提示，引导去 Cloudflare 面板配置
var cfStructuredTypes = map[string]bool{
	"SRV": true, "LOC": true, "HTTPS": true, "SVCB": true, "NAPTR": true,
}

// validCFTTL Cloudflare TTL 合法值：1（自动）或 60~86400 秒
func validCFTTL(ttl int64) bool {
	return ttl == 1 || (ttl >= 60 && ttl <= 86400)
}

// toDnsRecordItem 将 Cloudflare 记录映射为统一前端结构
// domainName: 主域名（用于从 FQDN 截取主机记录 RR）
func (r cfRecord) toDnsRecordItem(domainName string) DnsRecordItem {
	rr := r.Name
	if strings.EqualFold(rr, domainName) {
		rr = "@"
	} else if suffix := "." + domainName; len(r.Name) > len(suffix) && strings.EqualFold(r.Name[len(r.Name)-len(suffix):], suffix) {
		rr = r.Name[:len(r.Name)-len(suffix)]
	}
	var priority int64
	if r.Priority != nil {
		priority = *r.Priority
	}
	return DnsRecordItem{
		RecordID:   r.ID,
		RR:         rr,
		Type:       r.Type,
		Value:      r.Content,
		TTL:        r.TTL,
		Priority:   priority,
		Line:       "",
		Weight:     0,
		Status:     "ENABLE", // Cloudflare 记录创建即生效，无启用/禁用状态
		Locked:     r.Locked,
		Remark:     r.Comment,
		CreateTime: formatCFTime(r.CreatedOn),
		UpdateTime: formatCFTime(r.ModifiedOn),
		Proxied:    r.Proxied,
		Proxiable:  r.Proxiable,
	}
}

// buildFQDN 主机记录 + 主域名 → FQDN（@ 或空 → 主域名本身）
func buildFQDN(rr, domainName string) string {
	if rr == "" || rr == "@" {
		return domainName
	}
	return rr + "." + domainName
}

// formatCFTime Cloudflare 时间（RFC3339）→ "2006-01-02 15:04:05"（本地时区）
func formatCFTime(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return s
}
