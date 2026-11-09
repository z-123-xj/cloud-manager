// internal/pkg/cloud/interface.go
package cloud

// DNSRecord DNS 记录
type DNSRecord struct {
	RecordID string
	Domain   string
	RR       string
	Type     string
	Value    string
	TTL      int
}

// DomainInfo 域名信息
type DomainInfo struct {
	DomainName string
	ExpiryDate string
	Status     string
}

// DNSProvider DNS 操作接口
type DNSProvider interface {
	GetDNSRecords(domain string) ([]DNSRecord, error)
	AddDNSRecord(record DNSRecord) error
	DeleteDNSRecord(recordID string) error
}

// DomainProvider 域名操作接口
type DomainProvider interface {
	ScrollDomainList(pageSize int32) ([]DomainInfo, error)
}
