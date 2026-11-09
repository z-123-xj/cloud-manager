package cloud

import (
	"cloud-manager-backend/internal/config"
	"cloud-manager-backend/internal/pkg/cloud/bastion"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Router 统一注册 cloud 目录下所有路由
func Router(r *gin.RouterGroup) {
	// 跨平台域名查询  GET /api/domains?page=1&size=10&keyword=&account_id=&platform=&is_offline=&expire_start=&expire_end=
	r.GET("/domains", GetDomains)
	// 域名漏斗筛选选项  GET /api/domains/filter-options
	r.GET("/domains/filter-options", GetDomainFilterOptions)
	// 跨平台查询单个域名详情  GET /api/domains/:domain
	r.GET("/domains/:domain", GetDomainDetail)
	// 域名手动 CRUD（管理员）：用于录入其它平台/未在云上同步的域名
	r.POST("/domains", CreateDomain)
	r.PUT("/domains/:domain", UpdateDomain)
	// 修改域名备注  PUT /api/domains/:id/remark（阿里云国内站同步云端，其它平台仅改本地库）
	r.PUT("/domains/:domain/remark", UpdateDomainRemarkHandler)
	r.DELETE("/domains/:domain", DeleteDomain)

	// ===== DNS可管理域名列表（从 dns_domains 表查询） =====
	// GET /api/dns-domains?page=1&size=10&keyword=
	r.GET("/dns-domains", GetDnsDomains)
	// GET /api/dns-domains/filter-options  漏斗筛选选项
	r.GET("/dns-domains/filter-options", GetDnsDomainFilterOptions)

	// ===== CDN加速域名列表（从 cdn_domains 表查询：阿里云CDN / AWS CloudFront） =====
	// GET /api/cdn-domains?page=1&size=10&keyword=&account_name=&platform=
	r.GET("/cdn-domains", GetCdnDomains)
	// GET /api/cdn-domains/filter-options  筛选选项
	r.GET("/cdn-domains/filter-options", GetCdnDomainFilterOptions)

	// ===== 域名DNS解析记录操作（实时调用云API，不写入数据库） =====
	// GET    /api/domains/:domain/dns        查询域名的DNS解析记录
	r.GET("/domains/:domain/dns", GetDomainDnsRecords)
	// POST   /api/domains/:domain/dns        添加DNS解析记录
	r.POST("/domains/:domain/dns", AddDomainDnsRecord)
	// PUT    /api/domains/:domain/dns        修改DNS解析记录
	r.PUT("/domains/:domain/dns", UpdateDomainDnsRecord)
	// DELETE /api/domains/:domain/dns        删除DNS解析记录
	r.DELETE("/domains/:domain/dns", DeleteDomainDnsRecord)
	// PUT    /api/domains/:domain/dns/status 启用/禁用DNS解析记录
	r.PUT("/domains/:domain/dns/status", SetDomainDnsRecordStatus)

	// 查询ECS实例列表  GET /api/ecs?account_id=xxx&region_id=xxx
	r.GET("/ecs", GetEcsInstances)
	// ECS漏斗筛选选项  GET /api/ecs/filter-options
	r.GET("/ecs/filter-options", GetEcsFilterOptions)

	// ===== 阿里云资源包（实时调计费 BssOpenApi，汇总国内站+国际站，不落库） =====
	// GET /api/resource-packages?account_id=xxx   （account_id 为空则汇总所有阿里云账号）
	r.GET("/resource-packages", GetResourcePackages)
	// 手动ECS实例 CRUD（管理员）
	r.POST("/ecs", CreateEcsInstance)
	r.PUT("/ecs/:id", UpdateEcsInstance)
	r.DELETE("/ecs/:id", DeleteEcsInstance)
	// 维护ECS实例SSH端口（管理员，云同步资产也可改：服务器改过端口后用）
	r.PUT("/ecs/:id/ssh-port", UpdateEcsSSHPort)
	// 维护ECS实例备注（管理员，云同步资产也可改：阿里云写回云端，以云端为主）
	r.PUT("/ecs/:id/description", UpdateEcsDescription)
	// 维护ECS实例名称（管理员，云同步资产也可改：改名并锁定，云同步不再覆盖）
	r.PUT("/ecs/:id/name", UpdateEcsName)

	// ===== 地域管理 =====
	// GET /api/regions?platform=aliyun  获取地域列表
	r.GET("/regions", GetRegionsHandler)
	// PUT /api/regions/:id  更新地域启用状态
	r.PUT("/regions/:id", UpdateRegionHandler)

	// ===== 统一同步入口（从数据库 cloud_accounts 表读取所有启用账号，按平台分发） =====
	sync := r.Group("/sync")
	{
		// POST /api/sync/domains  同步所有启用账号的域名注册列表
		sync.POST("/domains", SyncDomainsHandler)
		// POST /api/sync/dns      同步所有启用账号的DNS可管理域名列表
		sync.POST("/dns", SyncDnsDomainsHandler)
		// POST /api/sync/ecs      同步所有启用账号的ECS实例
		sync.POST("/ecs", SyncEcsHandler)
		// POST /api/sync/cdn      同步所有启用账号的CDN加速域名（阿里云CDN/AWS CloudFront）
		sync.POST("/cdn", SyncCdnHandler)
		// POST /api/sync/all      同步所有启用账号的全部资源（域名+DNS+ECS+CDN）
		sync.POST("/all", SyncAllHandler)
		// GET /api/sync/progress  同步进度 SSE 推送
		sync.GET("/progress", SyncProgressHandler)
	}

	// ===== 堡垒机（SSH资产管理），可通过 config.yaml 关闭 =====
	bc := config.GetBastionConfig()
	if bc.Enabled {
		bastion.Router(r)
	}

	// 前端配置接口
	r.GET("/config", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"bastion_enabled": bc.Enabled,
			},
		})
	})
}
