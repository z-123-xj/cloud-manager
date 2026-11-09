package dns

// 查询DNS解析记录列表
// API: GET /2013-04-01/hostedzone/{zone_id}/rrset
// 文档: https://docs.aws.amazon.com/Route53/latest/APIReference/API_ListResourceRecordSets.html

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/gin-gonic/gin"
)

// DescribeRecordsHandler GET /api/domains/:domain/dns
// 查询指定域名（Route53 Hosted Zone）下的所有DNS解析记录（分页遍历）
func DescribeRecordsHandler(c *gin.Context) {
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

	var items []DnsRecordItem
	var startName *string
	var startType r53types.RRType
	for {
		out, err := client.ListResourceRecordSets(context.TODO(), &route53.ListResourceRecordSetsInput{
			HostedZoneId:    aws.String(zoneID),
			StartRecordName: startName,
			StartRecordType: startType,
			MaxItems:        aws.Int32(300),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询DNS记录失败: " + err.Error()})
			return
		}
		for i := range out.ResourceRecordSets {
			items = append(items, toDnsRecordItem(&out.ResourceRecordSets[i], domainName))
		}
		if !out.IsTruncated {
			break
		}
		startName = out.NextRecordName
		startType = out.NextRecordType
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"list":     items,
			"total":    len(items),
			"platform": "aws",
		},
	})
}
