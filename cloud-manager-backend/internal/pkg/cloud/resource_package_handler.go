package cloud

import (
	"net/http"

	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun"
	"cloud-manager-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// GetResourcePackages GET /api/resource-packages?account_id=xxx
//
// 实时查询阿里云资源包（BssOpenApi QueryResourcePackageInstances），汇总所有阿里云账号（国内站+国际站）。
// 该接口不落库（资源包是账号级实时数据，剩余量随时变化，实时查最准），按账号遍历后合并返回。
func GetResourcePackages(c *gin.Context) {
	accountIDFilter := c.Query("account_id")

	var accounts []models.CloudAccount
	q := models.DB.Where("platform = ? AND enabled = ?", "aliyun", true)
	if accountIDFilter != "" {
		q = q.Where("account_id = ?", accountIDFilter)
	}
	q.Find(&accounts)

	type accountOption struct {
		AccountID   string `json:"account_id"`
		AccountName string `json:"account_name"`
		Site        string `json:"site"`
	}

	var options []accountOption
	var allList []aliyun.ResourcePackageItem
	var fetchErrors []gin.H

	for _, acc := range accounts {
		options = append(options, accountOption{
			AccountID:   acc.AccountID,
			AccountName: acc.Name,
			Site:        acc.Site,
		})

		secret, err := utils.Decrypt(acc.AccessSecret)
		if err != nil {
			fetchErrors = append(fetchErrors, gin.H{
				"account_name": acc.Name,
				"site":         acc.Site,
				"error":        "账号凭证解密失败",
			})
			continue
		}

		items, err := aliyun.QueryResourcePackages(acc.AccessKey, secret, acc.Site, acc.AccountID, acc.Name)
		if err != nil {
			fetchErrors = append(fetchErrors, gin.H{
				"account_name": acc.Name,
				"site":         acc.Site,
				"error":        err.Error(),
			})
			continue
		}
		allList = append(allList, items...)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":     allList,
			"accounts": options,
			"errors":   fetchErrors,
		},
	})
}
