package cloud

import (
	"net/http"
	"strconv"
	"time"

	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun"
	"cloud-manager-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// accountOption 账号筛选选项（含平台，用于前端按平台分组展示）
type accountOption struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// GetDomainFilterOptions 获取域名漏斗选项（账号、平台） GET /api/domains/filter-options
func GetDomainFilterOptions(c *gin.Context) {
	var accounts []string
	models.DB.Model(&models.Domain{}).Distinct("account_name").Pluck("account_name", &accounts)
	var platforms []string
	models.DB.Model(&models.Domain{}).Distinct("platform").Pluck("platform", &platforms)
	var accountOptions []accountOption
	models.DB.Raw("SELECT DISTINCT account_name AS name, platform FROM domains ORDER BY platform, account_name").Scan(&accountOptions)

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

// GetDomains 跨平台查询域名列表  GET /api/domains?page=1&size=10&keyword=&account_id=&platform=&is_offline=0&expire_start=&expire_end=
func GetDomains(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	var domains []models.Domain
	var total int64

	query := models.DB.Model(&models.Domain{})

	// 关键词搜索（仅域名）
	if keyword := c.Query("keyword"); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("domain_name LIKE ?", like)
	}

	// 按账号ID筛选
	if accountID := c.Query("account_id"); accountID != "" {
		query = query.Where("account_id = ?", accountID)
	}

	// 按账号名称筛选
	if accountName := c.Query("account_name"); accountName != "" {
		query = query.Where("account_name = ?", accountName)
	}

	// 按平台筛选
	if platform := c.Query("platform"); platform != "" {
		query = query.Where("platform = ?", platform)
	}

	// 下线状态筛选
	if isOffline := c.Query("is_offline"); isOffline != "" {
		query = query.Where("is_offline = ?", isOffline)
	}

	// 到期时间范围筛选
	if expireStart := c.Query("expire_start"); expireStart != "" {
		if t, err := time.Parse("2006-01-02", expireStart); err == nil {
			query = query.Where("expiration_date >= ?", t)
		}
	}
	if expireEnd := c.Query("expire_end"); expireEnd != "" {
		if t, err := time.Parse("2006-01-02", expireEnd); err == nil {
			query = query.Where("expiration_date <= ?", t)
		}
	}

	query.Count(&total)

	// 排序
	sortKey := c.DefaultQuery("sort_key", "expiration_date")
	sortDir := c.DefaultQuery("sort_dir", "asc")
	// 白名单防止 SQL 注入
	allowedSortKeys := map[string]bool{
		"expiration_date":      true,
		"expiration_days_left": true,
		"domain_name":          true,
		"registration_date":    true,
	}
	if !allowedSortKeys[sortKey] {
		sortKey = "expiration_date"
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

// GetDomainDetail 跨平台查询单个域名详情  GET /api/domains/:domain
func GetDomainDetail(c *gin.Context) {
	domainName := c.Param("domain")
	if domainName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "域名不能为空"})
		return
	}

	var domain models.Domain
	if err := models.DB.Where("domain_name = ?", domainName).First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "域名不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": domain})
}

// computeDaysLeft 根据到期日计算剩余天数（按自然日差：到期日期 - 今天日期）。
// 到期日为 nil 时返回 0；已过期返回负数。
func computeDaysLeft(exp *time.Time) int {
	if exp == nil {
		return 0
	}
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := exp.Date()
	t1 := time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location())
	t2 := time.Date(y2, m2, d2, 0, 0, 0, 0, exp.Location())
	return int(t2.Sub(t1).Hours() / 24)
}

// CreateDomain POST /api/domains (管理员，手动添加其它平台/未在云上同步的域名)
func CreateDomain(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	var req struct {
		DomainName       string `json:"domain_name" binding:"required"`
		Platform         string `json:"platform"`
		AccountName      string `json:"account_name"`
		Owner            string `json:"owner"`
		AutoRenewEnabled *bool  `json:"auto_renew_enabled"`
		RegistrationDate string `json:"registration_date"` // 格式 2006-01-02
		ExpirationDate   string `json:"expiration_date"`   // 格式 2006-01-02
		Remark           string `json:"remark"`
		IsOffline        int    `json:"is_offline"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误：域名必填"})
		return
	}
	if req.DomainName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "域名必填"})
		return
	}

	domain := models.Domain{
		AccountID:        "manual",
		Platform:         req.Platform,
		AccountName:      req.AccountName,
		Source:           "manual",
		DomainName:       req.DomainName,
		Owner:            req.Owner,
		AutoRenewEnabled: req.AutoRenewEnabled != nil && *req.AutoRenewEnabled,
		Remark:           req.Remark,
		IsOffline:        req.IsOffline,
	}
	if t, err := time.Parse("2006-01-02", req.RegistrationDate); err == nil {
		domain.RegistrationDate = &t
	}
	if t, err := time.Parse("2006-01-02", req.ExpirationDate); err == nil {
		domain.ExpirationDate = &t
	}
	// 手动录入：根据到期日自动计算剩余天数（云同步域名以云端 diff 为准，会覆盖本值）
	domain.ExpirationDaysLeft = computeDaysLeft(domain.ExpirationDate)

	if err := models.DB.Create(&domain).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": domain})
}

// UpdateDomain PUT /api/domains/:id (管理员，仅手动添加的域名可编辑)
func UpdateDomain(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, err := strconv.ParseUint(c.Param("domain"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "非法ID"})
		return
	}
	var d models.Domain
	if err := models.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "域名不存在"})
		return
	}
	if d.Source != "manual" {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "云同步的域名不可编辑"})
		return
	}
	var req struct {
		DomainName       *string `json:"domain_name"`
		Platform         string  `json:"platform"`
		AccountName      string  `json:"account_name"`
		Owner            string  `json:"owner"`
		AutoRenewEnabled *bool   `json:"auto_renew_enabled"`
		RegistrationDate string  `json:"registration_date"`
		ExpirationDate   string  `json:"expiration_date"`
		Remark           string  `json:"remark"`
		IsOffline        *int    `json:"is_offline"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	updates := map[string]interface{}{}
	if req.DomainName != nil {
		updates["domain_name"] = *req.DomainName
	}
	if req.Platform != "" {
		updates["platform"] = req.Platform
	}
	if req.AccountName != "" {
		updates["account_name"] = req.AccountName
	}
	if req.Owner != "" {
		updates["owner"] = req.Owner
	}
	if req.AutoRenewEnabled != nil {
		updates["auto_renew_enabled"] = *req.AutoRenewEnabled
	}
	if req.RegistrationDate != "" {
		if t, err := time.Parse("2006-01-02", req.RegistrationDate); err == nil {
			updates["registration_date"] = t
		}
	}
	if req.ExpirationDate != "" {
		if t, err := time.Parse("2006-01-02", req.ExpirationDate); err == nil {
			updates["expiration_date"] = t
			// 编辑时按填写的到期日重算剩余天数
			updates["expiration_days_left"] = computeDaysLeft(&t)
		}
	} else {
		// 前端显式清空到期日：剩余天数一并清零（前端编辑总会带该字段，空串即代表清空）
		updates["expiration_date"] = nil
		updates["expiration_days_left"] = 0
	}
	if req.Remark != "" {
		updates["remark"] = req.Remark
	}
	if req.IsOffline != nil {
		updates["is_offline"] = *req.IsOffline
	}

	if len(updates) > 0 {
		if err := models.DB.Model(&d).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

// UpdateDomainRemarkHandler PUT /api/domains/:id/remark (管理员，修改域名备注)
//
// 备注以云平台为准，按平台是否真的存在「备注」这个参数分别处理：
//   - 阿里云国内站(site=cn)：云上有域名备注字段，调用云解析 UpdateDomainRemark 写回云端；
//   - 阿里云国际站(site=intl) / AWS / Cloudflare 等：云上根本没有域名备注这个参数，
//     不调任何云端接口，直接落入本平台数据库（云同步也不会覆盖这些平台的备注）。
func UpdateDomainRemarkHandler(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, err := strconv.ParseUint(c.Param("domain"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "非法ID"})
		return
	}
	var d models.Domain
	if err := models.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "域名不存在"})
		return
	}
	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}

	synced := false
	syncMsg := ""

	if d.Platform != "aliyun" {
		// AWS / Cloudflare / 腾讯云 等平台云上没有域名备注参数，直接落本地库
		syncMsg = "该平台域名没有备注参数，仅保存到本地"
	} else {
		var acc models.CloudAccount
		if err := models.DB.Where("account_id = ?", d.AccountID).First(&acc).Error; err != nil {
			syncMsg = "未找到域名所属账号，仅保存到本地"
		} else if !models.DomainRemarkSupported(d.Platform, acc.Site) {
			// 阿里云国际站同样没有备注参数
			syncMsg = "阿里云国际站域名没有备注参数，仅保存到本地"
		} else {
			secret, err := utils.Decrypt(acc.AccessSecret)
			if err != nil {
				syncMsg = "账号凭证解密失败，仅保存到本地"
			} else if err := aliyun.UpdateDomainRemark(acc.AccessKey, secret, d.DomainName, req.Remark); err != nil {
				syncMsg = "云端同步失败，已保存到本地：" + err.Error()
			} else {
				synced = true
				syncMsg = "已同步到阿里云"
			}
		}
	}

	if err := models.DB.Model(&d).Updates(map[string]interface{}{
		"remark": req.Remark,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "备注已保存",
		"data": gin.H{"synced": synced, "sync_msg": syncMsg, "remark": req.Remark},
	})
}

// DeleteDomain DELETE /api/domains/:id (管理员，仅手动添加的域名可删除)
func DeleteDomain(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, err := strconv.ParseUint(c.Param("domain"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "非法ID"})
		return
	}
	var d models.Domain
	if err := models.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "域名不存在"})
		return
	}
	if d.Source != "manual" {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "云同步的域名不可删除"})
		return
	}
	models.DB.Delete(&d)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}
