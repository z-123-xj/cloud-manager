package login

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"cloud-manager-backend/internal/captcha"

	"github.com/gin-gonic/gin"
)

func LoginRouter(r *gin.RouterGroup) {
	group := r.Group("/login")
	{
		group.POST("/", loginWithGuard)
	}
	// 图形验证码（无需登录）
	r.GET("/captcha", captcha.GenerateHandler)
}

// loginWithGuard 在真正登录前做：频率限制 + 验证码校验
func loginWithGuard(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	var pre LoginRequest
	if err := json.Unmarshal(body, &pre); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请求格式错误"})
		return
	}

	ip := c.ClientIP()
	if IsBlocked(ip) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "登录尝试过于频繁，请15分钟后再试"})
		return
	}
	if !captcha.Verify(pre.CaptchaID, pre.CaptchaCode) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "验证码错误"})
		return
	}
	// 重置请求体，交给原 Login 重新解析
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	Login(c)
}