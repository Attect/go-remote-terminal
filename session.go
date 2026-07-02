package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ==================== 常量 ====================

const (
	defaultRingBufferSize = 1 * 1024 * 1024 // 1MB
	MaxSessions           = 50               // 最大活跃会话数
	SessionIdleTimeout    = 7 * 24 * time.Hour // 无连接会话超时
)

// ==================== RingBuffer ====================

// RingBuffer 环形缓冲区，用于存储终端输出
type RingBuffer struct {
	buf   []byte
	size  int
	start int
	count int
	mu    sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = defaultRingBufferSize
	}
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

func (rb *RingBuffer) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(data) >= rb.size {
		copy(rb.buf, data[len(data)-rb.size:])
		rb.start = 0
		rb.count = rb.size
		return len(data), nil
	}
	freeSpace := rb.size - rb.count
	if freeSpace < len(data) {
		overflow := len(data) - freeSpace
		rb.start = (rb.start + overflow) % rb.size
		rb.count = rb.size
	} else {
		rb.count += len(data)
	}
	writePos := (rb.start + rb.count - len(data)) % rb.size
	firstChunk := rb.size - writePos
	if firstChunk > len(data) {
		firstChunk = len(data)
	}
	copy(rb.buf[writePos:], data[:firstChunk])
	if firstChunk < len(data) {
		copy(rb.buf[0:], data[firstChunk:])
	}
	return len(data), nil
}

func (rb *RingBuffer) ReadAll() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.count == 0 {
		return nil
	}
	result := make([]byte, rb.count)
	for i := 0; i < rb.count; i++ {
		result[i] = rb.buf[(rb.start+i)%rb.size]
	}
	return result
}

func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.start = 0
	rb.count = 0
}

// ==================== WSConn ====================

type WSConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewWSConn(conn *websocket.Conn) *WSConn {
	return &WSConn{conn: conn}
}

func (w *WSConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return w.conn.WriteMessage(messageType, data)
}

func (w *WSConn) WriteJSON(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return w.conn.WriteJSON(v)
}

func (w *WSConn) ReadMessage() (int, []byte, error) {
	return w.conn.ReadMessage()
}

func (w *WSConn) Close() error {
	return w.conn.Close()
}

// ==================== ConnInfo ====================

type ConnInfo struct {
	Rows     uint16 // 终端行数
	Cols     uint16 // 终端列数
	Name     string // 连接者名称
	Color    string // 连接者颜色
	Focus    bool   // 是否拥有输入焦点
	ReadOnly bool   // 是否为只读连接
}

// ==================== Session ====================

type SessionStatus string

const (
	SessionActive SessionStatus = "active"
	SessionExited SessionStatus = "exited"
)

var connNames = []string{"红", "橙", "黄", "绿", "青", "蓝", "紫", "粉"}
var connColors = []string{"#e94560", "#ff9800", "#ffd740", "#4caf50", "#00d4ff", "#2196f3", "#e040fb", "#ea80fc"}

func generateConnIdentity() (string, string) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "用户-????", "#e94560"
	}
	idx := int(b[0]) % len(connNames)
	suffix := hex.EncodeToString(b)
	return "用户-" + suffix[:4], connColors[idx]
}

type Session struct {
	ID           string
	Name         string
	Pty          PtyProcess
	CreatedAt    time.Time
	LastConnTime time.Time          // 最后一次有连接的时间
	Status       SessionStatus
	mu           sync.Mutex
	outputBuf    *RingBuffer
	conns        map[*WSConn]*ConnInfo
	cancelFn     context.CancelFunc
	ptyRows      uint16
	ptyCols      uint16
	FocusConn    *WSConn // 当前拥有输入焦点的连接

	// MCP 相关字段
	FixedRows uint16    // MCP终端固定行数，0表示动态
	FixedCols uint16    // MCP终端固定列数，0表示动态
	Opener    string    // 打开者名称（MCP）
	Purpose   string    // 打开目的（MCP）
	vtScreen  *VTScreen // 虚拟终端屏幕（非nil时启用VT模拟）

	// 输出静默检测相关
	outMu          sync.Mutex
	lastOutputTime time.Time
}

func newSession(id, name string, ptyProc PtyProcess, rows, cols uint16) *Session {
	now := time.Now()
	if rows == 0 || cols == 0 {
		rows = 24
		cols = 80
	}
	return &Session{
		ID:             id,
		Name:           name,
		Pty:            ptyProc,
		CreatedAt:      now,
		LastConnTime:   now,
		Status:         SessionActive,
		outputBuf:      NewRingBuffer(defaultRingBufferSize),
		conns:          make(map[*WSConn]*ConnInfo),
		ptyRows:        rows,
		ptyCols:        cols,
		lastOutputTime: now,
	}
}

func (s *Session) AddConn(ws *WSConn, rows, cols uint16, readOnly bool) (ptyRows, ptyCols uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name, color := generateConnIdentity()
	s.conns[ws] = &ConnInfo{
		Rows:     rows,
		Cols:     cols,
		Name:     name,
		Color:    color,
		ReadOnly: readOnly,
	}
	s.LastConnTime = time.Now()

	// 如果没有主控且此连接不是只读，自动成为主控
	if s.FocusConn == nil && !readOnly {
		s.FocusConn = ws
		s.conns[ws].Focus = true
	}

	minRows, minCols := s.calcMinSizeLocked()
	if s.Pty != nil {
		_ = s.Pty.Resize(minRows, minCols)
	}
	s.ptyRows = minRows
	s.ptyCols = minCols
	return minRows, minCols
}

func (s *Session) RemoveConn(ws *WSConn) (newPtyRows, newPtyCols uint16, shouldNotify bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conns, ws)

	// 如果移除的是主控，尝试转移焦点给下一个非只读连接
	if s.FocusConn == ws {
		s.FocusConn = nil
		for w, info := range s.conns {
			if !info.ReadOnly {
				s.FocusConn = w
				info.Focus = true
				break
			}
		}
	}

	if len(s.conns) == 0 {
		return 0, 0, false
	}

	minRows, minCols := s.calcMinSizeLocked()
	if minRows == s.ptyRows && minCols == s.ptyCols {
		return minRows, minCols, false
	}

	if s.Pty != nil {
		_ = s.Pty.Resize(minRows, minCols)
	}
	s.ptyRows = minRows
	s.ptyCols = minCols
	return minRows, minCols, true
}

func (s *Session) UpdateConnSize(ws *WSConn, rows, cols uint16) (newPtyRows, newPtyCols uint16, ptyChanged bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.conns[ws]
	if !ok {
		s.conns[ws] = &ConnInfo{Rows: rows, Cols: cols}
	} else {
		info.Rows = rows
		info.Cols = cols
	}

	minRows, minCols := s.calcMinSizeLocked()
	if minRows == s.ptyRows && minCols == s.ptyCols {
		// 固定尺寸终端：即使 PTY 尺寸未变，若客户端请求尺寸与固定尺寸不一致，
		// 仍需通知客户端同步，避免前端与 PTY 尺寸不匹配导致渲染错位
		if s.FixedRows > 0 && s.FixedCols > 0 && (rows != minRows || cols != minCols) {
			return minRows, minCols, true
		}
		return minRows, minCols, false
	}

	if s.Pty != nil {
		_ = s.Pty.Resize(minRows, minCols)
	}
	s.ptyRows = minRows
	s.ptyCols = minCols
	return minRows, minCols, true
}

func (s *Session) calcMinSizeLocked() (minRows, minCols uint16) {
	// 固定尺寸终端不受WebSocket连接影响
	if s.FixedRows > 0 && s.FixedCols > 0 {
		return s.FixedRows, s.FixedCols
	}
	minRows = 0
	minCols = 0
	for _, info := range s.conns {
		if minRows == 0 || info.Rows < minRows {
			minRows = info.Rows
		}
		if minCols == 0 || info.Cols < minCols {
			minCols = info.Cols
		}
	}
	if minRows == 0 {
		minRows = 24
	}
	if minCols == 0 {
		minCols = 80
	}
	return
}

func (s *Session) BroadcastMessage(msg V1Message) {
	s.mu.Lock()
	conns := make([]*WSConn, 0, len(s.conns))
	for ws := range s.conns {
		conns = append(conns, ws)
	}
	s.mu.Unlock()

	for _, ws := range conns {
		if err := ws.WriteJSON(msg); err != nil {
			log.Printf("[Session %s] broadcast to WS failed: %v", s.ID, err)
		}
	}
}

func (s *Session) WriteOutput(data []byte) {
	if len(data) == 0 {
		return
	}
	_, _ = s.outputBuf.Write(data)

	// 更新最后输出时间，供MCP wait模式检测静默
	s.outMu.Lock()
	s.lastOutputTime = time.Now()
	s.outMu.Unlock()

	// 同时更新虚拟终端屏幕
	if s.vtScreen != nil {
		s.vtScreen.Feed(data)
	}

	frame := EncodeBinaryFrame(BinaryTypeOutput, data)

	s.mu.Lock()
	conns := make([]*WSConn, 0, len(s.conns))
	for ws := range s.conns {
		conns = append(conns, ws)
	}
	s.mu.Unlock()

	for _, ws := range conns {
		go func(conn *WSConn) {
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				log.Printf("[Session %s] write binary to WS failed: %v", s.ID, err)
			}
		}(ws)
	}
}

func (s *Session) GetOutput() []byte {
	return s.outputBuf.ReadAll()
}

func (s *Session) broadcastError(code, message string) {
	s.BroadcastMessage(NewErrorMessage(code, message))
}

func (s *Session) connDTOsLocked() []ConnDTO {
	dtos := make([]ConnDTO, 0, len(s.conns))
	for _, info := range s.conns {
		dtos = append(dtos, ConnDTO{
			Name:  info.Name,
			Color: info.Color,
			Focus: info.Focus,
		})
	}
	return dtos
}

func (s *Session) BroadcastConnList() {
	s.BroadcastMessage(NewConnListMessage(s))
}

func (s *Session) BroadcastFocusChange() {
	s.mu.Lock()
	name := ""
	if s.FocusConn != nil {
		if info, ok := s.conns[s.FocusConn]; ok {
			name = info.Name
		}
	}
	s.mu.Unlock()
	s.BroadcastMessage(NewFocusChangeMessage(name))
}

func (s *Session) broadcastSessionInfo() {
	s.mu.Lock()
	conns := make(map[*WSConn]*ConnInfo)
	for ws, info := range s.conns {
		conns[ws] = info
	}
	s.mu.Unlock()

	for ws, info := range conns {
		msg := NewSessionInfoMessage(s, ws, info.ReadOnly)
		_ = ws.WriteJSON(msg)
	}
}

func (s *Session) TakeFocus(ws *WSConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.conns[ws]
	if !ok || info.ReadOnly {
		return false
	}

	if s.FocusConn != nil && s.FocusConn != ws {
		if oldInfo, ok := s.conns[s.FocusConn]; ok {
			oldInfo.Focus = false
		}
	}

	s.FocusConn = ws
	info.Focus = true
	return true
}

func (s *Session) ReleaseFocus(ws *WSConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.FocusConn != ws {
		return
	}

	s.FocusConn = nil
	if info, ok := s.conns[ws]; ok {
		info.Focus = false
	}
}

func (s *Session) CanInput(ws *WSConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.conns[ws]
	if !ok || info.ReadOnly {
		return false
	}
	return s.FocusConn == ws
}

func (s *Session) ConnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// MCPWrite 直接写入PTY，不经过焦点检查，供MCP Agent使用
// 使用循环写入确保所有数据完整送达，避免底层缓冲区限制导致的长命令截断。
// Windows ConPTY 下分批写入并添加短暂延迟，避免快速连续写入导致输入竞争和显示混乱。
func (s *Session) MCPWrite(data []byte) error {
	s.mu.Lock()
	pty := s.Pty
	s.mu.Unlock()
	if pty == nil {
		return fmt.Errorf("PTY not available")
	}
	const chunkSize = 64
	for len(data) > 0 {
		chunk := data
		if len(chunk) > chunkSize {
			chunk = chunk[:chunkSize]
		}
		n, err := pty.Write(chunk)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("PTY write returned 0 bytes")
		}
		data = data[n:]
		// Windows ConPTY 在快速连续写入时可能出现输入竞争，添加微小延迟给 PTY 处理时间
		if runtime.GOOS == "windows" && len(data) > 0 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	return nil
}

// MCPWriteSubmit 发送数据后追加回车执行。
// Windows ConPTY + PowerShell 下，先发送命令内容，等待 PSReadLine 完成语法高亮处理，
// 再发送 \r，避免快速连续写入导致的显示残留（如多余的 m）和 \r\n 导致的多行提示符（>>）。
func (s *Session) MCPWriteSubmit(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	// 分离命令内容和行尾（如果 data 末尾有 \r 或 \n）
	content := data
	for len(content) > 0 && (content[len(content)-1] == '\r' || content[len(content)-1] == '\n') {
		content = content[:len(content)-1]
	}
	// 发送命令内容
	if len(content) > 0 {
		if err := s.MCPWrite(content); err != nil {
			return err
		}
	}
	// Windows 下给 PowerShell/PSReadLine 处理输入和语法高亮的时间
	if runtime.GOOS == "windows" {
		time.Sleep(50 * time.Millisecond)
	}
	// 发送回车
	return s.MCPWrite([]byte{'\r'})
}

// WaitForIdle 等待PTY输出静默，返回期间累积的输出和是否成功完成
// idleThreshold: 连续无输出的静默阈值
// maxWait: 最大等待时间
// 核心改进：只有在检测到输出至少有一次增长后，才开始用 idleThreshold 判断完成。
// 这避免了 Shell 启动慢时，WaitForIdle 在命令输出出现前就提前返回。
func (s *Session) WaitForIdle(idleThreshold, maxWait time.Duration) ([]byte, bool) {
	start := time.Now()
	deadline := start.Add(maxWait)

	initialOutput := s.GetOutput()
	hasGrown := false

	// 先给命令一个最小启动窗口
	minInitialWait := 100 * time.Millisecond
	if time.Now().Add(minInitialWait).Before(deadline) {
		time.Sleep(minInitialWait)
	}

	pollInterval := 50 * time.Millisecond
	if idleThreshold < pollInterval {
		pollInterval = idleThreshold / 4
		if pollInterval < 10*time.Millisecond {
			pollInterval = 10 * time.Millisecond
		}
	}

	for {
		now := time.Now()
		if now.After(deadline) {
			return s.GetOutput(), false
		}

		currentOutput := s.GetOutput()
		if !hasGrown && len(currentOutput) > len(initialOutput) {
			hasGrown = true
		}

		s.outMu.Lock()
		lastOut := s.lastOutputTime
		s.outMu.Unlock()

		idle := now.Sub(lastOut)

		if hasGrown {
			// 已有输出增长，使用 idle 阈值判断完成
			if idle >= idleThreshold {
				// 只检查新增输出的最后一行是否像 shell 提示符
				// 避免历史输出中的旧提示符导致误判，也避免长时间静默命令（如 Start-Sleep）被提前认为已完成
				newOutput := currentOutput
				if len(currentOutput) > len(initialOutput) {
					newOutput = currentOutput[len(initialOutput):]
				}
				if lastLineLooksLikePrompt(string(CleanTerminalOutput(newOutput))) {
					return currentOutput, true
				}
				// 不像提示符，继续等待直到 maxWait 超时
				// 不再使用 3*idleThreshold 作为 fallback，避免误判长时间静默命令
			}
		} else {
			// 一直没有输出增长，使用较长的无增长容忍时间（5秒）
			// 给慢启动的 Shell（如 PowerShell）充分时间，也适用于长时间静默命令
			if now.Sub(start) >= 5*time.Second {
				return currentOutput, true
			}
		}

		// 睡眠策略：有增长后按 idle 预计时间睡，无增长时快速轮询
		var sleepUntil time.Time
		if hasGrown {
			sleepUntil = lastOut.Add(idleThreshold)
		} else {
			sleepUntil = now.Add(200 * time.Millisecond)
		}
		if sleepUntil.After(deadline) {
			sleepUntil = deadline
		}
		remaining := sleepUntil.Sub(now)
		if remaining > pollInterval {
			remaining = pollInterval
		}
		if remaining > 0 {
			time.Sleep(remaining)
		} else {
			time.Sleep(pollInterval)
		}
	}
}

// ExtractOutputSince 从当前outputBuf中截取自mark以来的新增输出
// 如果mark已被RingBuffer覆盖，则返回全部当前输出，ok=false
func (s *Session) ExtractOutputSince(mark []byte) (output []byte, ok bool) {
	all := s.GetOutput()
	if len(all) > len(mark) && bytes.Equal(all[:len(mark)], mark) {
		return all[len(mark):], true
	}
	return all, false
}

// lastLineLooksLikePrompt 检查文本最后一行是否像 shell 提示符
// 支持 PowerShell(PS C:\>)、bash(user@host:~$)、cmd(C:\>)、root(#) 等
func lastLineLooksLikePrompt(text string) bool {
	lines := strings.Split(text, "\n")
	// 从后往前找第一个非空行
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// 常见提示符结尾：> (PowerShell/cmd)、$ (bash)、# (root)、% (csh/zsh)
		if strings.HasSuffix(line, ">") || strings.HasSuffix(line, "$") || strings.HasSuffix(line, "#") || strings.HasSuffix(line, "%") {
			// 启发式：真正的提示符通常包含路径分隔符、@、:、~ 等，或者是很短的单字符提示符
			// 避免将普通文本（如 echo "a > b"）误判为提示符
			if strings.ContainsAny(line, "\\/:@~") || strings.HasPrefix(line, "PS ") || len(line) < 10 {
				return true
			}
		}
		return false
	}
	return false
}

func (s *Session) startOutputReader(ctx context.Context) {
	readCh := make(chan []byte, 16)
	errCh := make(chan error, 1)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.Pty.Read(buf)
			if err != nil {
				errCh <- err
				return
			}
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case readCh <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	var batch []byte
	flush := func() {
		if len(batch) > 0 {
			s.WriteOutput(batch)
			batch = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-ticker.C:
			flush()
		case data := <-readCh:
			batch = append(batch, data...)
		case err := <-errCh:
			s.mu.Lock()
			s.Status = SessionExited
			s.mu.Unlock()
			log.Printf("[Session %s] PTY read ended: %v", s.ID, err)
			flush()
			s.broadcastError("SESSION_EXITED", "Shell process has exited")
			go func() { _, _ = s.Pty.Wait() }()
			return
		}
	}
}

// ==================== 错误定义 ====================

type SessionError struct {
	Code    string
	Message string
}

func (e *SessionError) Error() string {
	return e.Message
}

var (
	ErrSessionNotFound = &SessionError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	ErrSessionExpired  = &SessionError{Code: "SESSION_EXPIRED", Message: "session process has exited"}
	ErrInvalidName     = &SessionError{Code: "INVALID_SESSION_NAME", Message: "invalid session name"}
	ErrSessionLimit    = &SessionError{Code: "SESSION_LIMIT", Message: "maximum number of sessions reached"}
)

// ==================== SessionPool ====================

type SessionPool struct {
	sessions  sync.Map
	counter   int64
	counterMu sync.Mutex
}

func NewSessionPool() *SessionPool {
	return &SessionPool{}
}

// BroadcastSessionsChanged 向所有会话的所有WebSocket连接广播会话列表变更通知
func (p *SessionPool) BroadcastSessionsChanged() {
	msg := NewSessionsChangedMessage()
	p.sessions.Range(func(_, value interface{}) bool {
		session := value.(*Session)
		session.BroadcastMessage(msg)
		return true
	})
}

func (p *SessionPool) nextSessionName() string {
	p.counterMu.Lock()
	defer p.counterMu.Unlock()
	p.counter++
	return fmt.Sprintf("终端 %d", p.counter)
}

func (p *SessionPool) sessionCount() int {
	count := 0
	p.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

func (p *SessionPool) closeOldestSession() {
	var oldest *Session
	p.sessions.Range(func(_, value interface{}) bool {
		session := value.(*Session)
		if oldest == nil || session.CreatedAt.Before(oldest.CreatedAt) {
			oldest = session
		}
		return true
	})
	if oldest != nil {
		_ = p.Close(oldest.ID)
	}
}

func (p *SessionPool) Create(name, shellPath string) (*Session, error) {
	return p.CreateWithMeta(name, shellPath, "", "", 0, 0, "")
}

func (p *SessionPool) CreateWithMeta(name, shellPath, opener, purpose string, rows, cols uint16, workDir string) (*Session, error) {
	if name == "" {
		name = p.nextSessionName()
	}
	if len(name) > 50 {
		return nil, ErrInvalidName
	}

	// 检查会话上限
	if p.sessionCount() >= MaxSessions {
		p.closeOldestSession()
	}

	shellConfig, err := DetectShellWithOverride(shellPath)
	if err != nil {
		log.Printf("[SessionPool] shell detection warning: %v", err)
		if shellConfig == nil {
			return nil, &SessionError{Code: "SHELL_NOT_FOUND", Message: err.Error()}
		}
	}

	// 确定PTY尺寸
	ptyRows, ptyCols := rows, cols
	if ptyRows == 0 || ptyCols == 0 {
		ptyRows, ptyCols = 24, 80
	}

	ptyProc := NewPtyProcess()
	if err := ptyProc.Start(shellConfig.Path, shellConfig.Args, ptyRows, ptyCols, workDir); err != nil {
		return nil, &SessionError{
			Code:    "SHELL_START_FAILED",
			Message: fmt.Sprintf("failed to start shell: %v", err),
		}
	}

	sessionID := generateSessionID()
	session := newSession(sessionID, name, ptyProc, rows, cols)
	session.Opener = opener
	session.Purpose = purpose

	// 固定尺寸终端
	if rows > 0 && cols > 0 {
		session.FixedRows = rows
		session.FixedCols = cols
		session.vtScreen = NewVTScreen(int(rows), int(cols))
	}

	ctx, cancel := context.WithCancel(context.Background())
	session.cancelFn = cancel
	go session.startOutputReader(ctx)

	p.sessions.Store(sessionID, session)

	// 广播会话列表变更通知
	p.BroadcastSessionsChanged()

	log.Printf("[SessionPool] created session %s (name=%s, shell=%s, pid=%d, fixed=%v)",
		sessionID, name, shellConfig.Path, ptyProc.Pid(), rows > 0 && cols > 0)

	return session, nil
}

func (p *SessionPool) Get(id string) (*Session, bool) {
	val, ok := p.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Session), true
}

func (p *SessionPool) List() []*Session {
	var list []*Session
	p.sessions.Range(func(key, value interface{}) bool {
		list = append(list, value.(*Session))
		return true
	})
	return list
}

func (p *SessionPool) Close(id string) error {
	val, ok := p.sessions.Load(id)
	if !ok {
		return ErrSessionNotFound
	}

	session := val.(*Session)

	if session.cancelFn != nil {
		session.cancelFn()
	}

	if session.Pty != nil {
		if err := session.Pty.Close(); err != nil {
			log.Printf("[SessionPool] close PTY for session %s failed: %v", id, err)
		}
	}

	session.broadcastError("SESSION_CLOSED", "session has been closed by another client")

	session.mu.Lock()
	for ws := range session.conns {
		_ = ws.Close()
	}
	session.conns = make(map[*WSConn]*ConnInfo)
	session.mu.Unlock()

	p.sessions.Delete(id)

	// 广播会话列表变更通知
	p.BroadcastSessionsChanged()

	log.Printf("[SessionPool] closed session %s", id)
	return nil
}

func (p *SessionPool) Rename(id, newName string) error {
	val, ok := p.sessions.Load(id)
	if !ok {
		return ErrSessionNotFound
	}

	if len(newName) == 0 || len(newName) > 50 {
		return ErrInvalidName
	}

	session := val.(*Session)
	session.mu.Lock()
	session.Name = newName
	session.mu.Unlock()

	// 广播会话列表变更通知
	p.BroadcastSessionsChanged()

	log.Printf("[SessionPool] renamed session %s to %q", id, newName)
	return nil
}

func (p *SessionPool) Cleanup() {
	p.sessions.Range(func(key, value interface{}) bool {
		session := value.(*Session)
		shouldClean := false

		session.mu.Lock()
		if session.Status == SessionExited {
			shouldClean = true
		} else if len(session.conns) == 0 && time.Since(session.LastConnTime) > SessionIdleTimeout {
			shouldClean = true
		}
		session.mu.Unlock()

		if shouldClean {
			if session.cancelFn != nil {
				session.cancelFn()
			}
			if session.Pty != nil {
				_ = session.Pty.Close()
			}
			session.mu.Lock()
			for ws := range session.conns {
				_ = ws.Close()
			}
			session.conns = make(map[*WSConn]*ConnInfo)
			session.mu.Unlock()

			p.sessions.Delete(key)
			log.Printf("[SessionPool] cleaned up session %s", key)
		}
		return true
	})
}

var (
	sessionIDCounter int64
	sessionIDMu      sync.Mutex
)

func generateSessionID() string {
	sessionIDMu.Lock()
	defer sessionIDMu.Unlock()
	sessionIDCounter++
	return fmt.Sprintf("%06d", sessionIDCounter)
}
