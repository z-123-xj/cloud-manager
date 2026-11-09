package ssh

// SSH 会话管理器：管理连接的生命周期、会话保持、超时回收。
//
// 设计要点（方案 A：刷新复用同一 shell）：
//   - 一个 SessionInfo 持有「持久化的 pty shell」+「输出环形缓冲」，与 WebSocket 生命周期解耦。
//   - WebSocket 连接/断开只做 attach/detach：断开时 shell 不关闭，远端输出继续写入缓冲；
//     刷新后用同一个 session_id 重新 attach，回放缓冲即可恢复屏幕内容与正在运行的进程。
//   - 仅「真实终端 I/O」（远端输出 / 用户输入）才刷新 LastAccess；前端心跳(ping)与 resize 不计入，
//     因此空闲超过 session_timeout 会被 reaper 回收（解决“一直发心跳却永不超时”的问题）。

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"cloud-manager-backend/pkg/logger"

	gossh "golang.org/x/crypto/ssh"
	"github.com/gorilla/websocket"
)

// SessionTimeout SSH 会话无操作自动断开时长（由配置文件注入，默认 30 分钟）
var SessionTimeout = 30 * time.Minute

// SetSessionTimeout 由启动流程根据配置文件设置（分钟）
func SetSessionTimeout(minutes int) {
	if minutes <= 0 {
		minutes = 30
	}
	SessionTimeout = time.Duration(minutes) * time.Minute
}

// SessionInfo SSH 会话信息（含持久化 pty 与输出缓冲）
type SessionInfo struct {
	ID         string // 会话唯一标识
	AssetID    uint   // 关联资产ID
	AssetName  string // 资产名称
	Client     *gossh.Client
	CreatedAt  time.Time
	LastAccess time.Time // 最后「真实终端 I/O」时间，用于超时回收
	mu         sync.Mutex

	// 持久化 pty shell（与 WS 生命周期解耦）
	pty    *gossh.Session
	stdin  io.WriteCloser
	stdout io.Reader
	outBuf *RingBuffer // 远端输出环形缓冲，供刷新后回放
	cols   int
	rows   int
	done   chan struct{} // pty 读取 goroutine 退出信号

	// 当前附着的 WebSocket（nil 表示已 detach，shell 仍在后台运行）
	attached   *websocket.Conn
	attachedMu sync.Mutex
	wsMu       sync.Mutex // 保护对 attached WS 的写入
}

// RingBuffer 简单有界字节缓冲：超过容量则丢弃最旧数据，用于回放终端历史
type RingBuffer struct {
	buf []byte
	cap int
	mu  sync.Mutex
}

func NewRingBuffer(cap int) *RingBuffer { return &RingBuffer{cap: cap} }

func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	return len(p), nil
}

func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := make([]byte, len(r.buf))
	copy(b, r.buf)
	return b
}

// Manager 会话管理器
type Manager struct {
	sessions map[string]*SessionInfo
	mu       sync.RWMutex
}

var defaultManager *Manager

func init() {
	defaultManager = &Manager{
		sessions: make(map[string]*SessionInfo),
	}
	// 启动定时清理（按真实活跃时间回收）
	go defaultManager.reaper()
}

// GetManager 获取全局会话管理器
func GetManager() *Manager {
	return defaultManager
}

// CreateSession 创建新会话：建立 SSH 连接 + 持久化 pty shell，并启动输出泵
func (m *Manager) CreateSession(assetID uint, assetName, host string, port int, config *gossh.ClientConfig, cols, rows int) (*SessionInfo, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := gossh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH连接失败: %w", err)
	}

	info := &SessionInfo{
		ID:         fmt.Sprintf("bastion-%d-%d", assetID, time.Now().UnixNano()),
		AssetID:    assetID,
		AssetName:  assetName,
		Client:     client,
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
		cols:       cols,
		rows:       rows,
		outBuf:     NewRingBuffer(256 * 1024), // 保留最近 256KB 输出用于回放
		done:       make(chan struct{}),
	}

	if err := info.startPty(cols, rows); err != nil {
		client.Close()
		return nil, err
	}

	m.mu.Lock()
	m.sessions[info.ID] = info
	m.mu.Unlock()

	// 启动输出泵：持续读取远端输出 → 缓冲 + 转发给已 attach 的 WS（即使 detach 也继续读，保证 shell 不阻塞）
	go info.pump()

	logger.Debug("bastion", "创建SSH会话", "session_id", info.ID, "asset", assetName, "host", host, "port", port)
	return info, nil
}

// startPty 在已建立的 SSH 连接上创建并启动持久化 shell（bash -li）
func (s *SessionInfo) startPty(cols, rows int) error {
	pty, err := s.Client.NewSession()
	if err != nil {
		return fmt.Errorf("创建SSH会话失败: %w", err)
	}
	modes := gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}
	if err := pty.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		pty.Close()
		return fmt.Errorf("创建PTY失败: %w", err)
	}
	stdin, err := pty.StdinPipe()
	if err != nil {
		pty.Close()
		return err
	}
	stdout, err := pty.StdoutPipe()
	if err != nil {
		pty.Close()
		return err
	}
	// 以交互模式启动 shell（readline 支持行编辑），缺失 bash 时回退 sh
	if err := pty.Start("bash -li || sh -i"); err != nil {
		pty.Close()
		return fmt.Errorf("启动Shell失败: %w", err)
	}
	s.pty = pty
	s.stdin = stdin
	s.stdout = stdout
	return nil
}

// pump 持续读取远端输出：写入环形缓冲（供回放），并转发给已 attach 的 WS。
// 即使当前无 WS 附着也继续读取，避免远端管道写满导致 shell 阻塞。
func (s *SessionInfo) pump() {
	buf := make([]byte, 4096)
	for {
		n, err := s.stdout.Read(buf)
		if n > 0 {
			s.outBuf.Write(buf[:n])
			s.UpdateAccess() // 真实输出 = 活跃
			s.writeWS(buf[:n])
		}
		if err != nil {
			break
		}
	}
	close(s.done)
	s.notifyError("远端Shell已退出")
}

// Attach 将 WebSocket 附着到会话：按需 resize，并回放已有缓冲以恢复屏幕内容。
// 单 viewer 模型：若已有其它 WS 附着，则顶掉（关闭）旧连接，避免多窗口共享同一 shell 导致输入输出错乱。
func (s *SessionInfo) Attach(ws *websocket.Conn, cols, rows int) {
	if cols > 0 && rows > 0 && (cols != s.cols || rows != s.rows) {
		s.cols, s.rows = cols, rows
		if s.pty != nil {
			_ = s.pty.WindowChange(rows, cols)
		}
	}
	s.attachedMu.Lock()
	old := s.attached
	if old != nil && old != ws {
		go func() { _ = old.Close() }() // 异步关闭旧连接，避免阻塞当前附着
	}
	s.attached = ws
	s.attachedMu.Unlock()

	// 回放缓冲，刷新后恢复之前的屏幕内容与历史输出
	data := s.outBuf.Bytes()
	if len(data) > 0 {
		s.wsMu.Lock()
		_ = ws.WriteMessage(websocket.BinaryMessage, data)
		s.wsMu.Unlock()
	}
}

// Detach 解除 WebSocket 附着（shell 不关闭，继续后台运行）
func (s *SessionInfo) Detach(ws *websocket.Conn) {
	s.attachedMu.Lock()
	if s.attached == ws {
		s.attached = nil
	}
	s.attachedMu.Unlock()
}

// WriteInput 将用户输入写入远端 shell（真实输入 = 活跃）
func (s *SessionInfo) WriteInput(data []byte) {
	if s.stdin != nil {
		_, _ = s.stdin.Write(data)
		s.UpdateAccess()
	}
}

// Resize 变更终端尺寸
func (s *SessionInfo) Resize(cols, rows int) {
	if cols > 0 && rows > 0 {
		s.cols, s.rows = cols, rows
		if s.pty != nil {
			_ = s.pty.WindowChange(rows, cols)
		}
	}
}

// writeWS 向已 attach 的 WS 转发二进制输出（无 WS 或写入失败则忽略，仅缓冲）
func (s *SessionInfo) writeWS(data []byte) {
	s.attachedMu.Lock()
	ws := s.attached
	s.attachedMu.Unlock()
	if ws == nil {
		return
	}
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	_ = ws.WriteMessage(websocket.BinaryMessage, data)
}

// notifyError 向已 attach 的 WS 发送错误文本消息
func (s *SessionInfo) notifyError(msg string) {
	s.attachedMu.Lock()
	ws := s.attached
	s.attachedMu.Unlock()
	if ws == nil {
		return
	}
	d, _ := json.Marshal(map[string]string{"type": "error", "msg": msg})
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	_ = ws.WriteMessage(websocket.TextMessage, d)
}

// Close 关闭会话：断开 WS、关闭 pty（终止 shell）、关闭 SSH 连接
func (s *SessionInfo) Close() {
	s.attachedMu.Lock()
	ws := s.attached
	s.attached = nil
	s.attachedMu.Unlock()

	if s.pty != nil {
		_ = s.pty.Close() // 关闭 pty → 远端 shell 收到 EOF/被终止，pump 读到错误退出
	}
	if ws != nil {
		s.wsMu.Lock()
		_ = ws.Close()
		s.wsMu.Unlock()
	}
	if s.Client != nil {
		_ = s.Client.Close()
	}
}

// GetSession 获取会话（重连/刷新复用同一 shell 时调用，算作一次活跃）
func (m *Manager) GetSession(sessionID string) *SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.sessions[sessionID]
	if ok {
		info.UpdateAccess()
	}
	return info
}

// CloseSession 关闭并移除会话
func (m *Manager) CloseSession(sessionID string) {
	m.mu.Lock()
	info, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if ok {
		info.Close()
		logger.Debug("bastion", "关闭SSH会话", "session_id", sessionID)
	}
}

// Count 返回当前活跃会话数
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// UpdateAccess 更新会话访问时间（仅真实终端 I/O 调用）
func (s *SessionInfo) UpdateAccess() {
	s.mu.Lock()
	s.LastAccess = time.Now()
	s.mu.Unlock()
}

// LastAccessTime 读取最后活跃时间
func (s *SessionInfo) LastAccessTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastAccess
}

// reaper 定时清理超时会话（仅按真实活跃时间判断；心跳/resize 不计入）
func (m *Manager) reaper() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.RLock()
		expired := make([]string, 0)
		for id, info := range m.sessions {
			if now.Sub(info.LastAccessTime()) > SessionTimeout {
				expired = append(expired, id)
			}
		}
		m.mu.RUnlock()

		for _, id := range expired {
			m.mu.Lock()
			info, ok := m.sessions[id]
			if ok {
				delete(m.sessions, id)
			}
			m.mu.Unlock()
			if ok {
				info.notifyError(fmt.Sprintf("会话因 %d 分钟无操作已自动断开", int(SessionTimeout.Minutes())))
				info.Close()
				logger.Warn("bastion", "超时回收SSH会话", "session_id", id)
			}
		}
	}
}
