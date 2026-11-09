package cloud

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud/aliyun"
	"cloud-manager-backend/internal/pkg/cloud/aws"
	"cloud-manager-backend/internal/pkg/cloud/tencent"
	"cloud-manager-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// GetEcsFilterOptions 获取ECS漏斗选项（平台、账号、状态、地域） GET /api/ecs/filter-options?platform=&account_name=
// 传入 platform / account_name 时，地域列表按该平台/账号过滤（与左侧目录树联动），其余选项保持全局供目录树使用
func GetEcsFilterOptions(c *gin.Context) {
	platform := c.Query("platform")
	accountName := c.Query("account_name")

	var platforms, accounts, statuses, regions []string

	models.DB.Model(&models.EcsInstance{}).Distinct("platform").Pluck("platform", &platforms)
	models.DB.Model(&models.EcsInstance{}).Distinct("account_name").Pluck("account_name", &accounts)
	models.DB.Model(&models.EcsInstance{}).Distinct("status").Pluck("status", &statuses)

	regionQuery := models.DB.Model(&models.EcsInstance{})
	if platform != "" {
		regionQuery = regionQuery.Where("platform = ?", platform)
	}
	if accountName != "" {
		regionQuery = regionQuery.Where("account_name = ?", accountName)
	}
	regionQuery.Distinct("region_name").Pluck("region_name", &regions)

	var accountOptions []accountOption
	models.DB.Raw("SELECT DISTINCT account_name AS name, platform FROM ecs_instances ORDER BY platform, account_name").Scan(&accountOptions)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"platforms":       platforms,
			"accounts":        accounts,
			"account_options": accountOptions,
			"statuses":        statuses,
			"regions":         regions,
		},
	})
}

// GetEcsInstances 跨平台查询ECS实例列表  GET /api/ecs?page=1&size=10&keyword=&account_id=&platform=&region_id=&is_offline=0
func GetEcsInstances(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 10000 {
		size = 10
	}

	var instances []models.EcsInstance
	var total int64

	query := models.DB.Model(&models.EcsInstance{})

	// 关键词搜索（实例名、实例ID、公网IP、弹性IP、私网IP、账户名）
	if keyword := c.Query("keyword"); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"instance_name LIKE ? OR instance_id LIKE ? OR public_ip_address LIKE ? OR eip_address LIKE ? OR private_ip_address LIKE ? OR account_name LIKE ?",
			like, like, like, like, like, like,
		)
	}

	// 按账号ID筛选
	if accountID := c.Query("account_id"); accountID != "" {
		query = query.Where("account_id = ?", accountID)
	}

	// 按账号名称筛选（支持多值，逗号分隔；前端漏斗多选时传入）
	if accountName := c.Query("account_name"); accountName != "" {
		names := splitNonEmpty(accountName)
		if len(names) == 1 {
			query = query.Where("account_name = ?", names[0])
		} else if len(names) > 1 {
			query = query.Where("account_name IN ?", names)
		}
	}

	// 按平台筛选
	if platform := c.Query("platform"); platform != "" {
		query = query.Where("platform = ?", platform)
	}

	// 按地域筛选（支持多值，逗号分隔；前端漏斗多选时传入）
	if regionName := c.Query("region_name"); regionName != "" {
		names := splitNonEmpty(regionName)
		if len(names) == 1 {
			query = query.Where("region_name = ?", names[0])
		} else if len(names) > 1 {
			query = query.Where("region_name IN ?", names)
		}
	}

	// 按实例状态筛选
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// 下线状态筛选
	if isOffline := c.Query("is_offline"); isOffline != "" {
		query = query.Where("is_offline = ?", isOffline)
	}

	query.Count(&total)
	query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&instances)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":  instances,
			"total": total,
			"page":  page,
			"size":  size,
		},
	})
}

// splitNonEmpty 按逗号拆分查询参数并剔除空值，用于支持多值 IN 查询（如 account_name=阿里云,腾讯云）
func splitNonEmpty(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ==================== 手动实例 CRUD ====================

// adminOnly 校验管理员权限
func adminOnly(c *gin.Context) bool {
	role, _ := c.Get("role")
	return role == "admin"
}

// validSSHPort 校验SSH端口：1-65535，非法返回 false
func validSSHPort(port int) bool {
	return port >= 1 && port <= 65535
}

// CreateEcsInstance POST /api/ecs (管理员)
func CreateEcsInstance(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	var req struct {
		InstanceName string `json:"instance_name" binding:"required"`
		PublicIP     string `json:"public_ip"`
		Eip          string `json:"eip"` // 弹性公网IP
		PrivateIP    string `json:"private_ip"`
		Platform     string `json:"platform"`
		AccountName  string `json:"account_name"`
		InstanceType string `json:"instance_type"`
		Cpu          int64  `json:"cpu"`
		Memory       int64  `json:"memory"`
		OsName       string `json:"os_name"`
		RegionName   string `json:"region_name"`
		ZoneName     string `json:"zone_name"`
		SSHPort      int    `json:"ssh_port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if req.SSHPort != 0 && !validSSHPort(req.SSHPort) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH端口必须在 1-65535 之间"})
		return
	}

	now := time.Now()
	sshPort := req.SSHPort
	if !validSSHPort(sshPort) {
		sshPort = models.DefaultSSHPort // 未传/非法时默认22
	}
	inst := models.EcsInstance{
		InstanceId:       fmt.Sprintf("manual-%d", now.UnixMilli()),
		InstanceName:     req.InstanceName,
		PublicIpAddress:  req.PublicIP,
		EipAddress:       req.Eip,
		PrivateIpAddress: req.PrivateIP,
		SSHPort:          sshPort,
		Platform:         req.Platform,
		AccountName:      req.AccountName,
		InstanceType:     req.InstanceType,
		Cpu:              req.Cpu,
		Memory:           req.Memory,
		OSName:           req.OsName,
		RegionName:       req.RegionName,
		ZoneName:         req.ZoneName,
		Status:           "manual",
		Source:           "manual",
		IsOffline:        0,
		Site:             "cn",
		AccountID:        "manual",
		CreationTime:     &now,
	}

	if err := models.DB.Create(&inst).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": inst})
}

// UpdateEcsInstance PUT /api/ecs/:id (管理员，仅手动实例可编辑)
func UpdateEcsInstance(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	var inst models.EcsInstance
	if err := models.DB.First(&inst, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "实例不存在"})
		return
	}
	if inst.Source != "manual" {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "云同步的实例不可编辑"})
		return
	}

	var req struct {
		InstanceName string  `json:"instance_name"`
		PublicIP     *string `json:"public_ip"` // 用指针区分"没传"和"传空"
		Eip          *string `json:"eip"`       // 弹性公网IP（指针支持清空）
		PrivateIP    *string `json:"private_ip"`
		Platform     string  `json:"platform"`
		AccountName  string  `json:"account_name"`
		InstanceType string  `json:"instance_type"`
		Cpu          int64   `json:"cpu"`
		Memory       int64   `json:"memory"`
		OsName       string  `json:"os_name"`
		RegionName   string  `json:"region_name"`
		ZoneName     string  `json:"zone_name"`
		SSHPort      *int    `json:"ssh_port"` // 指针区分"没传"和"传0/传值"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if req.SSHPort != nil && !validSSHPort(*req.SSHPort) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH端口必须在 1-65535 之间"})
		return
	}

	updates := map[string]interface{}{}
	if req.InstanceName != "" {
		updates["instance_name"] = req.InstanceName
	}
	if req.PublicIP != nil {
		updates["public_ip_address"] = *req.PublicIP
	} // 指针支持空字符串
	if req.Eip != nil {
		updates["eip_address"] = *req.Eip
	}
	if req.PrivateIP != nil {
		updates["private_ip_address"] = *req.PrivateIP
	}
	if req.Platform != "" {
		updates["platform"] = req.Platform
	}
	if req.AccountName != "" {
		updates["account_name"] = req.AccountName
	}
	if req.InstanceType != "" {
		updates["instance_type"] = req.InstanceType
	}
	if req.Cpu > 0 {
		updates["cpu"] = req.Cpu
	}
	if req.Memory > 0 {
		updates["memory"] = req.Memory
	}
	if req.OsName != "" {
		updates["os_name"] = req.OsName
	}
	if req.RegionName != "" {
		updates["region_name"] = req.RegionName
	}
	if req.ZoneName != "" {
		updates["zone_name"] = req.ZoneName
	}
	if req.SSHPort != nil {
		updates["ssh_port"] = *req.SSHPort
	}

	if len(updates) > 0 {
		if err := models.DB.Model(&inst).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

// DeleteEcsInstance DELETE /api/ecs/:id (管理员，仅手动实例可删除)
func DeleteEcsInstance(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	var inst models.EcsInstance
	if err := models.DB.First(&inst, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "实例不存在"})
		return
	}
	if inst.Source != "manual" {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "云同步的实例不可删除"})
		return
	}

	models.DB.Delete(&inst)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// UpdateEcsSSHPort PUT /api/ecs/:id/ssh-port (管理员)
// 单独维护资产的SSH端口：云同步的实例服务器改过端口后，其它字段不可编辑，
// 但端口必须能手动修正，因此该接口不限制来源（sync/manual 均可）。
func UpdateEcsSSHPort(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	var inst models.EcsInstance
	if err := models.DB.First(&inst, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "实例不存在"})
		return
	}

	var req struct {
		SSHPort int `json:"ssh_port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if !validSSHPort(req.SSHPort) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "SSH端口必须在 1-65535 之间"})
		return
	}

	if err := models.DB.Model(&inst).Update("ssh_port", req.SSHPort).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

// UpdateEcsDescription PUT /api/ecs/:id/description (管理员，云同步/手动实例均可)
// 维护实例备注（description）：以云端为主——阿里云实例会调用云端接口写回备注（ModifyInstanceAttribute），
// 云端成功后更新本地库；aws/腾讯云无备注概念（本地改后下次同步会被云覆盖）；手动实例仅更新本地库。
// 不再本地锁定（符合"以云端为主，本地改了同步云端"）。
func UpdateEcsDescription(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	var inst models.EcsInstance
	if err := models.DB.First(&inst, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "实例不存在"})
		return
	}

	var req struct {
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	desc := strings.TrimSpace(req.Description)

	// 手动实例：无云端，仅更新本地库
	if inst.Source == "manual" {
		if err := models.DB.Model(&inst).Update("description", desc).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
		return
	}

	// 云同步实例：阿里云支持写回备注，aws/腾讯云无备注概念（本地改后下次同步会被云覆盖）
	if inst.Platform == "aliyun" {
		var account models.CloudAccount
		if err := models.DB.Where("account_id = ?", inst.AccountID).First(&account).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "未找到对应云账号，无法调用云端写回备注"})
			return
		}
		secret, err := utils.Decrypt(account.AccessSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解密密钥失败: " + err.Error()})
			return
		}
		regionID, err := resolveRegionID(account.AccountID, inst.Platform, inst.RegionName)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		endpoint := resolveRegionEndpoint(account.AccountID, inst.Platform, regionID)
		if err := aliyun.UpdateInstanceDescription(account.AccessKey, secret, regionID, endpoint, inst.InstanceId, desc); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "云端写回备注失败: " + err.Error()})
			return
		}
	}

	if err := models.DB.Model(&inst).Update("description", desc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新本地库失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已同步修改云端"})
}

// UpdateEcsNameRequest 修改实例名称请求
type UpdateEcsNameRequest struct {
	InstanceName string `json:"instance_name" binding:"required,min=1,max=200"`
}

// UpdateEcsName PUT /api/ecs/:id/name (管理员)
// 修改实例名称：云同步实例直接调用云端改名接口（阿里云/AWS/腾讯云），成功后更新本地库；
// 手动实例（manual）仅更新本地库。不再锁定名称——因为改名已同步云端，下次云同步自然一致。
func UpdateEcsName(c *gin.Context) {
	if !adminOnly(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅管理员可操作"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	var req UpdateEcsNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	name := strings.TrimSpace(req.InstanceName)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "实例名称不能为空"})
		return
	}

	var inst models.EcsInstance
	if err := models.DB.First(&inst, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "实例不存在"})
		return
	}

	// 手动实例：无云端，仅更新本地库
	if inst.Source == "manual" {
		if err := models.DB.Model(&inst).Update("instance_name", name).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
		return
	}

	// 云同步实例：调用云端改名
	var account models.CloudAccount
	if err := models.DB.Where("account_id = ?", inst.AccountID).First(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "未找到对应云账号，无法调用云端改名"})
		return
	}
	secret, err := utils.Decrypt(account.AccessSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解密密钥失败: " + err.Error()})
		return
	}

	regionID, err := resolveRegionID(account.AccountID, inst.Platform, inst.RegionName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	var rerr error
	switch inst.Platform {
	case "aliyun":
		rerr = aliyun.RenameInstance(account.AccessKey, secret, regionID, resolveRegionEndpoint(account.AccountID, inst.Platform, regionID), inst.InstanceId, name)
	case "aws":
		rerr = aws.RenameInstance(account.AccessKey, secret, regionID, inst.InstanceId, name)
	case "tencent":
		rerr = tencent.RenameInstance(account.AccessKey, secret, regionID, inst.InstanceId, name)
	default:
		rerr = fmt.Errorf("不支持的平台: %s", inst.Platform)
	}
	if rerr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "云端改名失败: " + rerr.Error()})
		return
	}

	// 云端成功，更新本地库
	if err := models.DB.Model(&inst).Update("instance_name", name).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新本地库失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已同步修改云端"})
}

// resolveRegionID 根据实例中文地域名反查云地域ID（用于调用云端改名接口）
func resolveRegionID(accountID, platform, regionName string) (string, error) {
	for _, r := range models.GetEnabledRegions(platform) {
		if r.RegionName == regionName {
			return r.RegionID, nil
		}
	}
	return "", fmt.Errorf("未找到地域 %q 对应的地域ID，请确认该地域已在账号中启用", regionName)
}

// resolveRegionEndpoint 取地域对应的 API endpoint（阿里云 ECS 调用需要）
func resolveRegionEndpoint(accountID, platform, regionID string) string {
	for _, r := range models.GetEnabledRegions(platform) {
		if r.RegionID == regionID {
			return r.RegionEndpoint
		}
	}
	return ""
}
