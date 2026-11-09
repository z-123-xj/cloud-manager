package cloud

import (
	"net/http"
	"strconv"

	"cloud-manager-backend/internal/models"

	"github.com/gin-gonic/gin"
)

// GetDnsDomainFilterOptions 获取DNS域名漏斗选项 GET /api/dns-domains/filter-options
func GetDnsDomainFilterOptions(c *gin.Context) {
	var accounts []string
	models.DB.Model(&models.DnsDomain{}).Distinct("account_name").Pluck("account_name", &accounts)
	var platforms []string
	models.DB.Model(&models.DnsDomain{}).Distinct("platform").Pluck("platform", &platforms)
	var accountOptions []accountOption
	models.DB.Raw("SELECT DISTINCT account_name AS name, platform FROM dns_domains ORDER BY platform, account_name").Scan(&accountOptions)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"accounts":        accounts,
			"platforms":       platforms,
			"account_options": accountOptions,
		},
	})
}

// GetDnsDomains 查询DNS可管理域名列表 GET /api/dns-domains?page=1&size=10&keyword=&account_name=&platform=
func GetDnsDomains(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	var domains []models.DnsDomain
	var total int64

	query := models.DB.Model(&models.DnsDomain{})

	// 关键词搜索（仅域名）
	if keyword := c.Query("keyword"); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("domain_name LIKE ?", like)
	}

	// 按账号名称筛选
	if accountName := c.Query("account_name"); accountName != "" {
		query = query.Where("account_name = ?", accountName)
	}

	// 按平台筛选
	if platform := c.Query("platform"); platform != "" {
		query = query.Where("platform = ?", platform)
	}

	query.Count(&total)

	// 排序：默认按域名
	sortKey := c.DefaultQuery("sort_key", "domain_name")
	sortDir := c.DefaultQuery("sort_dir", "asc")
	allowedSortKeys := map[string]bool{
		"domain_name":  true,
		"record_count": true,
		"create_time":  true,
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
