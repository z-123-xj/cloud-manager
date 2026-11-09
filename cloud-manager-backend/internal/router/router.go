// internal/router/router.go
package router

import (
	"cloud-manager-backend/internal/cloud_account"
	"cloud-manager-backend/internal/config"
	"cloud-manager-backend/internal/login"
	"cloud-manager-backend/internal/pkg/cloud"
	"cloud-manager-backend/internal/user"
	"cloud-manager-backend/pkg/logger"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	// 关闭 Gin 默认日志（替换为自定义结构化日志）
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = logger.StdWriter()
	gin.DefaultErrorWriter = logger.StdWriter()

	r := gin.New()

	// 全局中间件
	r.Use(gin.Recovery())   // panic 恢复
	r.Use(RequestLogger())  // 请求日志

	api := r.Group("/api")
	{
		// /api 健康检查
		api.GET("/healthz", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"message": "ok",
			})
		})

		// 登录模块（无需认证）
		login.LoginRouter(api)

		// 以下路由需要登录认证
		auth := api.Group("")
		auth.Use(login.AuthMiddleware, login.ReadonlyMiddleware)
		{
			// 注册 cloud 模块所有路由（域名查询/同步等）—— 所有登录用户可访问
			cloud.Router(auth)

			// 云账号总数（首页展示用，无需管理员权限）
			auth.GET("/accounts/count", cloud_account.GetAccountCount)
		}

		// 个人资料 / 修改密码：所有登录用户均可维护自己的账号（不受只读中间件限制）
		selfAPI := api.Group("")
		selfAPI.Use(login.AuthMiddleware)
		{
			selfAPI.GET("/profile", user.GetProfile)
			selfAPI.PUT("/profile", user.UpdateProfile)
			selfAPI.PUT("/profile/password", user.ChangePassword)
		}

		// 以下路由仅管理员可访问
		admin := api.Group("")
		admin.Use(login.AuthMiddleware, login.AdminMiddleware)
		{
			// 注册用户模块路由
			user.UserRouter(admin)
			// 注册云账号模块路由
			cloud_account.AccountRouter(admin)
		}
	}

	// 前端静态文件服务：目录由 frontend.dir（或环境变量 FRONTEND_DIR）配置，
	// 默认 ../cloud-manager-front 兼容现有的同级目录部署。
	frontendDir := config.GetFrontendConfig().Dir

	// 辅助函数：读取并返回前端文件（禁止缓存，确保总是最新内容）
	serveFrontendFile := func(c *gin.Context, filename string) {
		fp, _ := filepath.Abs(filepath.Join(frontendDir, filename))
		data, err := os.ReadFile(fp)
		if err != nil {
			c.String(500, "文件读取失败: "+err.Error())
			return
		}
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Data(200, "text/html; charset=utf-8", data)
	}

	// 静态资源：assets 目录（CSS、JS）
	staticPath, _ := filepath.Abs(filepath.Join(frontendDir, "assets"))
	r.Static("/assets", staticPath)

	// 堡垒机终端独立页面
	r.GET("/terminal.html", func(c *gin.Context) {
		serveFrontendFile(c, "terminal.html")
	})

	// /logs.html 仅兼容旧书签；主入口已改到 /assets/pages/logs.html，由 r.Static 直接托管，无需新增路由
	r.GET("/logs.html", func(c *gin.Context) {
		serveFrontendFile(c, "assets/pages/logs.html")
	})

	// 首页：GET / → index.html
	r.GET("/", func(c *gin.Context) {
		serveFrontendFile(c, "index.html")
	})

	// SPA 路由回退：任何非 /api 开头的请求都返回 index.html
	r.NoRoute(func(c *gin.Context) {
		serveFrontendFile(c, "index.html")
	})

	return r
}
