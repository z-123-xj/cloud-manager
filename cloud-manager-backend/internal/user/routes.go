// internal/user/router.go
package user

import (
	"github.com/gin-gonic/gin"
)

func UserRouter(r *gin.RouterGroup) {
	userGroup := r.Group("/user")
	{
		userGroup.GET("/", GetUsers)            // GET  /api/user  查询列表
		userGroup.GET("/:id", GetUser)          // GET  /api/user/:id 查询单个
		userGroup.POST("/", CreateUser)         // POST /api/user  创建用户
		userGroup.PUT("/:id", UpdateUser)       // PUT  /api/user/:id 更新用户
		userGroup.PUT("/:id/unlock", UnlockUser) // PUT /api/user/:id/unlock 解锁账号
		userGroup.DELETE("/:id", DeleteUser)    // DELETE /api/user/:id 删除用户
	}
}

// func UserRouter(r *gin.Engine) {
// 	userGroup  := r.Group("/user")
// 	{
// 		// userGroup.GET("/", func(c *gin.Context) {
// 		// 	c.String(200, "首页")
// 		// })
// 		userGroup.GET("/list", func(c *gin.Context) {
// 			c.JSON(200, gin.H{
// 				"message": "user list",
// 			})
// 		})
// 		userGroup.GET("/", GetUsers)         // GET  /api/v1/users  查询列表
//         userGroup.GET("/:id", GetUser)       // GET  /api/v1/users/1 查询单个
//         userGroup.POST("/", CreateUser)      // POST /api/v1/users  创建用户
//         userGroup.PUT("/:id", UpdateUser)    // PUT  /api/v1/users/1 更新用户
//         userGroup.DELETE("/:id", DeleteUser) // DELETE /api/v1/users/1 删除用户
// 	}
// }
