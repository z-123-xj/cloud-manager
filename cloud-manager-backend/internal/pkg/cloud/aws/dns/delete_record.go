package dns

// 删除DNS解析记录
// 文档: https://docs.aws.amazon.com/Route53/latest/APIReference/API_ChangeResourceRecordSets.html

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/gin-gonic/gin"
)

// DeleteRecordRequest 删除DNS解析记录请求参数
type DeleteRecordRequest struct {
	RecordID string `json:"record_id" binding:"required"`
}

// DeleteRecordHandler DELETE /api/domains/:domain/dns
func DeleteRecordHandler(c *gin.Context) {
	var req DeleteRecordRequest
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

	name, recordType, ok := parseRecordID(req.RecordID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的记录ID"})
		return
	}
	// Route53 删除要求按完整 RRSet 精确匹配（Name+Type+TTL+Values）
	oldSet, ferr := findRecordSet(client, zoneID, name, recordType)
	if ferr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "未找到待删除的记录: " + ferr.Error()})
		return
	}

	if _, err := client.ChangeResourceRecordSets(context.TODO(), &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch: &r53types.ChangeBatch{
			Changes: []r53types.Change{
				{Action: r53types.ChangeActionDelete, ResourceRecordSet: oldSet},
			},
		},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除DNS记录失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}
