package bastion

import (
	"cloud-manager-backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ==================== 用户组管理 ====================

// ListGroups GET /api/bastion/groups
func ListGroups(c *gin.Context) {
	var groups []models.UserGroup
	models.DB.Order("id DESC").Find(&groups)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": groups})
}

// CreateGroup POST /api/bastion/groups
func CreateGroup(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required,max=100"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	g := models.UserGroup{Name: req.Name, Description: req.Description}
	if err := models.DB.Create(&g).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": g})
}

// UpdateGroup PUT /api/bastion/groups/:id
func UpdateGroup(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	u := map[string]interface{}{}
	if req.Name != "" {
		u["name"] = req.Name
	}
	if req.Description != "" {
		u["description"] = req.Description
	}
	models.DB.Model(&models.UserGroup{}).Where("id = ?", id).Updates(u)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

// DeleteGroup DELETE /api/bastion/groups/:id
func DeleteGroup(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	models.DB.Where("group_id = ?", id).Delete(&models.UserGroupMember{})
	models.DB.Where("group_id = ?", id).Delete(&models.BastionGroupAsset{})
	models.DB.Where("group_id = ?", id).Delete(&models.BastionGroupCredential{})
	models.DB.Delete(&models.UserGroup{}, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// ==================== 组成员管理 ====================

// GetGroupMembers GET /api/bastion/groups/:id/members
func GetGroupMembers(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var memberIDs []uint
	models.DB.Model(&models.UserGroupMember{}).Where("group_id = ?", id).Pluck("user_id", &memberIDs)

	// 返回完整用户信息
	var users []models.User
	if len(memberIDs) > 0 {
		models.DB.Where("id IN ?", memberIDs).Find(&users)
		for i := range users {
			users[i].Password = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": users})
}

// AddGroupMember POST /api/bastion/groups/:id/members
func AddGroupMember(c *gin.Context) {
	groupID, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		UserID uint `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	// 去重
	var exist int64
	models.DB.Model(&models.UserGroupMember{}).Where("group_id = ? AND user_id = ?", groupID, req.UserID).Count(&exist)
	if exist > 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已存在"})
		return
	}
	m := models.UserGroupMember{GroupID: uint(groupID), UserID: req.UserID}
	models.DB.Create(&m)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": m})
}

// RemoveGroupMember DELETE /api/bastion/groups/:id/members/:user_id
func RemoveGroupMember(c *gin.Context) {
	groupID, _ := strconv.Atoi(c.Param("id"))
	userID, _ := strconv.Atoi(c.Param("user_id"))
	models.DB.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&models.UserGroupMember{})
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "移除成功"})
}
