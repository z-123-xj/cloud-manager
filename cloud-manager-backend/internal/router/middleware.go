package router

import (
	"bytes"
	"cloud-manager-backend/pkg/logger"
	"io"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger 请求日志中间件：记录每个 HTTP 请求的方法、路径、参数、状态码、耗时
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 读取请求体（用于日志，之后恢复）
		var reqBody string
		if c.Request.Body != nil && c.Request.ContentLength > 0 {
			bodyBytes, _ := io.ReadAll(c.Request.Body)
			reqBody = string(bodyBytes)
			// 恢复 Body 供后续处理器读取
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// 执行请求
		c.Next()

		duration := time.Since(start)
		statusCode := c.Writer.Status()

		// 查询参数（不含 token 等敏感信息）
		queryMap := make(map[string]string)
		for k, v := range c.Request.URL.Query() {
			if k == "token" || k == "password" {
				queryMap[k] = "***"
			} else if len(v) > 0 {
				queryMap[k] = v[0]
			}
		}

		// 脱敏请求体中的敏感字段（密码、密钥、token 等），避免登录密码等写入日志泄露
		reqBody = maskSensitiveBody(reqBody)

		// 截断过长的请求体
		if len(reqBody) > 500 {
			reqBody = reqBody[:500] + "...(truncated)"
		}

		level := logger.INFO
		if statusCode >= 500 {
			level = logger.ERROR
		} else if statusCode >= 400 {
			level = logger.WARN
		}

		logger.Log(level, "http", "request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", statusCode,
			"duration_ms", duration.Milliseconds(),
			"query", queryMap,
			"body", reqBody,
			"ip", c.ClientIP(),
		)
	}
}

// 敏感字段正则：匹配 JSON 请求体中 password/secret/token/key 等敏感字段的值。
// 注意：字段名可能带前后缀（如 access_secret、client_secret、access_key），故用 [a-z0-9_]* 前后包裹，
// 避免只匹配精确字段名而漏掉 access_secret 等导致明文密钥写入日志。
var sensitiveBodyKeyRe = regexp.MustCompile(`"(?i)([a-z0-9_]*(?:password|passwd|pwd|secret|token|access_key|accesskey|authorization|new_password)[a-z0-9_]*)"\s*:\s*"[^"]*"`)

// maskSensitiveBody 将请求体 JSON 中的敏感字段值替换为 ***，避免登录密码、密钥等写入日志泄露
func maskSensitiveBody(body string) string {
	if body == "" {
		return body
	}
	return sensitiveBodyKeyRe.ReplaceAllString(body, `"$1":"***"`)
}
