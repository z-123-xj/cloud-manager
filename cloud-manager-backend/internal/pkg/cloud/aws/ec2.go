package aws

import (
	"context"
	"fmt"
	"log"

	"cloud-manager-backend/internal/models"
	// "cloud-manager-backend/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// createEc2Client 根据凭证和地域创建EC2客户端
func createEc2Client(accessKeyID, accessKeySecret, region string) (*ec2.Client, error) {
	creds := credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("无法加载AWS配置: %w", err)
	}
	return ec2.NewFromConfig(cfg), nil
}

// DescribeEC2InstancesByRegion 使用SDK查询单个地域的所有EC2实例（分页），并写入数据库
func DescribeEC2InstancesByRegion(accessKeyID, accessKeySecret, regionId, localname, accountID, accountName, platform, site string) {
	client, err := createEc2Client(accessKeyID, accessKeySecret, regionId)
	if err != nil {
		fmt.Printf("  [跳过] %s (%s) 创建EC2客户端异常: %v\n", localname, regionId, err)
		return
	}

	var nextToken *string
	page := 1

	for {
		input := &ec2.DescribeInstancesInput{
			MaxResults: aws.Int32(50),
			NextToken:  nextToken,
		}

		result, err := client.DescribeInstances(context.TODO(), input)
		if err != nil {
			// AuthFailure/UnauthorizedOperation 说明该地域未开通/无权限，直接跳过不重试
			if isAuthOrUnauthorizedErr(err) {
				fmt.Printf("  [跳过] %s (%s) 该地域未开通或暂无权限\n", localname, regionId)
				return
			}
			// 其他错误重试1次
			fmt.Printf("  [重试] %s (%s) 第1次异常: %v\n", localname, regionId, err)
			result, err = client.DescribeInstances(context.TODO(), input)
			if err != nil {
				fmt.Printf("  [跳过] %s (%s) 查询异常: %v\n", localname, regionId, err)
				return
			}
		}

		// 打印SDK结果，用于分析字段(不要删除，用的时候在打开)
		// jsonStr, _ := utils.PrintJsonData(result)
		// fmt.Println(jsonStr)

		// 查询实例类型详情，补充内存信息
		instanceTypeInfo := describeInstanceTypes(client, result)

		// 将SDK结果转为统一结构
		items := convertEC2Result(result, instanceTypeInfo, localname)

		fmt.Printf("  地域: %s, 第%d页: %d 条\n", localname, page, len(items))

		if len(items) > 0 {
			models.SyncEcsToDB(items, accountID, accountName, platform, site)
		}

		if result.NextToken == nil || *result.NextToken == "" {
			break
		}
		nextToken = result.NextToken
		page++
	}
}

// convertEC2Result 将EC2 SDK DescribeInstances结果转为统一ECS结构
func convertEC2Result(result *ec2.DescribeInstancesOutput, instanceTypeInfo map[string]ec2types.InstanceTypeInfo, regionName string) []models.UnifiedEcsItem {
	var items []models.UnifiedEcsItem

	for _, reservation := range result.Reservations {
		for _, ins := range reservation.Instances {
			// 实例名：从Tags中取Key=="Name"的Value
			instanceName := ""
			for _, tag := range ins.Tags {
				if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
					instanceName = *tag.Value
					break
				}
			}

			// 状态：首字母大写，前端只识别 Running/Stopped/Terminated
			status := ""
			if ins.State != nil {
				s := string(ins.State.Name) // "running" / "stopped" / "terminated"
				if len(s) > 0 {
					status = string(s[0]-32) + s[1:] // 首字母大写
				}
			}

			// CPU
			cpu := int64(0)
			if ins.CpuOptions != nil {
				coreCount := int64(aws.ToInt32(ins.CpuOptions.CoreCount))
				threadsPerCore := int64(aws.ToInt32(ins.CpuOptions.ThreadsPerCore))
				if threadsPerCore > 0 && coreCount > 0 {
					cpu = coreCount * threadsPerCore // vCPU = CoreCount * ThreadsPerCore
				} else if coreCount > 0 {
					cpu = coreCount
				}
			}

			// 内存：优先从 DescribeInstanceTypes 结果中获取
			memory := int64(0)
			itKey := string(ins.InstanceType)
			if info, ok := instanceTypeInfo[itKey]; ok && info.MemoryInfo != nil {
				memory = int64(aws.ToInt64(info.MemoryInfo.SizeInMiB)) / 1024 // MiB → GB
			}

			// 操作系统
			osName := "Linux"
			if ins.Platform != "" && ins.Platform == ec2types.PlatformValuesWindows {
				osName = "Windows"
			}

			// IP地址
			publicIp := aws.ToString(ins.PublicIpAddress)
			privateIp := aws.ToString(ins.PrivateIpAddress)
			// 如果顶层没有，尝试从网卡中获取
			if publicIp == "" && len(ins.NetworkInterfaces) > 0 {
				ni := ins.NetworkInterfaces[0]
				if ni.Association != nil {
					publicIp = aws.ToString(ni.Association.PublicIp)
				}
			}
			if privateIp == "" && len(ins.NetworkInterfaces) > 0 {
				privateIp = aws.ToString(ins.NetworkInterfaces[0].PrivateIpAddress)
			}

			// 可用区
			zoneName := ""
			if ins.Placement != nil {
				zoneName = aws.ToString(ins.Placement.AvailabilityZone)
			}

			// 创建时间
			creationTime := ""
			if ins.LaunchTime != nil {
				creationTime = ins.LaunchTime.Format("2006-01-02T15:04:05Z")
			}

			items = append(items, models.UnifiedEcsItem{
				InstanceId:       aws.ToString(ins.InstanceId),
				InstanceName:     instanceName,
				Description:      "",
				Status:           status,
				Cpu:              cpu,
				Memory:           memory,
				InstanceType:     itKey,
				OSName:           osName,
				PublicIpAddress:  publicIp,
				EipAddress:       "",
				PrivateIpAddress: privateIp,
				RegionName:       regionName,
				ZoneName:         zoneName,
				CreationTime:     creationTime,
			})
		}
	}

	return items
}

// isAuthOrUnauthorizedErr 判断是否为认证/权限错误（该地域未开通/无资源，不应重试）
func isAuthOrUnauthorizedErr(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsAny(errStr, "AuthFailure", "UnauthorizedOperation", "InvalidClientTokenId")
}

// containsAny 检查字符串是否包含任一子串
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// describeInstanceTypes 收集 DescribeInstances 结果中出现的所有实例类型，查询其详细信息
func describeInstanceTypes(client *ec2.Client, result *ec2.DescribeInstancesOutput) map[string]ec2types.InstanceTypeInfo {
	typeSet := make(map[string]bool)
	for _, reservation := range result.Reservations {
		for _, ins := range reservation.Instances {
			if ins.InstanceType != "" {
				typeSet[string(ins.InstanceType)] = true
			}
		}
	}
	if len(typeSet) == 0 {
		return nil
	}

	var typeList []ec2types.InstanceType
	for t := range typeSet {
		typeList = append(typeList, ec2types.InstanceType(t))
	}

	input := &ec2.DescribeInstanceTypesInput{
		InstanceTypes: typeList,
	}

	output, err := client.DescribeInstanceTypes(context.TODO(), input)
	if err != nil {
		fmt.Printf("    查询实例类型详情异常: %v\n", err)
		return nil
	}

	infoMap := make(map[string]ec2types.InstanceTypeInfo)
	for _, info := range output.InstanceTypes {
		if info.InstanceType != "" {
			infoMap[string(info.InstanceType)] = info
		}
	}
	return infoMap
}

// DescribeEC2InstancesAll 根据全局启用地域配置查询EC2实例
func DescribeEC2InstancesAll(accessKeyID, accessKeySecret, accountID, accountName, platform, site string) {
	dbRegions := models.GetEnabledRegions(platform)

	if len(dbRegions) == 0 {
		fmt.Printf("警告：账号 %s 没有可用的地域，请检查地域配置\n", accountName)
		return
	}

	fmt.Printf("账号 %s 共有 %d 个启用的地域\n", accountName, len(dbRegions))

	for _, region := range dbRegions {
		DescribeEC2InstancesByRegion(accessKeyID, accessKeySecret, region.RegionID, region.RegionName, accountID, accountName, platform, site)
	}
}

// 测试用函数，保留但去除硬编码凭证
func testDescribeInstances() {
	accessKey := "YOUR_ACCESS_KEY"
	secretKey := "YOUR_SECRET_KEY"
	region := "us-east-1"

	client, err := createEc2Client(accessKey, secretKey, region)
	if err != nil {
		log.Fatalf("无法创建EC2客户端: %v", err)
	}

	input := &ec2.DescribeInstancesInput{
		MaxResults: aws.Int32(10),
	}

	result, err := client.DescribeInstances(context.TODO(), input)
	if err != nil {
		log.Fatalf("无法获取 EC2 实例信息: %v", err)
	}

	for _, reservation := range result.Reservations {
		for _, ins := range reservation.Instances {
			fmt.Printf("  InstanceId=%s, Type=%s, State=%s, Zone=%s\n",
				aws.ToString(ins.InstanceId),
				string(ins.InstanceType),
				ins.State.Name,
				aws.ToString(ins.Placement.AvailabilityZone),
			)
		}
	}
}
