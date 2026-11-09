package login

import (
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/pkg/utils"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id"`   // 图形验证码ID（由外层 loginWithGuard 校验）
	CaptchaCode string `json:"captcha_code"` // 图形验证码答案（由外层 loginWithGuard 校验）
}

// Login 用户登录  POST /api/login
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 不暴露内部验证细节，统一返回友好提示
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请输入正确的用户名和密码"})
		return
	}

	// 频率限制/验证码已在外层 loginWithGuard 校验；此处仅记录本次尝试结果用于限流
	ip := c.ClientIP()
	succeeded := false
	defer func() {
		RecordLoginAttempt(ip, req.Username, succeeded)
	}()

	// 查询用户
	var user models.User
	if err := models.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在"})
		return
	}

	// 检查账号是否被锁定
	if user.Locked {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "账号已被锁定，请联系管理员解锁"})
		return
	}

	// 解密数据库中的密码，与传入明文比对
	plainPassword, err := utils.Decrypt(user.Password)
	if err != nil {
		// 解密失败（密钥不匹配或数据损坏），记录日志，视为密码错误
		log.Printf("[WARN] 用户 %s(ID=%d) 密码解密失败: %v", user.Username, user.ID, err)
		user.LoginAttempts++
		remaining := models.MaxLoginAttempts - user.LoginAttempts
		if user.LoginAttempts >= models.MaxLoginAttempts {
			user.Locked = true
			models.DB.Model(&user).Updates(map[string]interface{}{
				"login_attempts": user.LoginAttempts,
				"locked":         true,
			})
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": fmt.Sprintf("密码错误 %d 次，账号已被锁定，请联系管理员解锁", models.MaxLoginAttempts)})
			return
		}
		models.DB.Model(&user).Update("login_attempts", user.LoginAttempts)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": fmt.Sprintf("密码错误，还剩 %d 次机会", remaining)})
		return
	}
	if plainPassword != req.Password {
		// 密码错误：递增失败次数
		user.LoginAttempts++
		remaining := models.MaxLoginAttempts - user.LoginAttempts
		if user.LoginAttempts >= models.MaxLoginAttempts {
			user.Locked = true
			models.DB.Model(&user).Updates(map[string]interface{}{
				"login_attempts": user.LoginAttempts,
				"locked":         true,
			})
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": fmt.Sprintf("密码错误 %d 次，账号已被锁定，请联系管理员解锁", models.MaxLoginAttempts)})
			return
		}
		models.DB.Model(&user).Update("login_attempts", user.LoginAttempts)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": fmt.Sprintf("密码错误，还剩 %d 次机会", remaining)})
		return
	}

	// 登录成功：重置失败次数
	if user.LoginAttempts > 0 {
		models.DB.Model(&user).Update("login_attempts", 0)
	}

	// 生成 token（AES 加密: "userID:role:过期时间戳"）
	expireAt := time.Now().Add(24 * time.Hour).Unix()
	tokenPlain := fmt.Sprintf("%d:%s:%d", user.ID, user.Role, expireAt)
	token, err := utils.Encrypt(tokenPlain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "令牌生成失败"})
		return
	}

	succeeded = true
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "登录成功",
		"data": gin.H{
			"token":      token,
			"user_id":    user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"role":       user.Role,
			"expires_at": expireAt,
		},
	})
}

// ParseToken 解析 token，返回 userID、role 和是否有效
func ParseToken(token string) (userID int, role string, ok bool) {
	plain, err := utils.Decrypt(token)
	if err != nil {
		return 0, "", false
	}

	parts := strings.Split(plain, ":")
	var expireAt int64
	if len(parts) == 3 {
		// 新格式: userID:role:expireAt
		userID, _ = strconv.Atoi(parts[0])
		role = parts[1]
		expireAt, _ = strconv.ParseInt(parts[2], 10, 64)
	} else if len(parts) == 2 {
		// 旧格式: userID:expireAt
		userID, _ = strconv.Atoi(parts[0])
		role = ""
		expireAt, _ = strconv.ParseInt(parts[1], 10, 64)
	} else {
		return 0, "", false
	}

	if time.Now().Unix() > expireAt {
		return 0, "", false
	}

	return userID, role, true
}

// GetUserID 从 token 中获取用户 ID（快速方法）
func GetUserID(token string) int {
	userID, _, ok := ParseToken(token)
	if !ok {
		return 0
	}
	return userID
}

// AuthMiddleware 登录认证中间件
func AuthMiddleware(c *gin.Context) {
	token := c.GetHeader("Authorization")
	// WebSocket 不支持自定义 Header，兼容从 URL Query 传 token
	if token == "" {
		token = c.Query("token")
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未提供认证令牌"})
		c.Abort()
		return
	}

	// 支持 "Bearer xxx" 和直接传 token
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	userID, role, ok := ParseToken(token)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "令牌无效或已过期"})
		c.Abort()
		return
	}

	// 兼容旧 token（不含 role）：回退到查数据库
	if role == "" {
		var user models.User
		if err := models.DB.Select("role").Where("id = ?", userID).First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在"})
			c.Abort()
			return
		}
		role = user.Role
	}

	c.Set("user_id", strconv.Itoa(userID))
	c.Set("role", role)
	c.Next()
}

// AdminMiddleware 管理员权限中间件（需在 AuthMiddleware 之后使用）
func AdminMiddleware(c *gin.Context) {
	role, _ := c.Get("role")
	if role != models.RoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权限，仅管理员可操作"})
		c.Abort()
		return
	}
	c.Next()
}

// ReadonlyMiddleware 只读账号限制中间件（需在 AuthMiddleware 之后使用）
// 只读账号只能访问 GET 请求，POST/PUT/DELETE 等写操作全部拒绝
func ReadonlyMiddleware(c *gin.Context) {
	role, _ := c.Get("role")
	if role != models.RoleReadonly {
		c.Next()
		return
	}
	// 只读账号仅允许 GET 请求
	if c.Request.Method != http.MethodGet {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "只读账号无操作权限"})
		c.Abort()
		return
	}
	c.Next()
}
