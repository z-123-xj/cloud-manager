package dns

// 添加DNS解析记录（UPSERT：若同名同类型已存在则替换）
// API: POST /2013-04-01/hostedzone/{zone_id}/rrset
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

// AddRecordRequest 添加DNS解析记录请求参数（字段与阿里云/腾讯云/Cloudflare 前端保持一致）
type AddRecordRequest struct {
	DomainName string `json:"domain_name" binding:"required"`
	RR         string `json:"rr" binding:"required"`
	Type       string `json:"type" binding:"required"`
	Value      string `json:"value" binding:"required"`
	TTL        int64  `json:"ttl"`
	Priority   int64  `json:"priority"`
	Line       string `json:"line"`
	Weight     int    `json:"weight"`
	Remark     string `json:"remark"`
	Proxied    bool   `json:"proxied"`
}

// AddRecordHandler POST /api/domains/:domain/dns
func AddRecordHandler(c *gin.Context) {
	var req AddRecordRequest
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

	fqdn := buildFQDN(req.RR, domainName)
	ttl := req.TTL
	if ttl == 0 {
		ttl = 300
	}
	value := req.Value
	if req.Type == "MX" && req.Priority > 0 {
		value = fmt.Sprintf("%d %s", req.Priority, req.Value)
	}

	if _, err := client.ChangeResourceRecordSets(context.TODO(), &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch: &r53types.ChangeBatch{
			Changes: []r53types.Change{
				{
					Action:            r53types.ChangeActionUpsert,
					ResourceRecordSet: buildRRSet(fqdn, req.Type, ttl, value),
				},
			},
		},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "添加DNS记录失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": gin.H{"record_id": makeRecordID(fqdn, req.Type)},
	})
}
