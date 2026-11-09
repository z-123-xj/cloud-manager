package bastion

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ==================== 凭证CRUD ====================

// ListCredentials GET /api/bastion/credentials
func ListCredentials(c *gin.Context) {
	var list []models.ServerCredential
	models.DB.Order("id DESC").Find(&list)
	for i := range list {
		list[i].Password = ""
		list[i].PrivateKey = ""
		list[i].Passphrase = ""
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// CreateCredential POST /api/bastion/credentials
func CreateCredential(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required,max=100"`
		AuthType   string `json:"auth_type" binding:"required,oneof=password private_key"`
		Username   string `json:"username" binding:"required,max=100"`
		Password   string `json:"password"`
		PrivateKey string `json:"private_key"`
		Passphrase string `json:"passphrase"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误，请检查必填字段"})
		return
	}

	// 私钥模式校验：必须填写私钥内容
	if req.AuthType == "private_key" && req.PrivateKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "私钥内容不能为空"})
		return
	}
	// 私钥内容格式校验：必须包含 PEM 头
	if req.AuthType == "private_key" && req.PrivateKey != "" && !strings.HasPrefix(strings.TrimSpace(req.PrivateKey), "-----BEGIN") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "私钥格式错误，请粘贴完整的PEM格式私钥内容（需包含 -----BEGIN 开头）"})
		return
	}
	// 密码模式校验：必须填写密码
	if req.AuthType == "password" && req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "密码不能为空"})
		return
	}

	cred := models.ServerCredential{
		Name:      req.Name,
		AuthType:  req.AuthType,
		Username:  req.Username,
		CreatedBy: getCurrentUserID(c),
	}

	// 加密敏感字段
	if req.Password != "" {
		enc, err := utils.Encrypt(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "密码加密失败"})
			return
		}
		cred.Password = enc
	}
	if req.PrivateKey != "" {
		enc, err := utils.Encrypt(req.PrivateKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "私钥加密失败"})
			return
		}
		cred.PrivateKey = enc
	}
	if req.Passphrase != "" {
		enc, err := utils.Encrypt(req.Passphrase)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "私钥密码加密失败"})
			return
		}
		cred.Passphrase = enc
	}

	if err := models.DB.Create(&cred).Error; err != nil {
		// 判断是否为唯一约束冲突
		if isDuplicateEntryError(err) {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "凭证名称已存在，请使用其他名称"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建凭证失败: " + err.Error()})
		return
	}

	cred.Password = ""
	cred.PrivateKey = ""
	cred.Passphrase = ""
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cred})
}

// UpdateCredential PUT /api/bastion/credentials/:id
func UpdateCredential(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Name       string `json:"name"`
		AuthType   string `json:"auth_type"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		PrivateKey string `json:"private_key"`
		Passphrase string `json:"passphrase"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}

	// 先查出现有凭证，用于判断 auth_type 切换时的校验
	var existing models.ServerCredential
	if err := models.DB.First(&existing, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "凭证不存在"})
		return
	}

	// 判断最终生效的 auth_type
	effectiveAuthType := existing.AuthType
	if req.AuthType != "" {
		effectiveAuthType = req.AuthType
	}

	// 如果最终是私钥模式，且没传私钥内容，且原来也没有私钥，则报错
	if effectiveAuthType == "private_key" && req.PrivateKey == "" && existing.PrivateKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "私钥模式下必须提供私钥内容"})
		return
	}
	// 如果传了私钥内容，校验格式
	if req.PrivateKey != "" && !strings.HasPrefix(strings.TrimSpace(req.PrivateKey), "-----BEGIN") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "私钥格式错误，请粘贴完整的PEM格式私钥内容（需包含 -----BEGIN 开头）"})
		return
	}
	// 如果最终是密码模式，且没传密码，且原来也没有密码，则报错
	if effectiveAuthType == "password" && req.Password == "" && existing.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "密码模式下必须提供密码"})
		return
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Username != "" {
		updates["username"] = req.Username
	}
	if req.Password != "" {
		enc, err := utils.Encrypt(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "密码加密失败"})
			return
		}
		updates["password"] = enc
	}
	if req.PrivateKey != "" {
		enc, err := utils.Encrypt(req.PrivateKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "私钥加密失败"})
			return
		}
		updates["private_key"] = enc
	}
	if req.Passphrase != "" {
		enc, err := utils.Encrypt(req.Passphrase)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "私钥密码加密失败"})
			return
		}
		updates["passphrase"] = enc
	}
	if req.AuthType != "" {
		updates["auth_type"] = req.AuthType
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "无变更"})
		return
	}

	models.DB.Model(&models.ServerCredential{}).Where("id = ?", id).Updates(updates)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

// DeleteCredential DELETE /api/bastion/credentials/:id
func DeleteCredential(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var count int64
	models.DB.Model(&models.BastionGroupCredential{}).Where("credential_id = ?", id).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该凭证被用户组授权引用，无法删除"})
		return
	}
	models.DB.Delete(&models.ServerCredential{}, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// getCurrentUserID 从 gin.Context 中获取当前登录用户的 ID
func getCurrentUserID(c *gin.Context) int {
	uid, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	switch v := uid.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case uint:
		return int(v)
	case string:
		id, _ := strconv.Atoi(v)
		return id
	default:
		return 0
	}
}

// isDuplicateEntryError 判断是否为数据库唯一约束冲突错误
func isDuplicateEntryError(err error) bool {
	if err == nil {
		return false
	}
	// MySQL: Duplicate entry 'xxx' for key 'xxx'
	// SQLite: UNIQUE constraint failed
	// PostgreSQL: duplicate key value violates unique constraint
	errStr := err.Error()
	return strings.Contains(errStr, "Duplicate entry") ||
		strings.Contains(errStr, "UNIQUE constraint failed") ||
		strings.Contains(errStr, "duplicate key")
}



