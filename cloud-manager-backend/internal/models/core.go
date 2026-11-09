package models

import (
	"cloud-manager-backend/internal/config"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// gormCallerRe 匹配日志里编译期嵌入的源码路径+行号（如 Windows 绝对路径 C:/个人项目/.../core.go:107），
// 仅保留“文件名:行号”，去掉本机目录，避免泄露本地路径并让日志更干净。
var gormCallerRe = regexp.MustCompile(`[^\s]*[/\\]([^/\\\s]+\.go:\d+)`)

// pathScrubWriter 包装 io.Writer，把 GORM SQL 日志中的本地绝对路径压缩成“文件名:行号”。
type pathScrubWriter struct{ w io.Writer }

func (p pathScrubWriter) Write(b []byte) (int, error) {
	cleaned := gormCallerRe.ReplaceAll(b, []byte("${1}"))
	if _, err := p.w.Write(cleaned); err != nil {
		return 0, err
	}
	return len(b), nil
}

var DB *gorm.DB

// InitDB 初始化数据库连接，由 main.go 显式调用
// 表结构变更请通过 scripts/init_db.sql 手动管理，不使用 AutoMigrate
func InitDB(cfg *config.MysqlConfig) error {
	// 使用 mysql.Config 构建 DSN：自动对含 @、:、% 等特殊字符的密码做转义，
	// 避免字符串拼接导致密码被截断（例如密码含 @ 会被误判为 DSN 分隔符，导致 Access denied）
	mysqlCfg := mysqlDriver.NewConfig()
	mysqlCfg.User = cfg.User
	mysqlCfg.Passwd = cfg.Password
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mysqlCfg.DBName = cfg.DBname
	mysqlCfg.Params = map[string]string{"charset": "utf8mb4"}
	mysqlCfg.ParseTime = true
	mysqlCfg.Loc = time.Local

	dsn := mysqlCfg.FormatDSN()

	// GORM 日志配置：
	//   同步器等“先查后插”流程中 First 查不到是正常路径，不应每条都刷 record not found 警告
	//   （曾把同步日志刷爆）。这里包装默认 logger，仅丢弃 ErrRecordNotFound，
	//   其余错误与慢SQL（>1s）告警保留。
	defaultGormLogger := logger.New(
		log.New(pathScrubWriter{os.Stdout}, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold: time.Second,
			LogLevel:      logger.Warn,
			Colorful:      true,
		},
	)
	gormLogger := &recordNotFoundFilter{Interface: defaultGormLogger}

	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormLogger})
	if err != nil {
		return fmt.Errorf("数据库连接失败: %w", err)
	}

	log.Println("数据库连接成功!")

	// 启动自检（数据兜底/补索引/字符集）放后台执行：其中的 ALTER TABLE 会锁表，
	// 若在前台跑会拖慢甚至卡住 HTTP 服务启动。各步骤均打印 [migrate] 日志，
	// 且全部幂等——只在“确有必要时”（如索引缺失、字符集不对）才真正执行写操作，
	// 正常库会秒过、几乎无感。
	go func() {
		log.Println("[migrate] 开始执行启动自检（CDN列补丁 / 域名去重补索引 / 表字符集转utf8mb4）...")
		// 幂等确保 cdn_domains 表的证书相关列存在（兼容旧库未执行 init_db.sql 迁移脚本的情况，
		// 否则同步时新增字段写入会因 Unknown column 失败，导致整张表被清空）。
		ensureCdnCertColumns()
		// 幂等清理 domains 历史重复数据并补建 (account_id, domain_name) 唯一索引
		// （早期建的表没有该索引，同步会插出重复行，导致前端同域名显示两条）
		EnsureDomainsUnique()
		// 幂等确保核心业务表字符集为 utf8mb4（早期建的表可能非 utf8mb4，
		// 导致中文在写入时被替换为 '?'，且已损坏数据不可逆、需重新同步覆盖）
		EnsureTablesUtf8mb4()
		log.Println("[migrate] 启动自检完成")
	}()

	return nil
}

// ensureCdnCertColumns 幂等地确认 cdn_domains 表的证书字段列已存在（按结构体 tag 创建）。
// 仅作为“旧库未跑迁移脚本”的兜底；新库由 scripts/init_db.sql 统一建表。
func ensureCdnCertColumns() {
	if DB == nil {
		return
	}
	// 表尚未创建（init 导入建表前）时跳过，交由 init 统一建表，避免此处 ALTER 报 “table doesn't exist”
	if !DB.Migrator().HasTable(&CdnDomain{}) {
		return
	}
	for _, field := range []string{"CertName", "CertType", "CertExpireTime"} {
		if !DB.Migrator().HasColumn(&CdnDomain{}, field) {
			if err := DB.Migrator().AddColumn(&CdnDomain{}, field); err != nil {
				log.Printf("[migrate] 确保 cdn_domains 列 %s 失败: %v", field, err)
			} else {
				log.Printf("[migrate] 已为 cdn_domains 补齐列 %s", field)
			}
		}
	}
}

// recordNotFoundFilter 包装 gorm logger，丢弃 ErrRecordNotFound 的日志输出
// （同步“先查后插”流程中查不到是正常路径，不应刷成警告噪音）
type recordNotFoundFilter struct {
	logger.Interface
}

func (f *recordNotFoundFilter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	f.Interface.Trace(ctx, begin, fc, err)
}
