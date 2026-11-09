package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// RenameInstance 修改 EC2 实例名称（EC2 的“名称”即 Key=Name 的标签，通过 CreateTags 更新）
func RenameInstance(accessKey, secret, region, instanceID, name string) error {
	client, err := createEc2Client(accessKey, secret, region)
	if err != nil {
		return err
	}
	_, err = client.CreateTags(context.TODO(), &ec2.CreateTagsInput{
		Resources: []string{instanceID},
		Tags:      []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(name)}},
	})
	if err != nil {
		return fmt.Errorf("AWS 修改实例名称失败: %w", err)
	}
	return nil
}
