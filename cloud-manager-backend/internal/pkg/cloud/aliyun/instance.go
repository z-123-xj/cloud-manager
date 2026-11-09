package aliyun

import (
	"fmt"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"
)

// RenameInstance 调用阿里云 ECS ModifyInstanceAttribute 修改实例名称（InstanceName）
func RenameInstance(accessKey, secret, regionID, endpoint, instanceID, name string) error {
	if endpoint == "" {
		endpoint = "ecs.aliyuncs.com"
	}
	params := &openapi.Params{
		Action:      tea.String("ModifyInstanceAttribute"),
		Version:     tea.String("2014-05-26"),
		Protocol:    tea.String("HTTPS"),
		Method:      tea.String("POST"),
		AuthType:    tea.String("AK"),
		Style:       tea.String("RPC"),
		Pathname:    tea.String("/"),
		ReqBodyType: tea.String("json"),
		BodyType:    tea.String("json"),
	}
	queries := map[string]interface{}{
		"RegionId":     tea.String(regionID),
		"InstanceId":   tea.String(instanceID),
		"InstanceName": tea.String(name),
	}
	request := &openapi.OpenApiRequest{Query: openapiutil.Query(queries)}

	_, err := RequestCommonAPI(accessKey, secret, endpoint, params, request)
	if err != nil {
		return fmt.Errorf("阿里云修改实例名称失败: %w", err)
	}
	return nil
}

// UpdateInstanceDescription 调用阿里云 ECS ModifyInstanceAttribute 修改实例描述（Description）
func UpdateInstanceDescription(accessKey, secret, regionID, endpoint, instanceID, description string) error {
	if endpoint == "" {
		endpoint = "ecs.aliyuncs.com"
	}
	params := &openapi.Params{
		Action:      tea.String("ModifyInstanceAttribute"),
		Version:     tea.String("2014-05-26"),
		Protocol:    tea.String("HTTPS"),
		Method:      tea.String("POST"),
		AuthType:    tea.String("AK"),
		Style:       tea.String("RPC"),
		Pathname:    tea.String("/"),
		ReqBodyType: tea.String("json"),
		BodyType:    tea.String("json"),
	}
	queries := map[string]interface{}{
		"RegionId":    tea.String(regionID),
		"InstanceId":  tea.String(instanceID),
		"Description": tea.String(description),
	}
	request := &openapi.OpenApiRequest{Query: openapiutil.Query(queries)}

	_, err := RequestCommonAPI(accessKey, secret, endpoint, params, request)
	if err != nil {
		return fmt.Errorf("阿里云修改实例描述失败: %w", err)
	}
	return nil
}
