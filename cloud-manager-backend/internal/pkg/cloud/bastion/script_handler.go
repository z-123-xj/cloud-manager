package bastion

// ============================================================================
// 脚本库：脚本正文与参数定义都放在独立目录的文件里，服务器上直接放 .sh
// 即可自动发现；页面添加/上传也写同一个目录。数据库不存脚本正文。
//
// 目录结构（最多一层分类子目录）：
//   <script_dir>/
//     web/install-tengine.sh     脚本正文，可含 {{PORT}} 占位符
//     web/install-tengine.json   同名元数据（可选，缺省则用文件名/目录名兜底）
//
// 执行时由服务端读文件、校验参数、渲染占位符，再把「渲染后正文 + 引用注释头」
// 交给原有批量执行链路（stdin -> sh -s），前端无法篡改脚本来源。
// ============================================================================

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud-manager-backend/internal/config"

	"github.com/gin-gonic/gin"
)

const maxScriptFileBytes = 512 * 1024

// scriptParam 参数定义，对应同名 .json 里的 params 数组
type scriptParam struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // text / number / select
	Default  string   `json:"default"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
	Regex    string   `json:"regex"`
	Min      *int     `json:"min"`
	Max      *int     `json:"max"`
}

// scriptMeta 脚本元数据（同名 .json 的内容）
type scriptMeta struct {
	Name        string        `json:"name"`
	Category    string        `json:"category"`
	Description string        `json:"description"`
	Shell       string        `json:"shell"`
	TimeoutSec  int           `json:"timeout_sec"`
	Concurrency int           `json:"concurrency"`
	Params      []scriptParam `json:"params"`
}

// scriptInfo 列表项：不返回脚本正文，避免列表请求传输大文件
type scriptInfo struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Category    string        `json:"category"`
	Description string        `json:"description"`
	Shell       string        `json:"shell"`
	TimeoutSec  int           `json:"timeout_sec"`
	Concurrency int           `json:"concurrency"`
	Params      []scriptParam `json:"params"`
	Size        int64         `json:"size"`
	UpdatedAt   string        `json:"updated_at"`
	HasMeta     bool          `json:"has_meta"`
}

type scriptFile struct {
	ID   string
	Meta scriptMeta
	Body string
	Hash string // 正文 sha256 前 8 位，作为版本号写入任务快照
}

var (
	reSafeName  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	reUnsafeVal = regexp.MustCompile("[\r\n\"'`$;|&<>\\\\(){}!]")
	rePholder   = regexp.MustCompile(`\{\{[A-Za-z0-9_-]+\}\}`)
)

// scriptRoot 返回脚本库的绝对路径。相对路径以进程工作目录为基准，
// 生产环境请在 config.yaml 里配绝对路径。
func scriptRoot() (string, error) {
	dir := config.GetBastionConfig().ScriptDir
	if dir == "" {
		dir = "script-library"
	}
	if !filepath.IsAbs(dir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(cwd, dir)
	}
	return filepath.Clean(dir), nil
}

// safeScriptPath 校验脚本 ID（相对路径），返回根目录与绝对路径。
// 白名单文件名 + 禁止 .. + Rel 二次校验，防止目录穿越。
func safeScriptPath(id string) (string, string, error) {
	root, err := scriptRoot()
	if err != nil {
		return "", "", err
	}
	id = strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	if id == "" || !strings.HasSuffix(strings.ToLower(id), ".sh") {
		return "", "", fmt.Errorf("脚本 ID 必须是 .sh 文件相对路径")
	}
	clean := filepath.Clean(filepath.FromSlash(id))
	if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, "..") {
		return "", "", fmt.Errorf("脚本路径非法")
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if len(parts) > 2 {
		return "", "", fmt.Errorf("脚本最多放一层分类目录")
	}
	for i, p := range parts {
		base := p
		if i == len(parts)-1 {
			base = p[:len(p)-3] // 去掉 .sh
		}
		if !reSafeName.MatchString(base) {
			return "", "", fmt.Errorf("脚本名或目录含非法字符: %s", p)
		}
	}
	full := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("脚本路径越界")
	}
	return root, full, nil
}

// normalizeScript 统一 LF 并去 BOM：Windows 上传的 CRLF 会让远端 sh 报 ^M 错误
func normalizeScript(s string) string {
	s = strings.TrimPrefix(s, "\xef\xbb\xbf")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func metaJSONPath(shPath string) string {
	return strings.TrimSuffix(shPath, filepath.Ext(shPath)) + ".json"
}

// pruneEmptyCategory 脚本被改名/移走/删除后，清掉已经空了的那一层分类目录。
// 只处理 root 下的一层子目录，且仅用 os.Remove 删空目录，不递归删文件。
func pruneEmptyCategory(root, shPath string) {
	cleanRoot := filepath.Clean(root)
	dir := filepath.Clean(filepath.Dir(shPath))
	if dir == cleanRoot || filepath.Dir(dir) != cleanRoot {
		return
	}
	if !reSafeName.MatchString(filepath.Base(dir)) {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(dir)
}

// readMeta 读同名 .json；没有时用文件名/子目录名和默认值兜底，
// 这样只丢一个 .sh 到目录里也能被自动发现并执行。
func readMeta(root, shPath string) scriptMeta {
	name := strings.TrimSuffix(filepath.Base(shPath), filepath.Ext(shPath))
	category := "未分类"
	if dir := filepath.Dir(shPath); filepath.Clean(dir) != filepath.Clean(root) {
		category = filepath.Base(dir)
	}
	m := scriptMeta{
		Name: name, Category: category, Shell: "sh",
		TimeoutSec: 300, Concurrency: 5, Params: []scriptParam{},
	}
	if b, err := os.ReadFile(metaJSONPath(shPath)); err == nil {
		var parsed scriptMeta
		if err := json.Unmarshal(b, &parsed); err == nil {
			if parsed.Name != "" {
				m.Name = parsed.Name
			}
			if parsed.Category != "" {
				m.Category = parsed.Category
			}
			if parsed.Description != "" {
				m.Description = parsed.Description
			}
			if parsed.Shell != "" {
				if _, err := shellCommand(parsed.Shell); err == nil {
					m.Shell = parsed.Shell
				}
			}
			if parsed.TimeoutSec > 0 {
				m.TimeoutSec = parsed.TimeoutSec
			}
			if parsed.Concurrency > 0 {
				m.Concurrency = parsed.Concurrency
			}
			if parsed.Params != nil {
				m.Params = parsed.Params
			}
		}
	}
	return m
}

// loadScript 读取脚本正文并计算内容版本
func loadScript(id string) (*scriptFile, error) {
	root, full, err := safeScriptPath(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("读取脚本失败: %v", err)
	}
	if len(b) > maxScriptFileBytes {
		return nil, fmt.Errorf("脚本文件超过 %d 字节", maxScriptFileBytes)
	}
	body := normalizeScript(string(b))
	sum := sha256.Sum256([]byte(body))
	return &scriptFile{
		ID: id, Meta: readMeta(root, full), Body: body,
		Hash: hex.EncodeToString(sum[:])[:8],
	}, nil
}

func validateParamValue(p scriptParam, v string) error {
	if len(v) > 256 {
		return fmt.Errorf("参数 %s 过长", p.Key)
	}
	// 参数值会替换进脚本正文，禁止换行与 shell 元字符，避免注入新的命令
	if reUnsafeVal.MatchString(v) {
		return fmt.Errorf("参数 %s 含非法字符（不允许引号、换行、$、反引号、管道等）", p.Key)
	}
	switch p.Type {
	case "number":
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("参数 %s 必须是数字", p.Key)
		}
		if p.Min != nil && n < *p.Min {
			return fmt.Errorf("参数 %s 不能小于 %d", p.Key, *p.Min)
		}
		if p.Max != nil && n > *p.Max {
			return fmt.Errorf("参数 %s 不能大于 %d", p.Key, *p.Max)
		}
	case "select":
		ok := false
		for _, o := range p.Options {
			if o == v {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("参数 %s 的值不在允许范围内", p.Key)
		}
	}
	if p.Regex != "" {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return fmt.Errorf("参数 %s 的正则配置有误", p.Key)
		}
		if !re.MatchString(v) {
			return fmt.Errorf("参数 %s 格式不正确", p.Key)
		}
	}
	return nil
}

// insertHeader shebang 之后插入注释头，避免破坏 #!/bin/bash
func insertHeader(body, header string) string {
	if strings.HasPrefix(body, "#!") {
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			return body[:i+1] + header + body[i+1:]
		}
		return body + "\n" + header
	}
	return header + body
}

// renderScript 按元数据校验参数、替换 {{VAR}}，并在正文头部写入引用快照。
// 快照以 shell 注释形式存在，任务详情与「应用历史」都能看到当时用的脚本版本与参数。
func renderScript(sf *scriptFile, params map[string]string) (string, error) {
	if params == nil {
		params = map[string]string{}
	}
	resolved := make(map[string]string, len(sf.Meta.Params))
	defined := make(map[string]bool, len(sf.Meta.Params))
	for _, p := range sf.Meta.Params {
		key := strings.TrimSpace(p.Key)
		if !reSafeName.MatchString(key) {
			return "", fmt.Errorf("参数名非法: %s", p.Key)
		}
		defined[key] = true
		v, ok := params[key]
		if !ok || v == "" {
			v = p.Default
		}
		if p.Required && strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("缺少必填参数: %s", p.Label)
		}
		if err := validateParamValue(p, v); err != nil {
			return "", err
		}
		resolved[key] = v
	}
	for k := range params {
		if !defined[k] {
			return "", fmt.Errorf("未定义的参数: %s", k)
		}
	}
	rendered := sf.Body
	for k, v := range resolved {
		rendered = strings.ReplaceAll(rendered, "{{"+k+"}}", v)
	}
	if m := rePholder.FindString(rendered); m != "" {
		return "", fmt.Errorf("脚本中存在未定义的占位符: %s", m)
	}
	cfg := config.GetBatchConfig()
	if len(rendered)+512 > cfg.MaxScriptBytes {
		return "", fmt.Errorf("渲染后脚本超过 %d 字节上限，可调大 bastion.batch_max_script_bytes", cfg.MaxScriptBytes)
	}
	snap, _ := json.Marshal(resolved)
	header := fmt.Sprintf("# cloud-manager script: %s | sha256: %s | params: %s\n", sf.ID, sf.Hash, string(snap))
	return insertHeader(rendered, header), nil
}

// scanScriptInfos 每次请求重新扫描目录（文件量小、成本可忽略），
// 服务器上新增/修改文件刷新页面即可发现，无需重启、无需轮询。
func scanScriptInfos(root string) ([]scriptInfo, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	dirs := []string{root}
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range rootEntries {
		if e.IsDir() && reSafeName.MatchString(e.Name()) {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	infos := make([]scriptInfo, 0)
	for _, d := range dirs {
		files, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".sh") {
				continue
			}
			fi, err := f.Info()
			if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
				continue
			}
			full := filepath.Join(d, f.Name())
			rel, _ := filepath.Rel(root, full)
			m := readMeta(root, full)
			_, metaErr := os.Stat(metaJSONPath(full))
			infos = append(infos, scriptInfo{
				ID: filepath.ToSlash(rel), Name: m.Name, Category: m.Category,
				Description: m.Description, Shell: m.Shell,
				TimeoutSec: m.TimeoutSec, Concurrency: m.Concurrency, Params: m.Params,
				Size: fi.Size(), UpdatedAt: fi.ModTime().Format(time.RFC3339),
				HasMeta: metaErr == nil,
			})
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Category != infos[j].Category {
			return infos[i].Category < infos[j].Category
		}
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

// writeAtomic 临时文件 + rename，避免页面读到写了一半的脚本
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-script-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// ListScripts GET /api/bastion/scripts —— 自动发现目录里的脚本
func ListScripts(c *gin.Context) {
	root, err := scriptRoot()
	if err != nil {
		batchFail(c, "脚本目录配置错误: %v", err)
		return
	}
	infos, err := scanScriptInfos(root)
	if err != nil {
		batchFail(c, "扫描脚本目录失败: %v", err)
		return
	}
	batchOK(c, gin.H{"dir": root, "items": infos, "max_bytes": maxScriptFileBytes})
}

// GetScript GET /api/bastion/scripts/detail?id=xxx —— 返回正文与元数据
func GetScript(c *gin.Context) {
	sf, err := loadScript(c.Query("id"))
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	batchOK(c, gin.H{
		"id": sf.ID, "name": sf.Meta.Name, "category": sf.Meta.Category,
		"description": sf.Meta.Description, "shell": sf.Meta.Shell,
		"timeout_sec": sf.Meta.TimeoutSec, "concurrency": sf.Meta.Concurrency,
		"params": sf.Meta.Params, "content": sf.Body, "hash": sf.Hash,
	})
}

// PreviewScript GET /api/bastion/scripts/preview?id=xxx&params=<json>
// 只读渲染预览，用 GET 避免被全局 ReadonlyMiddleware 拦截
func PreviewScript(c *gin.Context) {
	sf, err := loadScript(c.Query("id"))
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	params := map[string]string{}
	if raw := c.Query("params"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			batchFail(c, "参数格式错误")
			return
		}
	}
	rendered, err := renderScript(sf, params)
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	batchOK(c, gin.H{"content": rendered, "hash": sf.Hash})
}

type saveScriptReq struct {
	ID          string        `json:"id"`
	OldID       string        `json:"old_id"` // 编辑前的脚本 ID；与 ID 不同时表示改名/换分类
	Content     string        `json:"content"`
	Name        string        `json:"name"`
	Category    string        `json:"category"`
	Description string        `json:"description"`
	Shell       string        `json:"shell"`
	TimeoutSec  int           `json:"timeout_sec"`
	Concurrency int           `json:"concurrency"`
	Params      []scriptParam `json:"params"`
}

// SaveScript POST /api/bastion/scripts —— 页面新建/编辑（仅管理员）
func SaveScript(c *gin.Context) {
	var req saveScriptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		batchFail(c, "请求参数错误: %s", err.Error())
		return
	}
	root, full, err := safeScriptPath(req.ID)
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}

	// 编辑时允许改文件名/分类：等价于重命名。先确认目标名没被占用，
	// 写完新文件后再删旧文件与旧元数据，最后清掉空掉的分类目录。
	renamed := false
	var oldFull string
	if req.OldID != "" {
		_, old, err := safeScriptPath(req.OldID)
		if err != nil {
			batchFail(c, "原脚本路径非法: %s", err.Error())
			return
		}
		if old != full {
			if _, err := os.Stat(full); err == nil {
				batchFail(c, "目标脚本 %s 已存在，请换个文件名或分类", req.ID)
				return
			} else if !os.IsNotExist(err) {
				batchFail(c, "检查目标脚本失败: %v", err)
				return
			}
			renamed = true
			oldFull = old
		}
	}

	content := normalizeScript(req.Content)
	if strings.TrimSpace(content) == "" {
		batchFail(c, "脚本内容不能为空")
		return
	}
	if len(content) > maxScriptFileBytes {
		batchFail(c, "脚本内容超过 %d 字节", maxScriptFileBytes)
		return
	}
	for _, p := range req.Params {
		if !reSafeName.MatchString(strings.TrimSpace(p.Key)) {
			batchFail(c, "参数名只能用字母、数字、下划线和短横线: %s", p.Key)
			return
		}
	}
	meta := scriptMeta{
		Name: strings.TrimSpace(req.Name), Category: strings.TrimSpace(req.Category),
		Description: strings.TrimSpace(req.Description), Shell: req.Shell,
		TimeoutSec: req.TimeoutSec, Concurrency: req.Concurrency, Params: req.Params,
	}
	if meta.Shell == "" {
		meta.Shell = "sh"
	}
	if _, err := shellCommand(meta.Shell); err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	if meta.Params == nil {
		meta.Params = []scriptParam{}
	}
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err := writeAtomic(full, []byte(content), 0644); err != nil {
		batchFail(c, "保存脚本失败: %v", err)
		return
	}
	if err := writeAtomic(metaJSONPath(full), append(mb, '\n'), 0644); err != nil {
		batchFail(c, "保存元数据失败: %v", err)
		return
	}
	warn := ""
	if renamed {
		if err := os.Remove(oldFull); err != nil && !os.IsNotExist(err) {
			warn = fmt.Sprintf("新脚本已保存，但旧文件清理失败: %v", err)
		}
		_ = os.Remove(metaJSONPath(oldFull)) // 同名 .json，不存在时忽略
		pruneEmptyCategory(root, oldFull)
	}
	data := gin.H{"id": req.ID, "renamed": renamed}
	if warn != "" {
		data["warn"] = warn
	}
	batchOK(c, data)
}

// UploadScript POST /api/bastion/scripts/upload —— multipart 上传 .sh（仅管理员）
// 与 /bastion/upload/:asset_id 不同：这里写到控制端本地脚本目录，不是 SFTP 到目标机
func UploadScript(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxScriptFileBytes+4096)
	f, header, err := c.Request.FormFile("file")
	if err != nil {
		batchFail(c, "读取上传文件失败: %v", err)
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		batchFail(c, "读取上传文件失败: %v", err)
		return
	}
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if !strings.HasSuffix(strings.ToLower(name), ".sh") || len(name) <= 3 {
		batchFail(c, "只能上传 .sh 文件")
		return
	}
	base := name[:len(name)-3]
	if !reSafeName.MatchString(base) {
		batchFail(c, "文件名只能用字母、数字、下划线和短横线")
		return
	}
	category := strings.TrimSpace(c.PostForm("category"))
	if category != "" && !reSafeName.MatchString(category) {
		batchFail(c, "分类名只能用字母、数字、下划线和短横线")
		return
	}
	id := name
	if category != "" {
		id = category + "/" + name
	}
	_, full, err := safeScriptPath(id)
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	content := normalizeScript(string(b))
	if len(content) > maxScriptFileBytes {
		batchFail(c, "脚本内容超过 %d 字节", maxScriptFileBytes)
		return
	}
	if err := writeAtomic(full, []byte(content), 0644); err != nil {
		batchFail(c, "保存上传脚本失败: %v", err)
		return
	}
	batchOK(c, gin.H{"id": id})
}

// DeleteScript DELETE /api/bastion/scripts?id=xxx （仅管理员）
func DeleteScript(c *gin.Context) {
	root, full, err := safeScriptPath(c.Query("id"))
	if err != nil {
		batchFail(c, "%s", err.Error())
		return
	}
	if err := os.Remove(full); err != nil {
		batchFail(c, "删除脚本失败: %v", err)
		return
	}
	os.Remove(metaJSONPath(full)) // 元数据一并清理，不存在时忽略
	pruneEmptyCategory(root, full)
	batchOK(c, gin.H{"id": c.Query("id")})
}
