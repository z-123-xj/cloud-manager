package models

import (
	"fmt"
	"time"
)

// DefaultSSHPort 云同步资产入库时的默认 SSH 端口（服务器改过端口的资产在平台上手动维护）
const DefaultSSHPort = 22

// UnifiedEcsItem 统一ECS实例结构体，各平台API数据统一转为此结构再写入数据库
type UnifiedEcsItem struct {
	InstanceId       string
	InstanceName     string
	Description      string
	Status           string
	Cpu              int64
	Memory           int64 // GB
	InstanceType     string
	OSName           string
	PublicIpAddress  string
	EipAddress       string
	PrivateIpAddress string
	RegionName       string
	ZoneName         string
	CreationTime     string // ISO 8601
}

// SyncEcsToDB 将统一结构的ECS实例同步到数据库（所有平台共用）
// 策略：InstanceId + AccountID 作为唯一键
//   - 不存在 → 新增
//   - 存在且字段有变化 → 更新，is_offline 置 0
//   - 存在且字段无变化 → 只更新 updated_at
func SyncEcsToDB(ecsList []UnifiedEcsItem, accountID, accountName, platform, site string) {
	for _, item := range ecsList {
		var existing EcsInstance
		err := DB.Where("account_id = ? AND instance_id = ?", accountID, item.InstanceId).First(&existing).Error

		creationTime := ParseEcsTime(item.CreationTime)

		if err != nil {
			// 数据库不存在 → 新增
			ecs := EcsInstance{
				AccountID:        accountID,
				Platform:         platform,
				Site:             site,
				AccountName:      accountName,
				InstanceId:       item.InstanceId,
				InstanceName:     item.InstanceName,
				Description:      item.Description,
				Status:           item.Status,
				Cpu:              item.Cpu,
				Memory:           item.Memory,
				InstanceType:     item.InstanceType,
				OSName:           item.OSName,
				PublicIpAddress:  item.PublicIpAddress,
				EipAddress:       item.EipAddress,
				PrivateIpAddress: item.PrivateIpAddress,
				SSHPort:          DefaultSSHPort, // 云同步资产默认22端口，平台上可手动维护
				RegionName:       item.RegionName,
				ZoneName:         item.ZoneName,
				CreationTime:     creationTime,
				IsOffline:        0,
			}
			if createErr := DB.Create(&ecs).Error; createErr != nil {
				fmt.Printf("  插入ECS失败 [%s]: %v\n", item.InstanceId, createErr)
			} else {
				fmt.Printf("  新增ECS: %s (%s)\n", item.InstanceName, item.InstanceId)
			}
		} else {
			// 数据库已存在 → 对比字段，有变化才更新
			// 注意：ssh_port 为平台手动维护字段（云API不返回端口），此处刻意不参与比对，
			// 避免每日同步覆盖用户在平台上修改过的端口
			needUpdate := false
			updates := map[string]interface{}{}

			// 实例名称：云端改名后会同步回库，这里始终以云端为准
			if existing.InstanceName != item.InstanceName {
				updates["instance_name"] = item.InstanceName
				needUpdate = true
			}
			// 备注以云端为主：每次同步用云上值覆盖本地（不再本地锁定）
			if existing.Description != item.Description {
				updates["description"] = item.Description
				needUpdate = true
			}
			if existing.Status != item.Status {
				updates["status"] = item.Status
				needUpdate = true
			}
			if existing.Cpu != item.Cpu {
				updates["cpu"] = item.Cpu
				needUpdate = true
			}
			if existing.Memory != item.Memory {
				updates["memory"] = item.Memory
				needUpdate = true
			}
			if existing.InstanceType != item.InstanceType {
				updates["instance_type"] = item.InstanceType
				needUpdate = true
			}
			if existing.OSName != item.OSName {
				updates["os_name"] = item.OSName
				needUpdate = true
			}
			if existing.PublicIpAddress != item.PublicIpAddress {
				updates["public_ip_address"] = item.PublicIpAddress
				needUpdate = true
			}
			if existing.EipAddress != item.EipAddress {
				updates["eip_address"] = item.EipAddress
				needUpdate = true
			}
			if existing.PrivateIpAddress != item.PrivateIpAddress {
				updates["private_ip_address"] = item.PrivateIpAddress
				needUpdate = true
			}
			if existing.RegionName != item.RegionName {
				updates["region_name"] = item.RegionName
				needUpdate = true
			}
			if existing.ZoneName != item.ZoneName {
				updates["zone_name"] = item.ZoneName
				needUpdate = true
			}
			if !ecsTimePtrEqual(existing.CreationTime, creationTime) {
				updates["creation_time"] = creationTime
				needUpdate = true
			}
			if existing.AccountName != accountName {
				updates["account_name"] = accountName
				needUpdate = true
			}
			if existing.Site != site {
				updates["site"] = site
				needUpdate = true
			}
			// 之前标记下线的重新上线
			if existing.IsOffline != 0 {
				updates["is_offline"] = 0
				needUpdate = true
			}

			if needUpdate {
				if updateErr := DB.Model(&existing).Updates(updates).Error; updateErr != nil {
					fmt.Printf("  更新ECS失败 [%s]: %v\n", item.InstanceId, updateErr)
				} else {
					fmt.Printf("  更新ECS: %s (%s)\n", item.InstanceName, item.InstanceId)
				}
			} else {
				// 字段无变化，只刷新 updated_at
				DB.Model(&existing).Update("updated_at", time.Now())
			}
		}
	}
}

// ParseEcsTime 解析时间字符串，支持多种格式，空字符串返回 nil
func ParseEcsTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	formats := []string{
		time.RFC3339,                // 2026-06-15T07:06:00Z
		"2006-01-02T15:04:05-07:00", // 带时区偏移
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04Z07:00",   // 阿里云 ECS: 2026-09-21T08:00Z（UTC，精确到分钟、无秒）
		"2006-01-02T15:04",         // 无秒且无时区
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

// ecsTimePtrEqual 比较两个 *time.Time 是否相等（nil == nil 视为相等）
func ecsTimePtrEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}
