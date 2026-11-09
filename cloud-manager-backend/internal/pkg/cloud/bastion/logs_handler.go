package bastion

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud-manager-backend/scripts"

	"github.com/gin-gonic/gin"
	bssh "golang.org/x/crypto/ssh"
)

// ============================================================================
// 日志排查 v6：备份 + 分析闭环
//
//   - go:embed 把 scripts/ 下两份脚本编进二进制，调用前用 md5 比对目标服务器副本，
//     缺失或不一致时自动覆盖同步（同一 SSH 会话内完成，平时零额外开销）
//   - 首次「开始分析」会把服务器原有的切割脚本升级为平台版本、补装 cron、
//     落盘 log_format 模板，之后每天 0 点切割时在备份原始 .gz 的同时产出
//     分析页所需的 7 列 TSV.gz（PREPARED_DIR）
//   - 分析脚本优先读 TSV.gz；没跑过新切割脚本的历史备份、坏文件、log_format
//     变更时自动回退原始 .gz 完整解析，无需任何数据迁移
//   - 脚本参数（9 个）：域名 / 起始时间 / 结束时间 / 状态码 / URL / IP / 起始日期 /
//     结束日期 / log_format 模板（auto = 脚本自行探测）
//   - 脚本输出的 data 原样透传（global_stats + domain_stats），平台只做渲染
//   - 异步 Task：提交返回 task_id，前端轮询
// ============================================================================

const defaultAnalyzerPath = "/usr/local/sbin/nginx_analyzer.sh"
const defaultRotateScriptPath = "/usr/local/sbin/nginx_log_rotate.sh"
const defaultFormatTplPath = "/usr/local/sbin/nginx_log_format.tpl"
const currentLogDir = "/usr/local/nginx/logs"
const backupLogDir = "/data/backup/nginx_log"
const preparedLogDir = backupLogDir + "/analysis"

var safeDomainRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const defaultLogFormat = `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"`

// 远端探测脚本会把跨行的 log_format / access_log 指令拼成一行后再交给 Go 解析
var (
	logFormatDirectiveRe = regexp.MustCompile(`^log_format\s+(?:(?:escape=\S+)\s+)?(\S+)\s+(.*);\s*$`)
	quotedChunkRe        = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	accessLogDirectiveRe = regexp.MustCompile(`^access_log\s+(.*);\s*$`)
	requestVarRe         = regexp.MustCompile(`\$request\b`)
	statusVarRe          = regexp.MustCompile(`\$status\b`)
)

// safeLogPath 校验路径：绝对路径、不含空白与 shell 元字符
func safeLogPath(s string) bool {
	if s == "" || !strings.HasPrefix(s, "/") {
		return false
	}
	return !strings.ContainsAny(s, ";|&$`<>\\\"'\n\r \t")
}

// safeArg 校验脚本参数：这些值会被脚本二次拼进 awk 程序
func safeArg(s string) bool {
	return !strings.ContainsAny(s, "\"`\n\r;\\")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// formatProbeBody 是「路径探查」与「正式分析」共用的 nginx 配置探测脚本：
// 优先 nginx -T（能展开 include），否则扫常见配置目录；再把跨行指令拼成一行输出。
const formatProbeBody = `( if command -v nginx >/dev/null 2>&1; then nginx -T 2>/dev/null; else ` +
	`find /etc/nginx /usr/local/nginx/conf /opt/nginx/conf /usr/local/etc/nginx /etc/nginx/conf.d -name '*.conf' -type f 2>/dev/null | head -200 | xargs -r cat 2>/dev/null; fi ) | awk '
/^[ \t]*#/ { next }
inblock { buf = buf " " $0; if ($0 ~ /;/) { print buf; buf = ""; inblock = 0 } next }
/^[ \t]*(log_format|access_log)[ \t]/ { buf = $0; if ($0 ~ /;/) { print buf; buf = "" } else { inblock = 1 } next }
' 2>/dev/null | head -100
`

func buildFormatProbeScript() string { return formatProbeBody }

// parseLogFormats 解析探测输出，返回「格式名 -> 模板」以及 access_log 实际引用的格式名
func parseLogFormats(conf string) (map[string]string, map[string]bool) {
	formats, used := map[string]string{}, map[string]bool{}
	for _, raw := range strings.Split(conf, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "log_format") {
			m := logFormatDirectiveRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			var b strings.Builder
			for _, chunk := range quotedChunkRe.FindAllString(m[2], -1) {
				b.WriteString(chunk[1 : len(chunk)-1])
			}
			if b.Len() > 0 {
				formats[m[1]] = b.String()
			}
			continue
		}
		if strings.HasPrefix(line, "access_log") {
			m := accessLogDirectiveRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			parts := strings.Fields(m[1])
			if len(parts) == 0 || parts[0] == "off" {
				continue
			}
			if len(parts) >= 2 && !strings.HasPrefix(parts[1], "/") && !strings.Contains(parts[1], "=") {
				used[parts[1]] = true
			} else {
				used["combined"] = true // 未显式指定格式名时 nginx 用内置 combined
			}
		}
	}
	return formats, used
}

func sortedFormatNames(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// selectLogFormat 选出 access_log 实际引用、且含 $request/$status 的模板
func selectLogFormat(conf string) (string, string, bool) {
	formats, used := parseLogFormats(conf)
	if len(formats) == 0 {
		if used["combined"] {
			return "combined", defaultLogFormat, true
		}
		return "", "", false
	}
	if used["combined"] && formats["combined"] == "" {
		formats["combined"] = defaultLogFormat
	}
	valid := func(t string) bool {
		// $request 也可以用 $request_method + $request_uri 拆开写
		hasRequest := requestVarRe.MatchString(t) ||
			(strings.Contains(t, "$request_method") && strings.Contains(t, "$request_uri"))
		return hasRequest && statusVarRe.MatchString(t)
	}
	usedNames := make([]string, 0, len(used))
	for n := range used {
		usedNames = append(usedNames, n)
	}
	sort.Strings(usedNames)
	for _, n := range usedNames {
		if t := formats[n]; t != "" && valid(t) {
			return n, t, true
		}
	}
	for _, n := range sortedFormatNames(formats) {
		if valid(formats[n]) {
			return n, formats[n], true
		}
	}
	return "", "", false
}

// nginxTime 把前端 "2026-11-05T17:04" 转成脚本要求的 "05/Nov/2026:17"
func nginxTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("02/Jan/2006:15")
		}
	}
	return ""
}

// nginxDate 把前端时间转成脚本挑选 .gz 备份用的 "20261105"
func nginxDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("20060102")
		}
	}
	return ""
}

// ============================================================================
// 公共：权限校验 + 目标主机 + 凭证 + SSH
// ============================================================================

// resolveLogSSH 日志排查接口的 HTTP 适配层；实际授权校验与连接组装见 sshspec.go
func resolveLogSSH(c *gin.Context, ecsID int, network string, credID int) *logSSHSpec {
	uidStr, _ := c.Get("user_id")
	uid, _ := strconv.Atoi(fmt.Sprint(uidStr))
	role, _ := c.Get("role")
	roleStr, _ := role.(string)
	spec, err := resolveSSHSpec(uid, roleStr, ecsID, network, credID)
	if err != nil {
		status := http.StatusBadRequest
		var se *sshSpecError
		if errors.As(err, &se) {
			status = se.status
		}
		c.JSON(status, gin.H{"code": status, "msg": err.Error()})
		return nil
	}
	return spec
}

func runRemote(client *bssh.Client, script string, timeout time.Duration) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdin = strings.NewReader(script)
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run("sh -s") }()
	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-time.After(timeout):
		session.Close()
		return "", "", fmt.Errorf("远端执行超时（>%s）", timeout)
	}
}

// ============================================================================
// 第一步：路径探查（域名列表 + 脚本状态 + log_format 预览）
// ============================================================================

type exploreReq struct {
	EcsID        int    `json:"ecs_id"`
	Network      string `json:"network"`
	CredentialID int    `json:"credential_id"`
	ScriptPath   string `json:"script_path"`
}

type exploreData struct {
	Domains          []string `json:"domains"`
	CurrentDir       string   `json:"current_dir"`
	BackupDir        string   `json:"backup_dir"`
	PreparedDir      string   `json:"prepared_dir"`
	PreparedFiles    int      `json:"prepared_files"`
	Format           string   `json:"format"`
	FormatName       string   `json:"format_name"`
	FormatSource     string   `json:"format_source"`
	FormatWarning    string   `json:"format_warning"`
	ScriptPath       string   `json:"script_path"`
	ScriptExists     bool     `json:"script_exists"`
	ScriptSynced     bool     `json:"script_synced"`
	RotateScriptPath string   `json:"rotate_script_path"`
	RotateExists     bool     `json:"rotate_exists"`
	RotateSynced     bool     `json:"rotate_synced"`
	RotateCron       bool     `json:"rotate_cron"`
}

func embeddedSum(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func analyzerWantSum() string { return embeddedSum(scripts.NginxAnalyzer) }

func rotateWantSum() string { return embeddedSum(scripts.NginxLogRotate) }

func buildExploreScript(scriptPath string) string {
	var b strings.Builder
	// 域名来自脚本固定的两个目录（当前日志 + 切割备份），与脚本 FILE_LIST 口径一致
	b.WriteString("ls -1 " + currentLogDir + "/*.log " + backupLogDir + "/*.log.*.gz 2>/dev/null | grep -v '\\*' | xargs -r -n 1 basename \\\n")
	b.WriteString("  | sed -E 's/\\.(access|error)\\.log(\\.[0-9]+)?(\\.gz)?$//; s/\\.log(\\.[0-9]+)?(\\.gz)?$//' \\\n")
	b.WriteString("  | sed '/^$/d' | sort -u\n")
	b.WriteString("echo '__LOG_FORMATS__'\n")
	b.WriteString(formatProbeBody)
	b.WriteString("echo '__SCRIPT_STATE__'\n")
	b.WriteString("SUM=$(md5sum " + shellQuote(scriptPath) + " 2>/dev/null | awk '{print $1}')\n")
	b.WriteString("[ -f " + shellQuote(scriptPath) + " ] && echo \"1 ${SUM}\" || echo \"0 ${SUM}\"\n")
	// 只读探查「备份 + 预生成」闭环状态：切割脚本版本、cron、预生成数据份数
	b.WriteString("echo '__ECOSYSTEM__'\n")
	b.WriteString("RS=" + shellQuote(defaultRotateScriptPath) + "\n")
	b.WriteString("[ -f \"$RS\" ] && echo 'ROTATE_EXISTS=1' || echo 'ROTATE_EXISTS=0'\n")
	b.WriteString("echo \"ROTATE_SUM=$(md5sum \"$RS\" 2>/dev/null | awk '{print $1}')\"\n")
	b.WriteString("echo \"PREP_COUNT=$(find " + preparedLogDir + " -maxdepth 1 -name '*.tsv.gz' -type f 2>/dev/null | wc -l)\"\n")
	b.WriteString("if ( crontab -l 2>/dev/null; cat /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/* 2>/dev/null ) | grep -q 'nginx_log_rotate.sh'; then echo 'CRON=1'; else echo 'CRON=0'; fi\n")
	return b.String()
}

// ExploreLogs POST /api/bastion/logs/explore
func ExploreLogs(c *gin.Context) {
	var req exploreReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	if req.EcsID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少 ecs_id"})
		return
	}
	if req.ScriptPath == "" {
		req.ScriptPath = defaultAnalyzerPath
	}
	if !safeLogPath(req.ScriptPath) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "脚本路径含非法字符"})
		return
	}

	spec := resolveLogSSH(c, req.EcsID, req.Network, req.CredentialID)
	if spec == nil {
		return
	}
	client, err := spec.dial()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "SSH 连接失败: " + err.Error()})
		return
	}
	defer client.Close()

	out, stderr, err := runRemote(client, buildExploreScript(req.ScriptPath), 30*time.Second)
	if err != nil {
		msg := err.Error()
		if strings.TrimSpace(stderr) != "" {
			msg = strings.TrimSpace(stderr)
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "路径探查失败: " + msg})
		return
	}

	domainPart := out
	confPart, statePart, ecoPart := "", "", ""
	if i := strings.Index(out, "__LOG_FORMATS__"); i >= 0 {
		domainPart = out[:i]
		rest := out[i+len("__LOG_FORMATS__"):]
		if j := strings.Index(rest, "__SCRIPT_STATE__"); j >= 0 {
			confPart = rest[:j]
			rest = rest[j+len("__SCRIPT_STATE__"):]
			if k := strings.Index(rest, "__ECOSYSTEM__"); k >= 0 {
				statePart = rest[:k]
				ecoPart = rest[k+len("__ECOSYSTEM__"):]
			} else {
				statePart = rest
			}
		} else {
			confPart = rest
		}
	}

	state := strings.Fields(statePart)
	scriptExists := len(state) > 0 && state[0] == "1"
	scriptSynced := len(state) > 1 && state[1] == analyzerWantSum()

	eco := map[string]string{}
	for _, line := range strings.Split(ecoPart, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			eco[k] = strings.TrimSpace(v)
		}
	}
	preparedFiles, _ := strconv.Atoi(eco["PREP_COUNT"])

	seen := map[string]bool{}
	domains := []string{}
	for _, line := range strings.Split(domainPart, "\n") {
		d := strings.TrimSpace(line)
		if d == "" || d == "__LOG_FORMATS__" || d == "__SCRIPT_STATE__" || d == "__ECOSYSTEM__" || seen[d] {
			continue
		}
		seen[d] = true
		domains = append(domains, d)
	}
	sort.Strings(domains)

	format, formatName, source, warning := defaultLogFormat, "combined", "default", ""
	if name, tpl, ok := selectLogFormat(confPart); ok {
		format, formatName, source = tpl, name, "nginx_conf"
	}
	if source == "default" {
		warning = "未能自动读取 Nginx 的 log_format。分析脚本会自行探测；仍读不到时回退内置正则解析。"
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": exploreData{
		Domains:          domains,
		CurrentDir:       currentLogDir,
		BackupDir:        backupLogDir,
		PreparedDir:      preparedLogDir,
		PreparedFiles:    preparedFiles,
		Format:           format,
		FormatName:       formatName,
		FormatSource:     source,
		FormatWarning:    warning,
		ScriptPath:       req.ScriptPath,
		ScriptExists:     scriptExists,
		ScriptSynced:     scriptSynced,
		RotateScriptPath: defaultRotateScriptPath,
		RotateExists:     eco["ROTATE_EXISTS"] == "1",
		RotateSynced:     eco["ROTATE_SUM"] == rotateWantSum(),
		RotateCron:       eco["CRON"] == "1",
	}})
}

// ============================================================================
// 第二步：同步脚本 + 调用（异步 Task）
// ============================================================================

type analyzeReq struct {
	EcsID        int    `json:"ecs_id"`
	Network      string `json:"network"`
	CredentialID int    `json:"credential_id"`
	Domains      string `json:"domains"`
	Start        string `json:"start"`
	End          string `json:"end"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	IP           string `json:"ip"`
	ScriptPath   string `json:"script_path"`
}

type logTask struct {
	Status   string      `json:"status"` // running | done | error
	Data     interface{} `json:"data,omitempty"`
	Error    string      `json:"error,omitempty"`
	DebugCmd string      `json:"debug_cmd,omitempty"` // 可在服务器直接执行的复现命令
	Started  time.Time   `json:"-"`
}

var logTaskStore sync.Map

// analyzerReproCommand 生成一条可直接在目标服务器粘贴执行的命令，
// 便于在服务器上跳过 SSH 同步逻辑、单独验证脚本本身有没有数据。
func analyzerReproCommand(req analyzeReq) string {
	sp := req.ScriptPath
	if sp == "" {
		sp = defaultAnalyzerPath
	}
	args := []string{
		req.Domains,
		nginxTime(req.Start),
		nginxTime(req.End),
		req.Status,
		req.URL,
		req.IP,
		nginxDate(req.Start),
		nginxDate(req.End),
		"auto", // 第 9 个参数：让脚本自行探测 nginx log_format，与平台下发的结果一致
	}
	parts := []string{"bash", shellQuote(sp)}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// validateAnalyzeReq 只做参数校验，保证同步请求能立即返回 400，不必等 SSH
func validateAnalyzeReq(req analyzeReq) error {
	if req.ScriptPath != "" && !safeLogPath(req.ScriptPath) {
		return fmt.Errorf("脚本路径含非法字符")
	}
	for _, d := range strings.Split(req.Domains, ",") {
		d = strings.TrimSpace(d)
		if d != "" && !safeDomainRe.MatchString(d) {
			return fmt.Errorf("域名含非法字符: %s", d)
		}
	}
	for name, v := range map[string]string{"状态码": req.Status, "URL": req.URL, "IP": req.IP} {
		if !safeArg(v) {
			return fmt.Errorf("%s 含非法字符（不支持双引号、分号、反斜杠等）", name)
		}
	}
	startDate, endDate := nginxDate(req.Start), nginxDate(req.End)
	// 日期跨度保护：脚本按天循环展开备份文件，跨度过大将拖慢执行
	if startDate != "" && endDate != "" {
		if s, err1 := time.Parse("20060102", startDate); err1 == nil {
			if e, err2 := time.Parse("20060102", endDate); err2 == nil {
				if e.Before(s) {
					return fmt.Errorf("结束日期早于起始日期")
				}
				if e.Sub(s) > 31*24*time.Hour {
					return fmt.Errorf("日期跨度不能超过 31 天，请缩小范围")
				}
			}
		}
	}
	return nil
}

// buildEcosystemSyncCommand 让「平台跑过一次」就把目标服务器纳入备份 + 分析闭环：
//   - md5 同步分析脚本与切割脚本；切割脚本沿用服务器原路径，已有的 crontab
//     不用改就会自动调用平台版本，首次覆盖前留一份 .bak 便于回滚
//   - 未检测到任何 cron 调度时补装 /etc/cron.d（非 root 时静默跳过）
//   - 把本次探测到的 log_format 落盘，切割脚本预生成时复用；探测失败则删掉旧模板，
//     让预生成与在线分析一起退回 auto 探测，指纹仍然一致
func buildEcosystemSyncCommand(scriptPath, formatTemplate string) string {
	var b strings.Builder

	b.WriteString("SP=" + shellQuote(scriptPath) + "\n")
	b.WriteString("WANT=" + shellQuote(analyzerWantSum()) + "\n")
	b.WriteString("CUR=$(md5sum \"$SP\" 2>/dev/null | awk '{print $1}')\n")
	b.WriteString("if [ \"$CUR\" != \"$WANT\" ]; then\n")
	b.WriteString("  mkdir -p \"$(dirname \"$SP\")\" 2>/dev/null\n")
	b.WriteString("  cat > \"$SP\" <<'__NGINX_ANALYZER_SH__'\n")
	b.WriteString(scripts.NginxAnalyzer)
	if !strings.HasSuffix(scripts.NginxAnalyzer, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("__NGINX_ANALYZER_SH__\n")
	b.WriteString("  chmod +x \"$SP\" 2>/dev/null\n")
	b.WriteString("fi\n")
	// 用户自定义分析脚本路径时，仍在默认路径留一份供 cron 切割脚本调用
	if scriptPath != defaultAnalyzerPath {
		b.WriteString("cp -f \"$SP\" " + shellQuote(defaultAnalyzerPath) + " 2>/dev/null || true\n")
	}

	b.WriteString("RS=" + shellQuote(defaultRotateScriptPath) + "\n")
	b.WriteString("RWANT=" + shellQuote(rotateWantSum()) + "\n")
	b.WriteString("RCUR=$(md5sum \"$RS\" 2>/dev/null | awk '{print $1}')\n")
	b.WriteString("if [ \"$RCUR\" != \"$RWANT\" ]; then\n")
	b.WriteString("  [ -f \"$RS\" ] && cp -p \"$RS\" \"$RS.bak.$(date +%Y%m%d%H%M%S)\" 2>/dev/null\n")
	b.WriteString("  cat > \"$RS\" <<'__NGINX_LOG_ROTATE_SH__'\n")
	b.WriteString(scripts.NginxLogRotate)
	if !strings.HasSuffix(scripts.NginxLogRotate, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("__NGINX_LOG_ROTATE_SH__\n")
	b.WriteString("  chmod +x \"$RS\" 2>/dev/null\n")
	b.WriteString("  echo '[cloud-manager] 已同步 nginx 日志切割脚本（旧版本备份为 .bak.*）' >&2\n")
	b.WriteString("fi\n")

	// 已有调度（用户原有 crontab / 其它 cron.d）一律不动，避免一天切割两次
	b.WriteString("if ! ( crontab -l 2>/dev/null; cat /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/* 2>/dev/null ) | grep -q 'nginx_log_rotate.sh'; then\n")
	b.WriteString("  cat > /etc/cron.d/nginx_log_rotate <<'__NGINX_ROTATE_CRON__' 2>/dev/null || true\n")
	b.WriteString("0 0 * * * root " + defaultRotateScriptPath + " >/dev/null 2>&1\n")
	b.WriteString("__NGINX_ROTATE_CRON__\n")
	b.WriteString("  chmod 0644 /etc/cron.d/nginx_log_rotate 2>/dev/null || true\n")
	b.WriteString("fi\n")

	if formatTemplate == "" {
		b.WriteString("rm -f " + shellQuote(defaultFormatTplPath) + " 2>/dev/null\n")
	} else {
		b.WriteString("cat > " + shellQuote(defaultFormatTplPath) + " <<'__NGINX_LOG_FORMAT_TPL__'\n")
		b.WriteString(formatTemplate + "\n")
		b.WriteString("__NGINX_LOG_FORMAT_TPL__\n")
	}
	return b.String()
}

// buildAnalyzerCommand：同步平台生态（两份脚本 + cron + log_format），随后传 9 个参数执行。
// formatTemplate 为空时传 auto，由脚本自行探测 log_format。
func buildAnalyzerCommand(req analyzeReq, formatTemplate string) string {
	sp := req.ScriptPath
	if sp == "" {
		sp = defaultAnalyzerPath
	}
	startDate, endDate := nginxDate(req.Start), nginxDate(req.End)
	formatArg := formatTemplate
	if formatArg == "" {
		formatArg = "auto"
	}
	args := []string{
		shellQuote(req.Domains),
		shellQuote(nginxTime(req.Start)),
		shellQuote(nginxTime(req.End)),
		shellQuote(req.Status),
		shellQuote(req.URL),
		shellQuote(req.IP),
		shellQuote(startDate),
		shellQuote(endDate),
		shellQuote(formatArg),
	}

	// 打印实际传参，方便在服务器日志里核对「时间/日期是否为空导致不读备份」
	log.Printf("[nginx_analyzer] 参数: DOMAINS_STR=%q START_TIME=%q END_TIME=%q STATUS=%q URL_KEYWORD=%q IP_FILTER=%q START_DATE=%q END_DATE=%q FORMAT=%q",
		req.Domains, nginxTime(req.Start), nginxTime(req.End), req.Status, req.URL, req.IP, startDate, endDate, formatArg)
	log.Printf("[nginx_analyzer] 服务器复现命令: %s", analyzerReproCommand(req))
	log.Printf("[nginx_analyzer] 本地脚本 md5=%s 切割脚本 md5=%s 远端路径=%s（不一致时将覆盖服务器脚本）",
		analyzerWantSum(), rotateWantSum(), sp)

	var b strings.Builder
	b.WriteString(buildEcosystemSyncCommand(sp, formatTemplate))
	b.WriteString("bash \"$SP\" " + strings.Join(args, " ") + "\n")
	return b.String()
}

// fixLooseEscapes 兼容旧版远端脚本：日志里的 \vendor、\xHH 等反斜杠未转义就拼进
// JSON 字符串，会产生非法转义（invalid escape sequence \x in string 等）。
// 处理三类问题：非法反斜杠转义补成 \\、字符串内裸控制字符替换成空格、
// \u 后不是合法 4 位十六进制时同样补成 \\。合法转义保持原样。
func fixLooseEscapes(b []byte) []byte {
	out := make([]byte, 0, len(b)+32)
	inStr := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if !inStr {
			out = append(out, c)
			if c == '"' {
				inStr = true
			}
			continue
		}
		if c == '"' {
			inStr = false
			out = append(out, c)
			continue
		}
		// 日志里可能带真实 NUL / 控制字节，JSON 字符串不允许裸控制字符
		if c < 0x20 {
			out = append(out, ' ')
			continue
		}
		if c != '\\' {
			out = append(out, c)
			continue
		}
		if i+1 >= len(b) {
			out = append(out, '\\', '\\')
			continue
		}
		n := b[i+1]
		switch n {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			out = append(out, c, n)
		case 'u':
			if i+5 < len(b) && isHex4(b[i+2:i+6]) {
				out = append(out, c, n, b[i+2], b[i+3], b[i+4], b[i+5])
				i += 4
			} else {
				out = append(out, '\\', '\\', n)
			}
		default:
			// \x00、\v 等 nginx 十六进制转义 / 非法转义：补成字面反斜杠
			out = append(out, '\\', '\\', n)
		}
		i++
	}
	return out
}

func isHex4(s []byte) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func parseScriptOutput(out string) (json.RawMessage, error) {
	i := strings.Index(out, "{")
	j := strings.LastIndex(out, "}")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("分析脚本未返回有效 JSON，原始输出：%s", strings.TrimSpace(out))
	}
	raw := []byte(out[i : j+1])
	// 后端已重编译但远端脚本同步失败（或仍是旧版）时，输出会带非法转义；
	// 修复后能解析就继续返回结果，避免仅仅因为一条 \vendor URL 让整次分析失败。
	if !json.Valid(raw) {
		if loose := fixLooseEscapes(raw); !bytes.Equal(loose, raw) && json.Valid(loose) {
			log.Printf("[nginx_analyzer] 远端脚本输出含非法转义，已兼容修复；本地脚本 md5=%s（建议重新编译后端以同步新脚本）", analyzerWantSum())
			raw = loose
		}
	}
	var v struct {
		Code  int             `json:"code"`
		Msg   string          `json:"msg"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("解析脚本输出失败: %v（本地脚本 md5=%s，与远端不一致说明后端未重新编译或脚本同步失败）；原始输出：%s", err, analyzerWantSum(), strings.TrimSpace(out))
	}
	if v.Error != "" {
		return nil, fmt.Errorf("分析脚本报错: %s", v.Error)
	}
	if v.Code != 0 {
		return nil, fmt.Errorf("%s", v.Msg)
	}
	if len(v.Data) == 0 {
		return json.RawMessage("{}"), nil
	}
	return v.Data, nil
}

// AnalyzeLogs POST /api/bastion/logs/analyze
func AnalyzeLogs(c *gin.Context) {
	var req analyzeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	if req.EcsID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少 ecs_id"})
		return
	}
	if err := validateAnalyzeReq(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	repro := analyzerReproCommand(req)
	spec := resolveLogSSH(c, req.EcsID, req.Network, req.CredentialID)
	if spec == nil {
		return
	}

	now := time.Now()
	logTaskStore.Range(func(k, v interface{}) bool {
		if t, ok := v.(*logTask); ok && now.Sub(t.Started) > 10*time.Minute {
			logTaskStore.Delete(k)
		}
		return true
	})
	taskID := strconv.FormatInt(now.UnixNano(), 36)
	logTaskStore.Store(taskID, &logTask{Status: "running", Started: now, DebugCmd: repro})

	go func() {
		client, err := spec.dial()
		if err != nil {
			logTaskStore.Store(taskID, &logTask{Status: "error", Error: "SSH 连接失败: " + err.Error(), Started: now, DebugCmd: repro})
			return
		}
		defer client.Close()

		// 分析前探测服务器当前生效的 log_format，作为第 9 个参数下发给脚本。
		// 探测失败不阻断分析：脚本会自行探测，仍读不到时回退内置正则。
		formatTemplate := ""
		if probeOut, _, probeErr := runRemote(client, buildFormatProbeScript(), 30*time.Second); probeErr == nil {
			if name, tpl, ok := selectLogFormat(probeOut); ok {
				formatTemplate = tpl
				log.Printf("[nginx_analyzer] 采用 log_format %q: %s", name, tpl)
			}
		}
		if formatTemplate == "" {
			log.Printf("[nginx_analyzer] 未读取到 log_format，脚本将自行探测或回退内置正则")
		}

		command := buildAnalyzerCommand(req, formatTemplate)
		out, stderr, err := runRemote(client, command, 180*time.Second)
		if s := strings.TrimSpace(stderr); s != "" {
			log.Printf("[nginx_analyzer] 远端脚本输出: %s", s)
		}
		if err != nil {
			msg := err.Error()
			if strings.TrimSpace(stderr) != "" {
				msg = strings.TrimSpace(stderr)
			}
			logTaskStore.Store(taskID, &logTask{Status: "error", Error: msg, Started: now, DebugCmd: repro})
			return
		}
		data, err := parseScriptOutput(out)
		if err != nil {
			logTaskStore.Store(taskID, &logTask{Status: "error", Error: err.Error(), Started: now, DebugCmd: repro})
			return
		}
		logTaskStore.Store(taskID, &logTask{Status: "done", Data: data, Started: now, DebugCmd: repro})
	}()

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"task_id": taskID, "debug_cmd": repro}})
}

// GetLogTask GET /api/bastion/logs/task/:id
func GetLogTask(c *gin.Context) {
	v, ok := logTaskStore.Load(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在或已过期"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": v.(*logTask)})
}
