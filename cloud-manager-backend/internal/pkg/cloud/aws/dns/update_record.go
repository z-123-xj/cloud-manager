package dns

// 修改DNS解析记录（Route53 无独立 update，采用「删除旧记录 + UPSERT 新记录」语义）
// 文档: https://docs.aws.amazon.com/Route53/latest/APIReference/API_ChangeResourceRecordSets.html

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/gin-gonic/gin"
)

// UpdateRecordRequest 修改DNS解析记录请求参数（字段与阿里云/腾讯云/Cloudflare 前端一致）
type UpdateRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"`
	RR       string `json:"rr"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	TTL      int64  `json:"ttl"`
	Priority int64  `json:"priority"`
	Line     string `json:"line"`
	Weight   int    `json:"weight"`
	Remark   string `json:"remark"`
	Status   string `json:"status"`
	Proxied  *bool  `json:"proxied"`
}

// UpdateRecordHandler PUT /api/domains/:domain/dns
func UpdateRecordHandler(c *gin.Context) {
	var req UpdateRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	domainName := c.Param("domain")
	accessKeyID, accessKeySecret := getCredentialFromContext(c)

	client, err := newRoute53Client(accessKeyID, accessKeySecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	zoneID, err := resolveHostedZoneID(client, domainName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	oldName, oldType, ok := parseRecordID(req.RecordID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的记录ID"})
		return
	}

	newRR := req.RR
	newType := req.Type
	newValue := req.Value
	newTTL := req.TTL
	newPriority := req.Priority

	fqdn := buildFQDN(newRR, domainName)
	ttl := newTTL
	if ttl == 0 {
		ttl = 300
	}
	val := newValue
	if newType == "MX" && newPriority > 0 {
		val = fmt.Sprintf("%d %s", newPriority, newValue)
	}

	changes := []r53types.Change{
		{
			Action:            r53types.ChangeActionUpsert,
			ResourceRecordSet: buildRRSet(fqdn, newType, ttl, val),
		},
	}

	// 若主机记录或类型发生变化，需先删除旧记录（按原始 Name+Type 精确匹配）
	if newRR != rrFromName(oldName, domainName) || newType != oldType {
		oldSet, ferr := findRecordSet(client, zoneID, oldName, oldType)
		if ferr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "未找到待修改的原始记录: " + ferr.Error()})
			return
		}
		changes = append([]r53types.Change{
			{Action: r53types.ChangeActionDelete, ResourceRecordSet: oldSet},
		}, changes...)
	}

	if _, err := client.ChangeResourceRecordSets(context.TODO(), &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改DNS记录失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "修改成功",
		"data": gin.H{"record_id": makeRecordID(fqdn, newType)},
	})
}
