package bastion

import (
	"cloud-manager-backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ==================== 用户组资产授权（ECS） ====================

// GetGroupAssets GET /api/bastion/groups/:id/assets
func GetGroupAssets(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var assets []models.BastionGroupAsset
	models.DB.Where("group_id = ?", id).Find(&assets)

	type AssetDetail struct {
		ID          uint   `json:"id"`
		GroupID     uint   `json:"group_id"`
		EcsID       uint64 `json:"ecs_id"`
		EcsName     string `json:"ecs_name"`
		PublicIp    string `json:"public_ip"`
		PrivateIp   string `json:"private_ip"`
		EipAddress  string `json:"eip_address"`
		InstanceId  string `json:"instance_id"`
		SourceLabel string `json:"source_label"`
		CreatedAt   string `json:"created_at"`
	}
	var result []AssetDetail
	for _, a := range assets {
		ad := AssetDetail{
			ID:      a.ID,
			GroupID: a.GroupID,
			EcsID:   a.EcsID,
		}

		var ecs models.EcsInstance
		if err := models.DB.Where("id = ?", a.EcsID).First(&ecs).Error; err == nil {
			ad.EcsName = ecs.InstanceName
			ad.PublicIp = ecs.PublicIpAddress
			ad.PrivateIp = ecs.PrivateIpAddress
			ad.EipAddress = ecs.EipAddress
			ad.InstanceId = ecs.InstanceId
			if ecs.Source == "manual" {
				ad.SourceLabel = "手动"
			} else {
				ad.SourceLabel = "同步"
			}
		}

		result = append(result, ad)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// AddGroupAssets POST /api/bastion/groups/:id/assets
func AddGroupAssets(c *gin.Context) {
	groupID, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		EcsID  uint64   `json:"ecs_id"`
		EcsIDs []uint64 `json:"ecs_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}

	ecsIDs := req.EcsIDs
	if req.EcsID > 0 {
		ecsIDs = []uint64{req.EcsID}
	}
	if len(ecsIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请选择ECS实例"})
		return
	}

	created := 0
	for _, ecsID := range ecsIDs {
		asset := models.BastionGroupAsset{
			GroupID: uint(groupID),
			EcsID:   ecsID,
		}
		// 唯一约束 (group_id, ecs_id) 会阻止重复，忽略即可
		if err := models.DB.Where("group_id = ? AND ecs_id = ?", groupID, ecsID).First(&models.BastionGroupAsset{}).Error; err == nil {
			continue
		}
		if err := models.DB.Create(&asset).Error; err == nil {
			created++
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "授权完成", "count": created})
}

// DeleteGroupAsset DELETE /api/bastion/groups/:gid/assets/:aid
func DeleteGroupAsset(c *gin.Context) {
	aid, _ := strconv.Atoi(c.Param("aid"))
	models.DB.Delete(&models.BastionGroupAsset{}, aid)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "移除成功"})
}

// DeleteGroupAssets POST /api/bastion/groups/assets/batch-delete
func DeleteGroupAssets(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	models.DB.Where("id IN ?", req.IDs).Delete(&models.BastionGroupAsset{})
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// ==================== 用户组凭证授权 ====================

// GetGroupCredentials GET /api/bastion/groups/:id/credentials
func GetGroupCredentials(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var groupCreds []models.BastionGroupCredential
	models.DB.Where("group_id = ?", id).Find(&groupCreds)

	type CredDetail struct {
		ID             uint   `json:"id"`
		GroupID        uint   `json:"group_id"`
		CredentialID   uint   `json:"credential_id"`
		CredentialName string `json:"credential_name"`
		Username       string `json:"username"`
		AuthType       string `json:"auth_type"`
	}
	var result []CredDetail
	for _, gc := range groupCreds {
		cd := CredDetail{
			ID:           gc.ID,
			GroupID:      gc.GroupID,
			CredentialID: gc.CredentialID,
		}

		var cred models.ServerCredential
		if err := models.DB.Where("id = ?", gc.CredentialID).First(&cred).Error; err == nil {
			cd.CredentialName = cred.Name
			cd.Username = cred.Username
			cd.AuthType = cred.AuthType
		}

		result = append(result, cd)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// AddGroupCredentials POST /api/bastion/groups/:id/credentials
func AddGroupCredentials(c *gin.Context) {
	groupID, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		CredentialID  uint   `json:"credential_id"`
		CredentialIDs []uint `json:"credential_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}

	credIDs := req.CredentialIDs
	if req.CredentialID > 0 {
		credIDs = []uint{req.CredentialID}
	}
	if len(credIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请选择凭证"})
		return
	}

	created := 0
	for _, cid := range credIDs {
		// 唯一约束 (group_id, credential_id) 阻止重复
		if err := models.DB.Where("group_id = ? AND credential_id = ?", groupID, cid).First(&models.BastionGroupCredential{}).Error; err == nil {
			continue
		}
		gc := models.BastionGroupCredential{
			GroupID:      uint(groupID),
			CredentialID: cid,
		}
		if err := models.DB.Create(&gc).Error; err == nil {
			created++
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "授权完成", "count": created})
}

// DeleteGroupCredential DELETE /api/bastion/groups/:gid/credentials/:cid
func DeleteGroupCredential(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Param("cid"))
	models.DB.Delete(&models.BastionGroupCredential{}, cid)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "移除成功"})
}

// DeleteGroupCredentials POST /api/bastion/groups/credentials/batch-delete
func DeleteGroupCredentials(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	models.DB.Where("id IN ?", req.IDs).Delete(&models.BastionGroupCredential{})
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// ==================== 用户可见资产列表 ====================

// GetUserAssets GET /api/bastion/assets
// 返回当前用户所在组被授权访问的 ECS 实例
func GetUserAssets(c *gin.Context) {
	uidStr, _ := c.Get("user_id")
	uid, _ := strconv.Atoi(uidStr.(string))
	role, _ := c.Get("role")

	var ecsList []models.EcsInstance

	if role == "admin" {
		// 管理员看所有已授权给任意用户组的ECS
		var ecsIDs []uint64
		models.DB.Model(&models.BastionGroupAsset{}).Distinct("ecs_id").Pluck("ecs_id", &ecsIDs)
		if len(ecsIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": []interface{}{}})
			return
		}
		models.DB.Where("id IN ?", ecsIDs).Order("id DESC").Find(&ecsList)
	} else {
		// 根据用户组查询授权的ECS
		var groupIDs []uint
		models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
		if len(groupIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": []interface{}{}})
			return
		}
		var ecsIDs []uint64
		models.DB.Model(&models.BastionGroupAsset{}).Where("group_id IN ?", groupIDs).Distinct("ecs_id").Pluck("ecs_id", &ecsIDs)
		if len(ecsIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": []interface{}{}})
			return
		}
		models.DB.Where("id IN ?", ecsIDs).Order("id DESC").Find(&ecsList)
	}

	type EcsWithSource struct {
		models.EcsInstance
		SourceLabel string `json:"source_label"`
	}
	var result []EcsWithSource
	for _, e := range ecsList {
		label := "同步"
		if e.Source == "manual" {
			label = "手动"
		}
		result = append(result, EcsWithSource{EcsInstance: e, SourceLabel: label})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// ==================== 用户可见凭证列表 ====================

// GetAssetCredentials GET /api/bastion/assets/:id/credentials
// 返回当前用户可以用于连接该ECS的凭证列表
func GetAssetCredentials(c *gin.Context) {
	uidStr, _ := c.Get("user_id")
	uid, _ := strconv.Atoi(uidStr.(string))
	role, _ := c.Get("role")

	var credentialIDs []uint

	if role == "admin" {
		// 管理员：所有凭证都可用
		models.DB.Model(&models.ServerCredential{}).Order("id ASC").Pluck("id", &credentialIDs)
	} else {
		// 普通用户：通过用户组查授权凭证
		var groupIDs []uint
		models.DB.Model(&models.UserGroupMember{}).Where("user_id = ?", uid).Pluck("group_id", &groupIDs)
		if len(groupIDs) == 0 {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": []interface{}{}})
			return
		}
		models.DB.Model(&models.BastionGroupCredential{}).
			Where("group_id IN ?", groupIDs).
			Distinct("credential_id").
			Pluck("credential_id", &credentialIDs)
	}

	if len(credentialIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": []interface{}{}})
		return
	}

	var creds []models.ServerCredential
	models.DB.Where("id IN ?", credentialIDs).Order("id ASC").Find(&creds)
	for i := range creds {
		creds[i].Password = ""
		creds[i].PrivateKey = ""
		creds[i].Passphrase = ""
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": creds})
}

// ==================== 兼容旧表（已废弃，保留用于过渡） ====================

// 保留旧的 BastionAssetBinding 相关函数，但不再注册路由
// 以下为过渡兼容代码，后续版本可删除

// GetGroupBindings 已废弃，使用 GetGroupAssets + GetGroupCredentials 替代
func GetGroupBindings(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"code": 410, "msg": "接口已废弃，请使用 /groups/:id/assets 和 /groups/:id/credentials"})
}

func AddGroupBinding(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"code": 410, "msg": "接口已废弃"})
}

func DeleteBindings(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"code": 410, "msg": "接口已废弃"})
}

func DeleteBinding(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"code": 410, "msg": "接口已废弃"})
}
