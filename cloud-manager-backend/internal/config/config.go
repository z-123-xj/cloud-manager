// internal/config/cloud.go
package config

import (
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
	// "github.com/tidwall/gjson"
	// "golang.org/x/text/unicode/rangetable"
	// "path/filepath"
	// "gopkg.in/yaml.v3"
)

// ========== 配置结构体定义 ==========

// 阿里云国内账号
type AliyunAccount struct {
	Name         string `yaml:"name"`
	SecretID     string `yaml:"secret_id"`
	SecretKey    string `yaml:"secret_key"`
}

type MysqlConfig struct {
	Host string `yaml:"host"`
	Port int `yaml:"port"`
	User string `yaml:"user"`
	Password string `yaml:"password"`
	DBname string `yaml:"dbname"`
}


func GetmysqlConfig() MysqlConfig {
	var config MysqlConfig
	if err := viper.UnmarshalKey("database.mysql", &config); err != nil {
		log.Printf("读取 MySQL 配置失败: %v", err)
	}
	return config
}

// BastionConfig 堡垒机配置
type BastionConfig struct {
	Enabled       bool
	MaxSessions   int
	SessionTimeout int // SSH 会话无操作自动断开时长（分钟），仅统计真实终端 I/O（输出/输入），不含心跳
	DefaultSSHPort int // 资产未设置/SSH端口非法时的全局默认SSH端口（老数据与漏设资产兜底）
}

func GetBastionConfig() BastionConfig {
	viper.SetDefault("bastion.enabled", true)
	viper.SetDefault("bastion.max_sessions", 200)
	viper.SetDefault("bastion.session_timeout", 30)
	viper.SetDefault("bastion.default_ssh_port", 22)
	return BastionConfig{
		Enabled:       viper.GetBool("bastion.enabled"),
		MaxSessions:   viper.GetInt("bastion.max_sessions"),
		SessionTimeout: viper.GetInt("bastion.session_timeout"),
		DefaultSSHPort: viper.GetInt("bastion.default_ssh_port"),
	}
}

// BatchConfig 批量命令/脚本执行的资源上限
type BatchConfig struct {
	MaxConcurrency int // 单任务最大并发台数
	MaxTimeoutSec  int // 单台执行超时上限（秒）
	MaxScriptBytes int // 脚本内容最大字节数
	MaxOutputBytes int // 单台 stdout/stderr 各自留存的最大字节数（须小于 TEXT 列上限）
	MaxAssets      int // 单任务最多目标主机数
}

func GetBatchConfig() BatchConfig {
	viper.SetDefault("bastion.batch_max_concurrency", 20)
	viper.SetDefault("bastion.batch_max_timeout_sec", 3600)
	viper.SetDefault("bastion.batch_max_script_bytes", 32768)
	viper.SetDefault("bastion.batch_max_output_bytes", 16384)
	viper.SetDefault("bastion.batch_max_assets", 200)
	return BatchConfig{
		MaxConcurrency: viper.GetInt("bastion.batch_max_concurrency"),
		MaxTimeoutSec:  viper.GetInt("bastion.batch_max_timeout_sec"),
		MaxScriptBytes: viper.GetInt("bastion.batch_max_script_bytes"),
		MaxOutputBytes: viper.GetInt("bastion.batch_max_output_bytes"),
		MaxAssets:      viper.GetInt("bastion.batch_max_assets"),
	}
}

// FrontendConfig 前端静态文件目录（后端自带的前端托管能力使用）
type FrontendConfig struct {
	Dir string
}

// GetFrontendConfig 返回前端目录。
// 优先级：环境变量 FRONTEND_DIR > config.yaml 的 frontend.dir > ../cloud-manager-front。
// 相对路径以进程工作目录（systemd 的 WorkingDirectory）为基准。
// 生产上若已由 Nginx 直接托管静态文件，此项仅在直连 :8080 或回滚时生效。
func GetFrontendConfig() FrontendConfig {
	viper.SetDefault("frontend.dir", "../cloud-manager-front")
	dir := strings.TrimSpace(viper.GetString("frontend.dir"))
	if v := strings.TrimSpace(os.Getenv("FRONTEND_DIR")); v != "" {
		dir = v
	}
	if dir == "" {
		dir = "../cloud-manager-front"
	}
	return FrontendConfig{Dir: dir}
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string // 日志级别: debug, info, warn, error
	Path       string // 日志目录（按天轮转写文件）；为空则仅输出到控制台
	MaxAgeDays int    // 日志保留天数（按天轮转后自动清理过期文件），0=永久保留
}

func GetLogConfig() LogConfig {
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.path", "")
	viper.SetDefault("log.max_age_days", 7)
	return LogConfig{
		Level:      viper.GetString("log.level"),
		Path:       viper.GetString("log.path"),
		MaxAgeDays: viper.GetInt("log.max_age_days"),
	}
}

// ServerConfig 服务器资源限制配置
type ServerConfig struct {
	Port          int
	MemoryLimitMB int
	GCPercent     int
	ReadTimeout   int
	WriteTimeout  int
	IdleTimeout   int
}

func GetServerConfig() ServerConfig {
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.memory_limit_mb", 512)
	viper.SetDefault("server.gc_percent", 50)
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 60)
	viper.SetDefault("server.idle_timeout", 120)
	return ServerConfig{
		Port:          viper.GetInt("server.port"),
		MemoryLimitMB: viper.GetInt("server.memory_limit_mb"),
		GCPercent:     viper.GetInt("server.gc_percent"),
		ReadTimeout:   viper.GetInt("server.read_timeout"),
		WriteTimeout:  viper.GetInt("server.write_timeout"),
		IdleTimeout:   viper.GetInt("server.idle_timeout"),
	}
}


