package dns

// 设置DNS解析记录启用/禁用状态
//
// AWS Route53 没有记录级"启用/禁用"概念（记录创建即生效，不存在暂停中的记录），
// 因此不提供等价操作，直接返回明确错误，避免与 Cloudflare 一样把其它语义误映射为启停状态。
// 需要"停用"效果请删除记录，需要时再添加。

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetStatusRequest 设置DNS解析记录状态请求参数（与阿里云/腾讯云前端字段保持一致）
type SetStatusRequest struct {
	RecordID string `json:"record_id" binding:"required"`
	Status   string `json:"status" binding:"required"`
}

// SetStatusHandler PUT /api/domains/:domain/dns/status
func SetStatusHandler(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"code": 400,
		"msg":  "AWS Route53 解析记录没有启用/禁用状态（记录创建即生效），停用请删除记录，需要时再重新添加",
	})
}
