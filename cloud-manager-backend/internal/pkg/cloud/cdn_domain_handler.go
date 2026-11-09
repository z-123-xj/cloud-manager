package cloud

// CDN加速域名列表查询（从 cdn_domains 表读取，数据由同步任务写入）
// 数据来源：阿里云 CDN（DescribeUserDomains）/ AWS CloudFront（ListDistributions）

import (
	"net/http"
	"strconv"

	"cloud-manager-backend/internal/models"

	"github.com/gin-gonic/gin"
)

// cdnAccountOption 账号筛选选项（含平台，用于前端按平台分组展示）
type cdnAccountOption struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// GetCdnDomainFilterOptions 获取CDN域名筛选选项 GET /api/cdn-domains/filter-options
func GetCdnDomainFilterOptions(c *gin.Context) {
	// 只返回 cdn_domains 表中实际存在数据的平台与账号（无资源的平台不展示），
	// 避免 cloudflare 等没有 CDN 产品的平台出现在筛选器里。
	var accounts []cdnAccountOption
	models.DB.Raw("SELECT DISTINCT account_name AS name, platform FROM cdn_domains ORDER BY platform, account_name").Scan(&accounts)
	var platforms []string
	models.DB.Model(&models.CdnDomain{}).Distinct("platform").Pluck("platform", &platforms)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"accounts":  accounts,
			"platforms": platforms,
		},
	})
}

// GetCdnDomains 查询CDN加速域名列表 GET /api/cdn-domains?page=1&size=10&keyword=&account_name=&platform=
func GetCdnDomains(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	var domains []models.CdnDomain
	var total int64

	query := models.DB.Model(&models.CdnDomain{})

	// 关键词搜索（加速域名 / 源站 / 账号名）
	if keyword := c.Query("keyword"); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("domain_name LIKE ? OR origin_domain LIKE ? OR account_name LIKE ?", like, like, like)
	}

	// 按账号名称筛选
	if accountName := c.Query("account_name"); accountName != "" {
		query = query.Where("account_name = ?", accountName)
	}

	// 按平台筛选
	if platform := c.Query("platform"); platform != "" {
		query = query.Where("platform = ?", platform)
	}

	// 按运行状态筛选（如 运行中/已停止/配置失败）；前端默认只显示"运行中"
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	query.Count(&total)

	// 排序：默认按加速域名
	sortKey := c.DefaultQuery("sort_key", "domain_name")
	sortDir := c.DefaultQuery("sort_dir", "asc")
	allowedSortKeys := map[string]bool{
		"domain_name":   true,
		"origin_domain": true,
		"account_name":  true,
		"updated_at":    true,
	}
	if !allowedSortKeys[sortKey] {
		sortKey = "domain_name"
	}
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "asc"
	}
	query.Order(sortKey + " " + sortDir).Offset((page - 1) * size).Limit(size).Find(&domains)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":  domains,
			"total": total,
			"page":  page,
			"size":  size,
		},
	})
}
