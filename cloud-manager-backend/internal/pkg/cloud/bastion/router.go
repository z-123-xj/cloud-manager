package bastion

import (
	"github.com/gin-gonic/gin"
)

// Router 注册堡垒机所有路由
func Router(r *gin.RouterGroup) {
	b := r.Group("/bastion")
	{
		// 凭证管理
		b.GET("/credentials", ListCredentials)
		admin := b.Group("")
		admin.Use(adminOnly)
		{
			admin.POST("/credentials", CreateCredential)
			admin.PUT("/credentials/:id", UpdateCredential)
			admin.DELETE("/credentials/:id", DeleteCredential)

			// 脚本库写操作（仅管理员）
			admin.POST("/scripts", SaveScript)
			admin.POST("/scripts/upload", UploadScript)
			admin.DELETE("/scripts", DeleteScript)
		}

		// 用户组管理（管理员）
		adminGroups := b.Group("/groups")
		adminGroups.Use(adminOnly)
		{
			adminGroups.GET("", ListGroups)
			adminGroups.POST("", CreateGroup)
			adminGroups.PUT("/:id", UpdateGroup)
			adminGroups.DELETE("/:id", DeleteGroup)
			adminGroups.GET("/:id/members", GetGroupMembers)
			adminGroups.POST("/:id/members", AddGroupMember)
			adminGroups.DELETE("/:id/members/:user_id", RemoveGroupMember)

			// 组资产授权（ECS）— 批量操作需在动态路由前注册
			adminGroups.POST("/assets/batch-delete", DeleteGroupAssets)
			adminGroups.GET("/:id/assets", GetGroupAssets)
			adminGroups.POST("/:id/assets", AddGroupAssets)
			adminGroups.DELETE("/:id/assets/:aid", DeleteGroupAsset)

			// 组凭证授权 — 批量操作需在动态路由前注册
			adminGroups.POST("/credentials/batch-delete", DeleteGroupCredentials)
			adminGroups.GET("/:id/credentials", GetGroupCredentials)
			adminGroups.POST("/:id/credentials", AddGroupCredentials)
			adminGroups.DELETE("/:id/credentials/:cid", DeleteGroupCredential)
		}

		// 脚本库（登录用户可读，目录自动发现；写操作在上面 admin 组）
		b.GET("/scripts", ListScripts)
		b.GET("/scripts/detail", GetScript)
		b.GET("/scripts/preview", PreviewScript)

		// 资产列表（登录用户可见已授权的ECS）
		b.GET("/assets", GetUserAssets)
		b.GET("/assets/:id/credentials", GetAssetCredentials)

		// 批量命令/脚本执行（只读账号由全局 ReadonlyMiddleware 拦截 POST）
		b.GET("/batch/options", GetBatchOptions)
		b.GET("/batch/tasks", ListBatchTasks)
		b.POST("/batch/tasks", RunBatchTask)
		b.GET("/batch/tasks/:id", GetBatchTask)
		b.POST("/batch/tasks/:id/cancel", CancelBatchTask)

		// 日志排查 v2：路径探查(Explore) + awk 聚合(Analyze，异步任务)
		b.POST("/logs/explore", ExploreLogs)
		b.POST("/logs/analyze", AnalyzeLogs)
		b.GET("/logs/task/:id", GetLogTask)

		// SSH 终端 + 文件（需登录）
		b.GET("/ws/:asset_id", SSHTerminal)
		b.POST("/disconnect", DisconnectSession)
		b.POST("/upload/:asset_id", UploadFile)
		b.GET("/download/:asset_id", DownloadFile)
		b.GET("/ls/:asset_id", ListDir)
		b.DELETE("/rm/:asset_id", DeleteFile)
		// 分片上传（断点续传 + MD5 校验）
		b.GET("/upload-status/:asset_id", UploadStatus)
		b.POST("/upload-chunk/:asset_id", UploadChunk)
	}
}

func adminOnly(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "admin" {
		c.JSON(403, gin.H{"code": 403, "msg": "仅管理员可操作"})
		c.Abort()
		return
	}
	c.Next()
}
