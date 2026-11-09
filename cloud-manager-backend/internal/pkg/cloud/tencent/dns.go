package tencent

// dns.go 是腾讯云DNS解析记录操作的入口文件
// 具体的增删改查等操作在 dns/ 子包中：
//   - dns/describe_records.go  查询DNS解析记录
//   - dns/add_record.go        添加DNS解析记录
//   - dns/update_record.go     修改DNS解析记录
//   - dns/set_status.go        设置DNS记录启用/禁用状态
//   - dns/delete_record.go     删除DNS解析记录
//   - dns/common.go            公共模块（常量、凭证、客户端创建、线路映射）

import (
	dns "cloud-manager-backend/internal/pkg/cloud/tencent/dns"
)

// ==================== 类型别名（保持外部引用兼容，与阿里云一致） ====================

// DnsRecordItem DNS解析记录条目
type DnsRecordItem = dns.DnsRecordItem

// AddDomainRecordRequest 添加DNS记录请求
type AddDomainRecordRequest = dns.AddRecordRequest

// UpdateDomainRecordRequest 修改DNS记录请求
type UpdateDomainRecordRequest = dns.UpdateRecordRequest

// SetDomainRecordStatusRequest 设置DNS记录状态请求
type SetDomainRecordStatusRequest = dns.SetStatusRequest

// DeleteDomainRecordRequest 删除DNS记录请求
type DeleteDomainRecordRequest = dns.DeleteRecordRequest

// ==================== Handler 委托 ====================

// DescribeDomainRecordsHandler 查询DNS记录 → 委托给 dns.DescribeRecordsHandler
var DescribeDomainRecordsHandler = dns.DescribeRecordsHandler

// AddDomainRecordHandler 添加DNS记录 → 委托给 dns.AddRecordHandler
var AddDomainRecordHandler = dns.AddRecordHandler

// UpdateDomainRecordHandler 修改DNS记录 → 委托给 dns.UpdateRecordHandler
var UpdateDomainRecordHandler = dns.UpdateRecordHandler

// DeleteDomainRecordHandler 删除DNS记录 → 委托给 dns.DeleteRecordHandler
var DeleteDomainRecordHandler = dns.DeleteRecordHandler

// SetDomainRecordStatusHandler 设置DNS记录状态 → 委托给 dns.SetStatusHandler
var SetDomainRecordStatusHandler = dns.SetStatusHandler
