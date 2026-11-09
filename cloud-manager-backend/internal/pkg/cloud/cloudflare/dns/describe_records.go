package dns

// 查询DNS解析记录列表
// API: GET /zones/{zone_id}/dns_records
// 文档: https://developers.cloudflare.com/api/operations/dns-records-for-a-zone-list-dns-records

import (
	"net/http"
	"net/url"
	"strconv"

	"cloud-manager-backend/internal/pkg/cloud/cloudflare"

	"github.com/gin-gonic/gin"
)

// DescribeRecordsHandler GET /api/domains/:domain/dns
// 查询指定域名（Cloudflare Zone）下的所有DNS解析记录（分页遍历）
func DescribeRecordsHandler(c *gin.Context) {
	domainName := c.Param("domain")
	_, token := getCredentialFromContext(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "未获取到 Cloudflare API Token"})
		return
	}

	client := cloudflare.NewClient(token, "")
	zoneID, err := resolveZoneID(client, domainName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	var allRecords []DnsRecordItem
	page := 1
	totalPages := 1
	for page <= totalPages {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")

		var records []cfRecord
		info, err := client.Get("/zones/"+zoneID+"/dns_records", query, &records)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询DNS记录失败: " + err.Error()})
			return
		}
		for _, r := range records {
			allRecords = append(allRecords, r.toDnsRecordItem(domainName))
		}

		if info != nil && info.TotalPages > 0 {
			totalPages = info.TotalPages
		} else {
			break
		}
		page++
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":  allRecords,
			"total": len(allRecords),
			// 平台标识：前端据此切换 Cloudflare 专属表单（代理状态/TTL自动/类型列表）
			"platform": "cloudflare",
		},
	})
}
