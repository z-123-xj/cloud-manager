package cloud_account

import "github.com/gin-gonic/gin"

func AccountRouter(r *gin.RouterGroup) {
	group := r.Group("/account")
	{
		group.GET("/", GetAccountList)
		group.POST("/", CreateAccount)
		group.GET("/:id", GetAccountByID)
		group.PUT("/:id", UpdateAccount)
		group.DELETE("/:id", DeleteAccount)
	}
}
