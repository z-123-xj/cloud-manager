package cloud_account

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun"
	awspkg "cloud-manager-backend/internal/pkg/cloud/aws"
	"cloud-manager-backend/internal/pkg/cloud/tencent"
	"cloud-manager-backend/pkg/logger"
	"cloud-manager-backend/pkg/utils"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// resolveAccountID 根据平台自动获取账号ID（account_id），无需手动填写：
//   - aliyun/tencent/aws：用 AccessKey/Secret 调云接口获取主账号ID
//   - cloudflare：AccessKey 即 Cloudflare Account ID，直接复用
func resolveAccountID(platform, accessKey, secret string) (string, error) {
	switch platform {
	case "aliyun":
		return aliyun.GetAccountID(accessKey, secret)
	case "tencent":
		return tencent.GetAccountID(accessKey, secret)
	case "aws":
		return awspkg.GetAccountID(accessKey, secret)
	case "cloudflare":
		if accessKey == "" {
			return "", fmt.Errorf("Cloudflare 需填写 Account ID（AccessKey）")
		}
		return accessKey, nil
	default:
		return "", fmt.Errorf("未知平台: %s", platform)
	}
}

// CreateAccountRequest 创建账号请求
type CreateAccountRequest struct {
	AccountID    string `json:"account_id" binding:"omitempty,max=50"`
	Platform     string `json:"platform" binding:"required,oneof=aliyun aws tencent cloudflare"`
	Site         string `json:"site" binding:"omitempty,oneof=cn intl"`
	Name         string `json:"name" binding:"required,max=100"`
	AccessKey    string `json:"access_key" binding:"required"`
	AccessSecret string `json:"access_secret" binding:"required"`
	Enabled      *int   `json:"enabled"`
}

// ==================== Handler（接收 *gin.Context）====================

// CreateAccount 创建云账号  POST /api/account
func CreateAccount(c *gin.Context) {
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误：" + err.Error()})
		return
	}

	// 根据平台自动获取 account_id（不依赖手动填写）
	resolvedAccountID, err := resolveAccountID(req.Platform, req.AccessKey, req.AccessSecret)
	if err != nil {
		logger.Error("cloud_account", "自动获取账号ID失败", "err", err, "platform", req.Platform)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "自动获取账号ID失败: " + err.Error()})
		return
	}

	// 站点由前端手动选择（cn/intl），不再自动探测
	if req.Site == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "站点(site)必填，请选择国内站或国际站"})
		return
	}
	site := req.Site

	// 检查 account_id 是否已存在（account_id 为资源归属关联键，需唯一）
	var count int64
	models.DB.Model(&models.CloudAccount{}).Where("account_id = ?", resolvedAccountID).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "该云账号已存在（account_id 重复）"})
		return
	}

	// 检查 access_key 是否已存在（access_key 是账号唯一标识）
	models.DB.Model(&models.CloudAccount{}).Where("access_key = ?", req.AccessKey).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "access_key 已存在"})
		return
	}

	// 加密 AccessSecret
	encryptedSecret, err := utils.Encrypt(req.AccessSecret)
	if err != nil {
		logger.Error("cloud_account", "加密 AccessSecret 失败", "err", err, "account_id", resolvedAccountID)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "加密失败"})
		return
	}

	account := &models.CloudAccount{
		AccountID:    resolvedAccountID,
		Platform:     req.Platform,
		Site:         site,
		Name:         req.Name,
		AccessKey:    req.AccessKey,
		AccessSecret: encryptedSecret,
		Enabled:      true,
	}
	if req.Enabled != nil {
		account.Enabled = *req.Enabled != 0
	}

	if err := models.DB.Create(account).Error; err != nil {
		logger.Error("cloud_account", "创建云账号失败", "err", err, "account_id", resolvedAccountID, "platform", req.Platform, "site", req.Site, "name", req.Name)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败：" + err.Error()})
		return
	}

	account.AccessSecret = ""
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": account})
}



// GetAccountList 查询账号列表  GET /api/account?page=1&size=10&keyword=&platform=&site=
func GetAccountList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	keyword := strings.TrimSpace(c.Query("keyword"))
	platform := strings.TrimSpace(c.Query("platform"))
	site := strings.TrimSpace(c.Query("site"))

	// 组装过滤条件（账号名称仅作为备注，不参与唯一性约束，可任意重复）
	q := models.DB.Model(&models.CloudAccount{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR account_id LIKE ? OR access_key LIKE ?", like, like, like)
	}
	if platform != "" {
		q = q.Where("platform = ?", platform)
	}
	if site != "" {
		q = q.Where("site = ?", site)
	}

	var total int64
	q.Count(&total)

	var accounts []models.CloudAccount
	q.Offset((page - 1) * size).Limit(size).Order("id DESC").Find(&accounts)

	for i := range accounts {
		accounts[i].AccessSecret = ""
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":  accounts,
			"total": total,
			"page":  page,
			"size":  size,
		},
	})
}

// GetAccountByID 查询单个账号  GET /api/account/:id
// 安全策略：AccessSecret 只返回脱敏值（前4+后4），不支持查看完整密钥
// 修改密钥请通过 PUT /api/account/:id 直接覆盖
func GetAccountByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
		return
	}

	var account models.CloudAccount
	if err := models.DB.Where("id = ?", id).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "账号不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
		return
	}

	// 解密后脱敏，不暴露完整密钥
	plainSecret, err := utils.Decrypt(account.AccessSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解密失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"id":            account.ID,
			"account_id":    account.AccountID,
			"platform":      account.Platform,
			"site":          account.Site,
			"name":          account.Name,
			"access_key":    account.AccessKey,
			"access_secret": maskSecret(plainSecret),
			"enabled":       account.Enabled,
			"created_at":    account.CreatedAt,
			"updated_at":    account.UpdatedAt,
		},
	})
}

// maskSecret 脱敏密钥，仅显示前4位和后4位
func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// UpdateAccountRequest 更新账号请求
type UpdateAccountRequest struct {
	AccountID    string `json:"account_id"`
	Platform     string `json:"platform" binding:"omitempty,oneof=aliyun aws tencent cloudflare"`
	Site         string `json:"site" binding:"omitempty,oneof=cn intl"`
	Name         string `json:"name" binding:"omitempty,max=100"`
	AccessKey    string `json:"access_key"`
	AccessSecret string `json:"access_secret"`
	Enabled      *int   `json:"enabled"`
}

// UpdateAccount 更新账号  PUT /api/account/:id
func UpdateAccount(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
		return
	}

	// 检查是否存在
	var existing models.CloudAccount
	if err := models.DB.Where("id = ?", id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "账号不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误：" + err.Error()})
		return
	}

	// 计算最终 account_id：始终根据有效 AccessKey/Secret 自动获取。
	// 即便库里存错、或请求里填错，也一律纠正为云端真实值（账号ID 是程序取字段用的关联键）。
	plat := existing.Platform
	if req.Platform != "" {
		plat = req.Platform
	}
	ak := existing.AccessKey
	// 前端编辑时未改动 access_key 会以掩码 "***" 提交，视为未修改，沿用库里真实值
	if req.AccessKey != "" && req.AccessKey != "***" {
		ak = req.AccessKey
	}
	sk := req.AccessSecret
	// 未提供新密钥（空或掩码 "***"）时，用已存（解密后）密钥，避免掩码覆盖真实值
	if sk == "" || sk == "***" {
		if dec, derr := utils.Decrypt(existing.AccessSecret); derr == nil {
			sk = dec
		}
	}
	var accountID string
	if plat == "cloudflare" {
		accountID = ak
	} else {
		resolved, rerr := resolveAccountID(plat, ak, sk)
		if rerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "获取账号ID失败: " + rerr.Error()})
			return
		}
		accountID = resolved
	}

	// 如果 account_id 变化，检查是否与其他账号冲突
	if accountID != existing.AccountID {
		var count int64
		models.DB.Model(&models.CloudAccount{}).Where("account_id = ? AND id != ?", accountID, id).Count(&count)
		if count > 0 {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "该云账号已存在（account_id 重复）"})
			return
		}
	}

	// 逐字段对比，只收集真正变化的字段
	updates := map[string]interface{}{}

	if accountID != existing.AccountID {
		updates["account_id"] = accountID
	}
	if req.Platform != "" && req.Platform != existing.Platform {
		updates["platform"] = req.Platform
	}
	// 站点由前端手动选择（cn/intl），不再自动探测
	if req.Site != "" && req.Site != existing.Site {
		updates["site"] = req.Site
	}
	if req.Name != "" && req.Name != existing.Name {
		updates["name"] = req.Name
	}
	if req.AccessKey != "" && req.AccessKey != existing.AccessKey {
		updates["access_key"] = req.AccessKey
	}
	if req.AccessSecret != "" {
		encrypted, err := utils.Encrypt(req.AccessSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "加密失败"})
			return
		}
		updates["access_secret"] = encrypted
	}
	if req.Enabled != nil && (*req.Enabled != 0) != existing.Enabled {
		updates["enabled"] = *req.Enabled != 0
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "没有字段需要更新", "data": gin.H{"account_id": accountID}})
		return
	}

	if err := models.DB.Model(&models.CloudAccount{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败：" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": gin.H{"account_id": accountID}})
}

// DeleteAccount 删除账号  DELETE /api/account/:id
func DeleteAccount(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
		return
	}

	// 检查是否存在
	var existing models.CloudAccount
	if err := models.DB.Where("id = ?", id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "账号不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
		return
	}

	if err := models.DB.Where("id = ?", id).Delete(&models.CloudAccount{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败：" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// SyncAccountRequest 按需同步请求：指定要同步的资源类型（可多选）
type SyncAccountRequest struct {
	Resources []string `json:"resources" binding:"required,min=1"` // domains=域名 / dns=DNS域名 / ecs=ECS / cdn=CDN / all=全部
}

// allowedSyncResources 允许的同步资源类型
var allowedSyncResources = map[string]bool{
	"domains": true, // 域名注册列表
	"dns":     true, // DNS可管理域名
	"ecs":     true, // ECS实例
	"cdn":     true, // CDN加速域名
	"all":     true, // 全部资源
}

// GetAccountCount 获取账号总数（无需管理员权限，首页展示用）
func GetAccountCount(c *gin.Context) {
	var count int64
	models.DB.Model(&models.CloudAccount{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"total": count}})
}
