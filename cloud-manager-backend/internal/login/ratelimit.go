package login

import (
	"time"

	"cloud-manager-backend/internal/models"
)

const (
	rateWindow   = 15 * time.Minute // 频率统计窗口
	rateMaxFails = 10               // 窗口内失败次数阈值，超过则临时阻断
	rateCleanAge = 24 * time.Hour  // 记录保留时长，过期清理
)

// RecordLoginAttempt 记录一次登录尝试（成功/失败），用于频率限制
func RecordLoginAttempt(ip, username string, success bool) {
	if models.DB == nil {
		return
	}
	models.DB.Create(&models.LoginAttempt{
		IP:       ip,
		Username: username,
		Success:  success,
	})
	// 顺手清理过期记录（低频次，影响可忽略）
	models.DB.Where("created_at < ?", time.Now().Add(-rateCleanAge)).Delete(&models.LoginAttempt{})
}

// RecentFailCount 统计某 IP 在时间窗内的失败次数
func RecentFailCount(ip string) int64 {
	var n int64
	if models.DB == nil {
		return 0
	}
	models.DB.Model(&models.LoginAttempt{}).
		Where("ip = ? AND success = ? AND created_at >= ?", ip, false, time.Now().Add(-rateWindow)).
		Count(&n)
	return n
}

// IsBlocked 失败次数超过阈值则临时阻断登录
func IsBlocked(ip string) bool {
	return RecentFailCount(ip) >= rateMaxFails
}
