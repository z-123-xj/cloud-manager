package aliyun

// 查询ECS实例支持的所有地域
// 官方api地址：https://api.aliyun.com/api/Ecs/2014-05-26/DescribeInstances?lang=GO&useCommon=true&params=%7B%22RegionId%22%3A%22ap-southeast-1%22%7D&tab=VALIDATION&RegionId=cn-hongkong

import (
	"fmt"

	"cloud-manager-backend/internal/models"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

// AliyunEcsItem 阿里云 ECS API 返回的实例原始字段
type AliyunEcsItem struct {
	InstanceId       string `json:"InstanceId"`       // 实例唯一ID
	InstanceName     string `json:"InstanceName"`     // 实例名称
	Description      string `json:"Description"`      // 实例描述
	Status           string `json:"Status"`           // 实例状态（Running/Stopped/Starting/Stopping）
	Cpu              int64  `json:"Cpu"`              // CPU核心数
	Memory           int64  `json:"Memory"`           // 内存大小（MB，API原始值）
	InstanceType     string `json:"InstanceType"`     // 实例规格（如 ecs.g7.xlarge）
	OSName           string `json:"OSName"`           // 操作系统名称（中文）
	PublicIpAddress  string `json:"PublicIpAddress"`  // 公网IP
	EipAddress       string `json:"EipAddress"`       // 弹性公网IP
	PrivateIpAddress string `json:"PrivateIpAddress"` // 私网IP
	RegionId         string `json:"RegionId"`         // 地域ID
	RegionName       string `json:"-"`                // 地域中文名（由调用方填入，不来自API）
	ZoneId           string `json:"ZoneId"`           // 可用区ID
	ZoneName         string `json:"-"`                // 可用区中文名（由调用方填入，不来自API）
	CreationTime     string `json:"CreationTime"`     // 创建时间（UTC）
}

func createApiInfo() (_result *openapi.Params) {
	params := &openapi.Params{
		// 接口名称
		Action: tea.String("DescribeInstances"),
		// 接口版本
		Version: tea.String("2014-05-26"),
		// 接口协议
		Protocol: tea.String("HTTPS"),
		// 接口 HTTP 方法
		Method:   tea.String("POST"),
		AuthType: tea.String("AK"),
		Style:    tea.String("RPC"),
		// 接口 PATH
		Pathname: tea.String("/"),
		// 接口请求体内容格式
		ReqBodyType: tea.String("json"),
		// 接口响应体内容格式
		BodyType: tea.String("json"),
	}
	_result = params
	return _result
}

// DescribeInstances 查询单个地域的单页ECS实例
// 返回: 实例列表 和 总记录数
func DescribeInstances(accessKeyID, accessKeySecret, endpoint, localname string, regionId string, pageNumber, pageSize int) ([]AliyunEcsItem, int64) {
	params := createApiInfo()

	queries := map[string]interface{}{}
	queries["RegionId"] = tea.String(regionId)
	queries["PageNumber"] = tea.Int(pageNumber)
	queries["PageSize"] = tea.Int(pageSize)
	request := &openapi.OpenApiRequest{
		Query: openapiutil.Query(queries),
	}

	jsondata, err := RequestCommonAPI(accessKeyID, accessKeySecret, endpoint, params, request)
	if err != nil {
		fmt.Printf("  [重试] %s (%s) 第1次异常: %v\n", localname, regionId, err)
		jsondata, err = RequestCommonAPI(accessKeyID, accessKeySecret, endpoint, params, request)
		if err != nil {
			fmt.Printf("  [跳过] %s (%s) 该地域暂不支持ECS查询: %v\n", localname, regionId, err)
			return nil, 0
		}
	}

	ecslistbody := jsondata.Get("body.Instances.Instance").Array()
	totalCount := jsondata.Get("body.TotalCount").Int()

	if totalCount == 0 || len(ecslistbody) == 0 {
		return nil, totalCount
	}

	var ecslist []AliyunEcsItem
	for _, ecs := range ecslistbody {
		// 公网IP: PublicIpAddress.IpAddress 是数组，取第一个
		publicIp := ""
		pubIps := ecs.Get("PublicIpAddress.IpAddress").Array()
		if len(pubIps) > 0 {
			publicIp = pubIps[0].String()
		}

		// 私网IP: 优先从 NetworkInterfaces[0].PrimaryIpAddress，其次 VpcAttributes.PrivateIpAddress.IpAddress[0]
		privateIp := ecs.Get("NetworkInterfaces.NetworkInterface.0.PrimaryIpAddress").String()
		if privateIp == "" {
			priIps := ecs.Get("VpcAttributes.PrivateIpAddress.IpAddress").Array()
			if len(priIps) > 0 {
				privateIp = priIps[0].String()
			}
		}

		ecslist = append(ecslist, AliyunEcsItem{
			InstanceId:       ecs.Get("InstanceId").String(),
			InstanceName:     ecs.Get("InstanceName").String(),
			Description:      ecs.Get("Description").String(),
			Status:           ecs.Get("Status").String(),
			Cpu:              ecs.Get("Cpu").Int(),
			Memory:           ecs.Get("Memory").Int() / 1024, // API返回MB，转换为GB
			InstanceType:     ecs.Get("InstanceType").String(),
			OSName:           ecs.Get("OSName").String(),
			PublicIpAddress:  publicIp,
			EipAddress:       ecs.Get("EipAddress.IpAddress").String(),
			PrivateIpAddress: privateIp,
			RegionId:         ecs.Get("RegionId").String(),
			ZoneId:           ecs.Get("ZoneId").String(),
			CreationTime:     ecs.Get("CreationTime").String(),
		})
	}

	return ecslist, totalCount
}

// describeInstancesInRegion 查询单个地域的所有ECS实例（分页），并写入数据库
func describeInstancesInRegion(accessKeyID, accessKeySecret, regionId, endpoint, localname, accountID, accountName, platform, site string, pageSize int) {
	// 先查第1页获取总记录数
	ecslist, totalCount := DescribeInstances(accessKeyID, accessKeySecret, endpoint, localname, regionId, 1, pageSize)
	fmt.Printf("  地域: %s, 总实例数: %d, 第1页: %d 条\n", localname, totalCount, len(ecslist))

	// 回填地域和可用区中文名，并转为统一结构体
	unified := convertAliyunEcsToUnified(ecslist, localname, regionId)

	if len(unified) > 0 {
		models.SyncEcsToDB(unified, accountID, accountName, platform, site)
	}

	if totalCount <= int64(pageSize) {
		return
	}

	// 计算总页数，继续查询剩余页
	totalPages := int((totalCount + int64(pageSize) - 1) / int64(pageSize))
	for i := 2; i <= totalPages; i++ {
		ecslist, _ = DescribeInstances(accessKeyID, accessKeySecret, endpoint, localname, regionId, i, pageSize)
		fmt.Printf("  地域: %s, 第%d/%d页: %d 条\n", localname, i, totalPages, len(ecslist))
		unified = convertAliyunEcsToUnified(ecslist, localname, regionId)
		if len(unified) > 0 {
			models.SyncEcsToDB(unified, accountID, accountName, platform, site)
		}
	}
}

// convertAliyunEcsToUnified 将阿里云 ECS 列表转为统一结构体
func convertAliyunEcsToUnified(ecslist []AliyunEcsItem, localname, regionId string) []models.UnifiedEcsItem {
	var result []models.UnifiedEcsItem
	for _, item := range ecslist {
		result = append(result, models.UnifiedEcsItem{
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
			RegionName:       localname,
			ZoneName:         buildZoneName(localname, item.ZoneId, regionId),
			CreationTime:     item.CreationTime,
		})
	}
	return result
}

// buildZoneName 根据 zoneId 构建可用区中文名（不包含地域前缀）
// 例如: zoneId="ap-southeast-1a", regionId="ap-southeast-1" → "可用区A"
func buildZoneName(localname, zoneId, regionId string) string {
	if zoneId == "" || regionId == "" {
		return zoneId
	}
	// zoneId 格式为 regionId + 后缀字母，如 ap-southeast-1a → 后缀 a
	suffix := zoneId
	if len(zoneId) > len(regionId) {
		suffix = zoneId[len(regionId):]
	}
	return "可用区" + suffix
}

// DescribeInstancesAllCN 国内站 ECS 实例同步
func DescribeInstancesAllCN(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	describeInstancesAll(accessKeyID, accessKeySecret, accountID, accountName, platform, "cn")
}

// DescribeInstancesAllIntl 国际站 ECS 实例同步
func DescribeInstancesAllIntl(accessKeyID, accessKeySecret, accountID, accountName, platform string) {
	describeInstancesAll(accessKeyID, accessKeySecret, accountID, accountName, platform, "intl")
}

// describeInstancesAll 根据全局启用地域配置查询ECS实例
func describeInstancesAll(accessKeyID, accessKeySecret, accountID, accountName, platform, site string) {
	pageSize := 50 // 每页取50条

	// 从数据库获取全局已启用的地域列表
	dbRegions := models.GetEnabledRegions(platform)

	if len(dbRegions) == 0 {
		fmt.Printf("警告：账号 %s 没有可用的地域，请检查地域配置\n", accountName)
		return
	}

	fmt.Printf("账号 %s 共有 %d 个启用的地域\n", accountName, len(dbRegions))

	for _, region := range dbRegions {
		endpoint := region.RegionEndpoint
		localname := region.RegionName

		describeInstancesInRegion(accessKeyID, accessKeySecret, region.RegionID, endpoint, localname, accountID, accountName, platform, site, pageSize)
	}
}
