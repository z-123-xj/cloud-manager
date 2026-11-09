// Package logger 提供结构化分级日志，支持 DEBUG/INFO/WARN/ERROR 四级
// 用法：
//
//	logger.Info("bastion", "SSH连接成功", "host", host, "port", port)
//	logger.Error("bastion", "SSH连接失败", "err", err, "host", host)
//	logger.Debug("sync", "开始同步资源", "account", accountID)
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

var levelNames = map[string]Level{
	"DEBUG": DEBUG,
	"INFO":  INFO,
	"WARN":  WARN,
	"ERROR": ERROR,
}

// Logger 结构化日志器
type Logger struct {
	mu       sync.Mutex
	out      io.Writer
	level    Level
	fileOut  io.WriteCloser // 可选的文件输出
	noColor  bool
}

var defaultLogger *Logger

// Init 初始化日志器，应在读取配置后调用。
// level 取值: "debug", "info", "warn", "error"
// logDir 为空时仅输出到 stdout；非空时按天轮转写入该目录下的文件（同时仍输出到 stdout）
// maxAge 为日志保留天数（<=0 表示永久保留）
func Init(level, logDir string, maxAge int) {
	lvl := INFO
	if parsed, ok := levelNames[strings.ToUpper(level)]; ok {
		lvl = parsed
	}
	defaultLogger = &Logger{
		out:     os.Stdout,
		level:   lvl,
		noColor: os.Getenv("NO_COLOR") == "1" || os.Getenv("TERM") == "dumb",
	}
	if logDir != "" {
		if err := SetLogFile(logDir, maxAge); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] 无法初始化日志目录 %s: %v\n", logDir, err)
			fmt.Fprintf(os.Stderr, "[logger] 日志将仅输出到控制台，不会写入文件\n")
		} else {
			absPath, _ := filepath.Abs(logDir)
			today := time.Now().Format("2006-01-02")
			defaultLogger.log(INFO, "logger", "日志文件已启用",
				"dir", absPath,
				"file", logFilePrefix+today+logFileExt,
				"level", level,
				"maxAge", maxAge)
		}
	}
}

// SetOutput 设置输出目标（默认 stdout）
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.out = w
}

// SetLevel 设置最低输出级别
func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// SetLogFile 启用按天轮转的文件日志（复用 grafana-webhook rotatingWriter 思路）：
// 日志按天写入 logDir 下的 cloud-manager-YYYY-MM-DD.log，并自动清理超过 maxAge 天的旧文件。
// stdout 仍由 Logger 单独输出，这里只负责文件落盘与轮转。
func SetLogFile(dir string, maxAge int) error {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	if defaultLogger.fileOut != nil {
		defaultLogger.fileOut.Close()
	}
	rw := &rotatingWriter{dir: dir, maxAge: maxAge}
	if err := rw.ensureFile(); err != nil {
		return err
	}
	defaultLogger.fileOut = rw

	// 启动时先清理一次过期日志
	cleanOldLogs(dir, maxAge)
	// 后台每日零点附近的清理任务
	go func() {
		for {
			now := time.Now()
			next := now.Add(24 * time.Hour).Truncate(24 * time.Hour)
			time.Sleep(time.Until(next))
			cleanOldLogs(dir, maxAge)
		}
	}()
	return nil
}

// ==================== 按天轮转写入器（复用 grafana-webhook rotatingWriter 思路） ====================

const logFilePrefix = "cloud-manager-"
const logFileExt = ".log"

// rotatingWriter 实现按天轮转的日志文件写入器：
//   - 每天自动生成新的日志文件 cloud-manager-YYYY-MM-DD.log
//   - 跨天自动切换到新文件，旧文件由 cleanOldLogs 按 maxAge 清理
//   - 仅写文件（stdout 由 Logger 单独处理），写文件失败时由调用方忽略错误，不影响 stdout 输出
type rotatingWriter struct {
	dir    string
	maxAge int
	mu     sync.Mutex
	date   string
	file   *os.File
}

// ensureFile 确保当前日期对应的日志文件已打开，跨天则切换
func (w *rotatingWriter) ensureFile() error {
	today := time.Now().Format("2006-01-02")
	if w.file != nil && w.date == today {
		return nil
	}
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(w.dir, logFilePrefix+today+logFileExt)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = f
	w.date = today
	return nil
}

// Write 实现 io.Writer：写入当前日期的日志文件（不写 stdout）
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureFile(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

// Close 关闭当前日志文件
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// cleanOldLogs 删除保留天数之外的历史日志文件（按文件名中的日期判断）
func cleanOldLogs(dir string, maxAge int) {
	if maxAge <= 0 {
		return // <=0 表示永久保留
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, logFilePrefix) || !strings.HasSuffix(name, logFileExt) {
			continue
		}
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileExt)
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// ==================== 公开 API ====================

// Log 通用日志方法，允许直接传 Level
func Log(level Level, module, msg string, kvs ...interface{}) {
	defaultLogger.log(level, module, msg, kvs...)
}

// Debug 调试日志（生产环境通常关闭）
func Debug(module, msg string, kvs ...interface{}) {
	defaultLogger.log(DEBUG, module, msg, kvs...)
}

// Info 常规信息日志
func Info(module, msg string, kvs ...interface{}) {
	defaultLogger.log(INFO, module, msg, kvs...)
}

// Warn 警告日志
func Warn(module, msg string, kvs ...interface{}) {
	defaultLogger.log(WARN, module, msg, kvs...)
}

// Error 错误日志
func Error(module, msg string, kvs ...interface{}) {
	defaultLogger.log(ERROR, module, msg, kvs...)
}

// ==================== 内部实现 ====================

func (l *Logger) log(level Level, module, msg string, kvs ...interface{}) {
	if level < l.level {
		return
	}

	// 构建结构化字段
	fields := make(map[string]interface{})
	fields["module"] = module
	fields["msg"] = msg

	// 奇数个 kvs 处理：最后一个单独的值标记为 "value"
	for i := 0; i < len(kvs); i += 2 {
		k := fmt.Sprintf("%v", kvs[i])
		if i+1 < len(kvs) {
			fields[k] = formatVal(kvs[i+1])
		} else {
			fields[k] = "<missing>"
		}
	}

	// 添加调用位置
	if level >= WARN {
		if _, file, line, ok := runtime.Caller(2); ok {
			// 截短路径
			if idx := strings.LastIndex(file, "/"); idx >= 0 {
				file = file[idx+1:]
			}
			fields["caller"] = fmt.Sprintf("%s:%d", file, line)
		}
	}

	now := time.Now()
	timeStr := now.Format("2006-01-02 15:04:05.000")

	// JSON 行输出
	jsonBytes, _ := json.Marshal(fields)

	// 控制台带颜色
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.noColor {
		fmt.Fprintf(l.out, "%s [%s] %s\n", timeStr, level.String(), string(jsonBytes))
	} else {
		color := colorForLevel(level)
		fmt.Fprintf(l.out, "%s %s%s%s %s\n", timeStr, color, level.String(), reset, string(jsonBytes))
	}

	// 同时写文件（无颜色）
	if l.fileOut != nil {
		fmt.Fprintf(l.fileOut, "%s [%s] %s\n", timeStr, level.String(), string(jsonBytes))
	}
}

// ==================== 颜色 ====================

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	gray   = "\033[90m"
	cyan   = "\033[36m"
)

func colorForLevel(l Level) string {
	switch l {
	case DEBUG:
		return gray
	case INFO:
		return cyan
	case WARN:
		return yellow
	case ERROR:
		return red
	default:
		return reset
	}
}

// formatVal 将值转为 JSON 可序列化的形式，对 error 类型提取 Error() 字符串
func formatVal(v interface{}) interface{} {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return v
}

// ==================== 便捷适配 log 标准库 ====================

// StdLogger 返回一个兼容标准 log.Logger 的 writer（用于 gin 等框架）
// 使用方式: gin.DefaultWriter = logger.StdLogger()
func StdWriter() io.Writer {
	return &stdWriterAdapter{}
}

type stdWriterAdapter struct{}

func (w *stdWriterAdapter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	Info("http", msg)
	return len(p), nil
}
