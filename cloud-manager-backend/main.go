// main.go
package main

import (
	"cloud-manager-backend/internal/config"
	"cloud-manager-backend/internal/models"
	"cloud-manager-backend/internal/pkg/cloud"
	"cloud-manager-backend/internal/pkg/cloud/bastion/ssh"
	"cloud-manager-backend/internal/router"
	"cloud-manager-backend/pkg/logger"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/spf13/viper"
)

// loadConfig 读取配置文件，并完成日志与资源限制初始化。
// 配置来源优先级：
//  1. --config 指定的配置文件路径（绝对或相对路径，支持 yaml/yml/json/toml 等 viper 支持的格式）
//  2. 未指定时：在当前工作目录下查找 config.yaml（默认行为，保持兼容）
//
// 注意：本函数必须在 flag.Parse() 之后调用（--config 才能生效），
// 且必须先于所有使用 logger / 配置的代码执行。
func loadConfig(configPath string) {
	if configPath != "" {
		// 方式一：--config 显式指定配置文件（直接使用该文件，不再搜索目录）
		viper.SetConfigFile(configPath)
	} else {
		// 方式二：默认在当前工作目录查找 config.yaml
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	// 初始化日志（必须先于所有使用 logger 的代码）
	logCfg := config.GetLogConfig()
	logger.Init(logCfg.Level, logCfg.Path, logCfg.MaxAgeDays)
	logger.Info("server", "日志系统初始化完成", "level", logCfg.Level, "log_dir", logCfg.Path, "config_file", viper.ConfigFileUsed())

	// 应用运行时资源限制（仅启动时生效）
	applyResourceLimits()
}

// applyResourceLimits 读取 server 配置并应用 Go 运行时限制
func applyResourceLimits() {
	sc := config.GetServerConfig()
	debug.SetMemoryLimit(int64(sc.MemoryLimitMB) * 1024 * 1024)
	debug.SetGCPercent(sc.GCPercent)
	logger.Info("server", "资源限制", "memory_limit_mb", sc.MemoryLimitMB, "gc_percent", sc.GCPercent)
}

// scheduleDailySync 每天凌晨 0 点自动执行域名+ECS同步
// 包含 panic recover 保护，确保定时任务不会因单次异常而终止
func scheduleDailySync() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("sync", "定时同步PANIC恢复", "panic", r, "stack", string(debug.Stack()))
			// 重启定时循环
			go scheduleDailySync()
		}
	}()

	for {
		now := time.Now()
		// 计算下一个凌晨 0 点
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		duration := next.Sub(now)

		logger.Info("sync", "下次同步时间", "next", next.Format("2006-01-02 15:04:05"), "wait", duration.Round(time.Second).String())

		time.Sleep(duration)

		taskID := fmt.Sprintf("cron-all-%d", time.Now().UnixNano())
		logger.Info("sync", "开始执行每日自动同步", "task_id", taskID)

		// 使用独立的 recover 保护单次同步
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("sync", "单次同步异常", "panic", r, "stack", string(debug.Stack()))
				}
			}()
			cloud.SyncAllResources("all", taskID, nil)
		}()

		logger.Info("sync", "每日自动同步完成", "task_id", taskID)
	}
}

func main() {
	// 启动参数：--config 指定配置文件路径（默认读取当前工作目录下的 config.yaml）
	// 启动参数：--init 执行一次性初始化（空库导入表结构 + 创建 admin），完成后退出
	// 启动参数：位置子命令 init 触发一次性初始化（空库导入表结构 + 创建 admin），完成后退出
	// 用法：./devops init [--init-sql <路径>] [--init-admin <用户名>] [--init-password <密码>]
	configPath := flag.String("config", "", "配置文件路径（默认读取当前目录下的 config.yaml）")
	initSQL := flag.String("init-sql", "scripts/init_db.sql", "初始化 SQL 文件路径（init 子命令使用）")
	initAdmin := flag.String("init-admin", "admin", "初始化管理员用户名（init 子命令使用）")
	initPass := flag.String("init-password", "Admin@123456", "初始化管理员密码（init 子命令使用）")
	resetUser := flag.String("username", "", "重置密码的用户名（resetpw 子命令使用）")
	resetPass := flag.String("new-password", "", "新密码（resetpw 子命令使用）")
	flag.Parse()

	// 加载配置（--config 优先，未指定时用当前目录 config.yaml），并初始化日志与资源限制
	// 必须在 flag.Parse() 之后执行，否则 --config 不生效
	loadConfig(*configPath)

	// 位置参数第一个为子命令；init = 初始化数据库后退出，不启动 HTTP 服务
	args := flag.Args()
	initFlag := len(args) > 0 && args[0] == "init"

	// 1. 获取数据库配置
	dbConfig := config.GetmysqlConfig()

	// 2. 初始化数据库连接
	if err := models.InitDB(&dbConfig); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	// 2.1 若指定 init 子命令：执行初始化（幂等）后退出，不启动 HTTP 服务
	if initFlag {
		if err := models.InitSystem(*initSQL, *initAdmin, *initPass); err != nil {
			log.Fatalf("初始化失败: %v", err)
		}
		fmt.Printf("✅ 数据库初始化完成（表结构已导入，管理员账号 %s 已确保存在）。\n", *initAdmin)
		fmt.Println("   默认管理员密码：", *initPass, "（请尽快登录修改）")
		os.Exit(0)
	}

	// 2.2 若指定 resetpw 子命令：应急重置用户密码并解锁（用当前 AES 密钥加密），完成后退出
	if len(args) > 0 && args[0] == "resetpw" {
		if *resetUser == "" || *resetPass == "" {
			log.Fatalf("resetpw 子命令需要 --username 和 --new-password 参数")
		}
		if err := models.ResetUserPassword(*resetUser, *resetPass); err != nil {
			log.Fatalf("重置密码失败: %v", err)
		}
		fmt.Printf("✅ 用户 %s 密码已重置并解锁，请用新密码登录。\n", *resetUser)
		os.Exit(0)
	}

	// 3. 启动每天凌晨 0 点自动同步定时任务
	go scheduleDailySync()

	// 3.1 注入堡垒机 SSH 会话超时配置（分钟）
	ssh.SetSessionTimeout(config.GetBastionConfig().SessionTimeout)

	r := router.SetupRouter()

	// HTTP 服务器资源限制
	sc := config.GetServerConfig()
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", sc.Port),
		Handler:           r,
		ReadTimeout:       time.Duration(sc.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(sc.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(sc.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20, // 请求头最大 1MB
	}

	// 优雅关闭
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		logger.Info("server", "正在优雅关闭...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("[服务] 强制关闭: %v", err)
		}
		logger.Info("server", "已安全退出")
	}()

	logger.Info("server", "服务启动", "port", sc.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[服务] 启动失败: %v", err)
	}

}
