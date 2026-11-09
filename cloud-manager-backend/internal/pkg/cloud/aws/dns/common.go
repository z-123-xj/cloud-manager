package dns

// AWS Route53 DNS 记录操作公共模块：凭证获取、Hosted Zone 解析、记录结构体与字段映射
// API 文档: https://docs.aws.amazon.com/Route53/latest/APIReference/
//
// Route53 记录由 (Name, Type) 唯一标识（无独立的 RecordId 字段），本模块用 "Name|Type"
// 作为前端 record_id。Route53 为全球服务（无区域区分），故 AWS 不分国内/国际站。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/gin-gonic/gin"
)

// DnsRecordItem DNS解析记录条目（返回给前端，字段与阿里云/腾讯云/Cloudflare 保持统一）
type DnsRecordItem struct {
	RecordID   string `json:"record_id"`
	RR         string `json:"rr"`
	Type       string `json:"type"`
	Value      string `json:"value"`
	TTL        int64  `json:"ttl"`
	Priority   int64  `json:"priority"`
	Line       string `json:"line"`
	Weight     int    `json:"weight"`
	Status     string `json:"status"`
	Locked     bool   `json:"locked"`
	Remark     string `json:"remark"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
	Proxied    bool   `json:"proxied"`
	Proxiable  bool   `json:"proxiable"`
}

// getCredentialFromContext 从 gin context 获取凭证（由 cloud.dns_handler.go 在调用前设置）
// dns_access_key_id = AWS AccessKey ID；dns_access_key_secret = AWS Secret Access Key
func getCredentialFromContext(c *gin.Context) (accessKeyID, accessKeySecret string) {
	akID, _ := c.Get("dns_access_key_id")
	akSecret, _ := c.Get("dns_access_key_secret")
	return fmt.Sprintf("%v", akID), fmt.Sprintf("%v", akSecret)
}

// newRoute53Client 创建 Route53 客户端（Route53 为全球服务，固定 us-east-1）
func newRoute53Client(accessKeyID, accessKeySecret string) (*route53.Client, error) {
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("未获取到 AWS AccessKey/Secret")
	}
	creds := awscreds.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")
	cfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("加载AWS配置失败: %w", err)
	}
	return route53.NewFromConfig(cfg), nil
}

// resolveHostedZoneID 通过域名查找 Route53 Hosted Zone ID
func resolveHostedZoneID(client *route53.Client, domainName string) (string, error) {
	dnsName := domainName
	if !strings.HasSuffix(dnsName, ".") {
		dnsName += "."
	}
	out, err := client.ListHostedZonesByName(context.TODO(), &route53.ListHostedZonesByNameInput{
		DNSName:  aws.String(dnsName),
		MaxItems: aws.Int32(10),
	})
	if err != nil {
		return "", fmt.Errorf("查询 Route53 Hosted Zone 失败: %w", err)
	}
	if len(out.HostedZones) == 0 {
		return "", fmt.Errorf("AWS Route53 未找到域名 %s 对应的 Hosted Zone（请确认已在 Route53 创建该域名的托管区）", domainName)
	}
	for _, z := range out.HostedZones {
		name := strings.TrimSuffix(aws.ToString(z.Name), ".")
		if strings.EqualFold(name, domainName) {
			return aws.ToString(z.Id), nil
		}
	}
	// 兜底：返回第一个（ListHostedZonesByName 已按名称过滤，通常即为该域名的父/自身 zone）
	return aws.ToString(out.HostedZones[0].Id), nil
}

// findRecordSet 在 Hosted Zone 内按 Name+Type 查找完整记录集（分页遍历），用于删除/修改时精确匹配
func findRecordSet(client *route53.Client, zoneID, name, recordType string) (*r53types.ResourceRecordSet, error) {
	target := strings.TrimSuffix(name, ".")
	var startName *string
	var startType r53types.RRType
	for {
		out, err := client.ListResourceRecordSets(context.TODO(), &route53.ListResourceRecordSetsInput{
			HostedZoneId:    aws.String(zoneID),
			StartRecordName: startName,
			StartRecordType: startType,
			MaxItems:        aws.Int32(300),
		})
		if err != nil {
			return nil, err
		}
		for i := range out.ResourceRecordSets {
			rs := &out.ResourceRecordSets[i]
			if strings.EqualFold(strings.TrimSuffix(aws.ToString(rs.Name), "."), target) && string(rs.Type) == recordType {
				return rs, nil
			}
		}
		if !out.IsTruncated {
			break
		}
		startName = out.NextRecordName
		startType = out.NextRecordType
	}
	return nil, fmt.Errorf("未找到记录 %s (%s)", target, recordType)
}

// buildFQDN 主机记录 + 主域名 → FQDN（带结尾点，Route53 要求）
func buildFQDN(rr, domainName string) string {
	rr = strings.TrimSpace(rr)
	if rr == "" || rr == "@" {
		return domainName + "."
	}
	return rr + "." + domainName + "."
}

// rrFromName FQDN（可能带结尾点）→ 主机记录 RR
func rrFromName(name, domainName string) string {
	name = strings.TrimSuffix(name, ".")
	if strings.EqualFold(name, domainName) {
		return "@"
	}
	suffix := "." + domainName
	if len(name) > len(suffix) && strings.EqualFold(name[len(name)-len(suffix):], suffix) {
		return name[:len(name)-len(suffix)]
	}
	return name
}

// makeRecordID 用 Name|Type 作为记录唯一标识（Route53 无独立 RecordId 字段）
func makeRecordID(name, recordType string) string {
	return strings.TrimSuffix(name, ".") + "|" + recordType
}

// parseRecordID 解析 Name|Type 形式的记录标识
func parseRecordID(id string) (name, recordType string, ok bool) {
	parts := strings.SplitN(id, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// buildRRSet 组装一个待提交的非别名记录集
func buildRRSet(fqdn, recordType string, ttl int64, value string) *r53types.ResourceRecordSet {
	return &r53types.ResourceRecordSet{
		Name: aws.String(fqdn),
		Type: r53types.RRType(recordType),
		TTL:  aws.Int64(ttl),
		ResourceRecords: []r53types.ResourceRecord{
			{Value: aws.String(value)},
		},
	}
}

// toDnsRecordItem 将 Route53 ResourceRecordSet 映射为统一前端结构
func toDnsRecordItem(rs *r53types.ResourceRecordSet, domainName string) DnsRecordItem {
	name := aws.ToString(rs.Name)
	recordType := string(rs.Type)
	rr := rrFromName(name, domainName)

	value := ""
	priority := int64(0)
	for i, r := range rs.ResourceRecords {
		v := aws.ToString(r.Value)
		if i > 0 {
			value += "\n"
		}
		value += v
	}
	// MX 的值形如 "10 mail.example.com"，拆出优先级
	if recordType == "MX" && value != "" {
		if idx := strings.Index(value, " "); idx > 0 {
			if p, err := strconv.ParseInt(value[:idx], 10, 64); err == nil {
				priority = p
				value = value[idx+1:]
			}
		}
	}

	ttl := int64(0)
	if rs.TTL != nil {
		ttl = aws.ToInt64(rs.TTL)
	}

	return DnsRecordItem{
		RecordID: makeRecordID(name, recordType),
		RR:       rr,
		Type:     recordType,
		Value:    value,
		TTL:      ttl,
		Priority: priority,
		// Route53 无解析线路/权重/启停/锁定/备注概念，统一补零值
		Line:   "",
		Weight: 0,
		Status: "ENABLE",
		Locked: false,
	}
}
