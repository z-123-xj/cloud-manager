package cloud

import (
	"cloud-manager-backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetRegionsHandler GET /api/regions?platform=aliyun 获取地域列表
func GetRegionsHandler(c *gin.Context) {
	platform := c.DefaultQuery("platform", "aliyun")

	var regions []models.CloudRegion
	models.DB.Where("platform = ?", platform).Order("id ASC").Find(&regions)

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": regions})
}

// UpdateRegionHandler PUT /api/regions/:id 更新单个地域启用状态
func UpdateRegionHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的ID"})
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "请求参数错误"})
		return
	}

	if req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 enabled 字段"})
		return
	}

	if err := models.DB.Model(&models.CloudRegion{}).Where("id = ?", id).Update("enabled", *req.Enabled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "更新失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}
