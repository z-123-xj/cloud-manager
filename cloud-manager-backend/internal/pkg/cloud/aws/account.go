package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// GetAccountID 通过 STS GetCallerIdentity 获取 AWS 12 位账号ID（account_id）
func GetAccountID(accessKey, secret string) (string, error) {
	creds := credentials.NewStaticCredentialsProvider(accessKey, secret, "")
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return "", fmt.Errorf("加载AWS配置失败: %w", err)
	}
	client := sts.NewFromConfig(cfg)
	out, err := client.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("查询AWS账号失败: %w", err)
	}
	if out.Account == nil {
		return "", fmt.Errorf("AWS 未返回账号ID")
	}
	return *out.Account, nil
}
