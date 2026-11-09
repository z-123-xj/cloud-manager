package user

// 个人资料 / 修改密码：所有登录用户均可维护自己的资料与密码（不受只读中间件限制，
// 因为这是账号对自身的操作，不涉及平台数据的读写）。

import (
	"net/http"
	"strconv"

	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// GetProfile 获取当前登录用户的个人资料 GET /api/profile
func GetProfile(c *gin.Context) {
	uid, _ := strconv.Atoi(c.GetString("user_id"))
	var u models.User
	if err := models.DB.Where("id = ?", uid).First(&u).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"id":        u.ID,
			"username":  u.Username,
			"email":     u.Email,
			"real_name": u.RealName,
			"role":      u.Role,
			"add_time":  u.AddTime,
		},
	})
}

// UpdateProfileRequest 更新个人资料请求
type UpdateProfileRequest struct {
	RealName string `json:"real_name"`
	Email    string `json:"email" binding:"omitempty,email"`
}

// UpdateProfile 更新当前登录用户的个人资料（真实姓名/邮箱） PUT /api/profile
func UpdateProfile(c *gin.Context) {
	uid, _ := strconv.Atoi(c.GetString("user_id"))
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误：" + err.Error()})
		return
	}
	var u models.User
	if err := models.DB.Where("id = ?", uid).First(&u).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
		return
	}

	updates := map[string]interface{}{}
	if req.RealName != "" && req.RealName != u.RealName {
		updates["real_name"] = req.RealName
	}
	if req.Email != "" && req.Email != u.Email {
		var cnt int64
		models.DB.Model(&models.User{}).Where("email = ? AND id <> ?", req.Email, uid).Count(&cnt)
		if cnt > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该邮箱已被其它账号使用"})
			return
		}
		updates["email"] = req.Email
	}
	if len(updates) > 0 {
		if err := models.DB.Model(&u).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败：" + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已保存"})
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// ChangePassword 修改当前登录用户的密码 PUT /api/profile/password
// 需校验原密码；新密码至少 6 位且不能与原密码相同。
func ChangePassword(c *gin.Context) {
	uid, _ := strconv.Atoi(c.GetString("user_id"))
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误：新密码至少 6 位"})
		return
	}
	var u models.User
	if err := models.DB.Where("id = ?", uid).First(&u).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
		return
	}

	// 校验原密码（数据库中为 AES 密文，解密后比对）
	plain, err := utils.Decrypt(u.Password)
	if err != nil || plain != req.OldPassword {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "原密码不正确"})
		return
	}
	if req.OldPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "新密码不能与原密码相同"})
		return
	}

	enc, err := utils.Encrypt(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "加密失败"})
		return
	}
	if err := models.DB.Model(&u).Updates(map[string]interface{}{
		"password":        enc,
		"login_attempts":  0,
		"locked":          false,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "密码修改成功"})
}
