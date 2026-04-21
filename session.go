package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ==================== RingBuffer ====================

// RingBuffer 环形缓冲区，用于存储终端输出
// 超过容量时自动覆盖最旧的数据，保证内存不无限增长
const defaultRingBufferSize = 1 * 1024 * 1024 // 1MB

type RingBuffer struct {
	buf   []byte     // 底层缓冲区
	size  int        // 缓冲区总容量
	start int        // 数据起始位置
	count int        // 当前数据量
	mu    sync.Mutex // 并发锁
}

// NewRingBuffer 创建环形缓冲区
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = defaultRingBufferSize
	}
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// Write 写入数据，超过容量时自动覆盖最旧数据
// 使用批量copy替代逐字节写入，提升大块数据写入效率
func (rb *RingBuffer) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 如果写入数据超过缓冲区容量，只保留最后size字节
	if len(data) >= rb.size {
		copy(rb.buf, data[len(data)-rb.size:])
		rb.start = 0
		rb.count = rb.size
		return len(data), nil
	}

	// 计算需要覆盖的旧数据量
	freeSpace := rb.size - rb.count
	if freeSpace < len(data) {
		// 需要覆盖旧数据，前移start
		overflow := len(data) - freeSpace
		rb.start = (rb.start + overflow) % rb.size
		rb.count = rb.size
	} else {
		rb.count += len(data)
	}

	// 批量写入：从写入位置开始，可能需要分两段copy（环绕）
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

// ReadAll 读取所有缓冲数据
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

// Reset 重置缓冲区
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.start = 0
	rb.count = 0
}

// ==================== WSConn ====================

// WSConn WebSocket连接封装，增加写保护
// gorilla/websocket的写操作非线程安全，需要加锁
type WSConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// NewWSConn 创建WebSocket连接封装
func NewWSConn(conn *websocket.Conn) *WSConn {
	return &WSConn{conn: conn}
}

// WriteMessage 线程安全地写入WebSocket消息
func (w *WSConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(messageType, data)
}

// WriteJSON 线程安全地写入JSON消息
func (w *WSConn) WriteJSON(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

// ReadMessage 读取消息
func (w *WSConn) ReadMessage() (int, []byte, error) {
	return w.conn.ReadMessage()
}

// Close 关闭连接
func (w *WSConn) Close() error {
	return w.conn.Close()
}

// ==================== ConnInfo ====================

// ConnInfo 记录每个WebSocket连接的信息
type ConnInfo struct {
	Rows uint16 // 终端行数
	Cols uint16 // 终端列数
}

// ==================== Session ====================

// SessionStatus 会话状态枚举
type SessionStatus string

const (
	SessionActive SessionStatus = "active" // 进程运行中
	SessionExited SessionStatus = "exited" // 进程已退出
)

// Session 表示一个终端会话
type Session struct {
	ID        string                // 会话唯一标识，UUID格式
	Name      string                // 会话显示名称
	Pty       PtyProcess            // 关联的PTY进程
	CreatedAt time.Time             // 创建时间
	Status    SessionStatus         // 会话状态
	mu        sync.Mutex            // 会话级别锁
	outputBuf *RingBuffer           // 终端输出环形缓冲区
	conns     map[*WSConn]*ConnInfo // 所有连接的WebSocket及其终端尺寸
	cancelFn  context.CancelFunc    // PTY输出读取goroutine取消函数
}

// newSession 创建新会话（内部方法）
func newSession(id, name string, ptyProc PtyProcess) *Session {
	return &Session{
		ID:        id,
		Name:      name,
		Pty:       ptyProc,
		CreatedAt: time.Now(),
		Status:    SessionActive,
		outputBuf: NewRingBuffer(defaultRingBufferSize),
		conns:     make(map[*WSConn]*ConnInfo),
	}
}

// AddConn 添加WebSocket连接到会话
// 返回当前PTY尺寸，用于通知新连接适配
func (s *Session) AddConn(ws *WSConn, rows, cols uint16) (ptyRows, ptyCols uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.conns[ws] = &ConnInfo{Rows: rows, Cols: cols}

	// 如果只有一个连接，直接将PTY调整到该连接的尺寸
	if len(s.conns) == 1 {
		if s.Pty != nil {
			_ = s.Pty.Resize(rows, cols)
		}
		return rows, cols
	}

	// 多个连接时，计算最小尺寸
	minRows, minCols := s.calcMinSizeLocked()
	if s.Pty != nil {
		_ = s.Pty.Resize(minRows, minCols)
	}
	return minRows, minCols
}

// RemoveConn 移除WebSocket连接
// 返回是否需要通知其他连接（PTY尺寸可能变更）
func (s *Session) RemoveConn(ws *WSConn) (newPtyRows, newPtyCols uint16, shouldNotify bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conns, ws)

	if len(s.conns) == 0 {
		return 0, 0, false
	}

	// 连接移除后，重新计算最小尺寸
	minRows, minCols := s.calcMinSizeLocked()
	if s.Pty != nil {
		_ = s.Pty.Resize(minRows, minCols)
	}
	return minRows, minCols, true
}

// UpdateConnSize 更新指定连接的终端尺寸
// 返回PTY是否需要调整尺寸，以及新的PTY尺寸
func (s *Session) UpdateConnSize(ws *WSConn, rows, cols uint16) (newPtyRows, newPtyCols uint16, ptyChanged bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.conns[ws]
	if !ok {
		// 连接不在会话中，先添加
		s.conns[ws] = &ConnInfo{Rows: rows, Cols: cols}
	} else {
		info.Rows = rows
		info.Cols = cols
	}

	// 重新计算最小尺寸
	minRows, minCols := s.calcMinSizeLocked()

	// 检查PTY尺寸是否需要变更
	var curRows, curCols uint16 = 24, 80 // 默认值
	if len(s.conns) > 0 {
		curRows = minRows
		curCols = minCols
	}

	if s.Pty != nil {
		_ = s.Pty.Resize(minRows, minCols)
	}

	return curRows, curCols, true
}

// calcMinSizeLocked 计算所有连接中的最小终端尺寸（调用方需持有锁）
func (s *Session) calcMinSizeLocked() (minRows, minCols uint16) {
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
	// 兜底：确保最小值合理
	if minRows == 0 {
		minRows = 24
	}
	if minCols == 0 {
		minCols = 80
	}
	return
}

// BroadcastMessage 向所有连接广播消息
// 先在锁内收集连接列表，再在锁外逐个发送，避免持锁写WS导致阻塞
func (s *Session) BroadcastMessage(msg WSMessage) {
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

// WriteOutput 向会话的输出缓冲区写入数据，同时推送到所有已连接的WebSocket
func (s *Session) WriteOutput(data []byte) {
	if len(data) == 0 {
		return
	}

	// 写入环形缓冲区
	s.outputBuf.Write(data)

	// 构建消息
	msg := NewOutputMessage(data)

	// 在锁内收集连接列表，锁外发送，避免持锁写WS阻塞
	s.mu.Lock()
	conns := make([]*WSConn, 0, len(s.conns))
	for ws := range s.conns {
		conns = append(conns, ws)
	}
	s.mu.Unlock()

	for _, ws := range conns {
		if err := ws.WriteJSON(msg); err != nil {
			log.Printf("[Session %s] write to WS failed: %v", s.ID, err)
		}
	}
}

// GetOutput 获取输出缓冲区内容（用于重连回显和导出）
func (s *Session) GetOutput() []byte {
	return s.outputBuf.ReadAll()
}

// broadcastError 向所有连接广播错误消息
func (s *Session) broadcastError(code, message string) {
	s.BroadcastMessage(NewErrorMessage(code, message))
}

// startOutputReader 启动PTY输出读取goroutine
// 该goroutine在Session创建时启动，WebSocket断开后继续运行
func (s *Session) startOutputReader(ctx context.Context) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := s.Pty.Read(buf)
		if err != nil {
			// PTY进程退出或读取错误
			s.mu.Lock()
			s.Status = SessionExited
			s.mu.Unlock()

			log.Printf("[Session %s] PTY read ended: %v", s.ID, err)

			// 通知所有已连接的WebSocket
			s.broadcastError("SESSION_EXITED", "Shell process has exited")
			return
		}
		if n > 0 {
			s.WriteOutput(buf[:n])
		}
	}
}

// ConnCount 返回当前连接数
func (s *Session) ConnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
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
)

// ==================== SessionPool ====================

// SessionPool 管理所有活跃会话
type SessionPool struct {
	sessions  sync.Map // map[sessionID]*Session
	counter   int64    // 会话计数器，用于生成默认名称
	counterMu sync.Mutex
}

// NewSessionPool 创建会话池
func NewSessionPool() *SessionPool {
	return &SessionPool{}
}

// nextSessionName 生成默认会话名称
func (p *SessionPool) nextSessionName() string {
	p.counterMu.Lock()
	defer p.counterMu.Unlock()
	p.counter++
	return fmt.Sprintf("终端 %d", p.counter)
}

// Create 创建新会话
func (p *SessionPool) Create(name, shellPath string) (*Session, error) {
	// 确定会话名称
	if name == "" {
		name = p.nextSessionName()
	}
	// 验证名称长度
	if len(name) > 50 {
		return nil, ErrInvalidName
	}

	// 检测Shell
	shellConfig, err := DetectShellWithOverride(shellPath)
	if err != nil {
		log.Printf("[SessionPool] shell detection warning: %v", err)
		// 即使有警告，也使用回退的默认Shell
		if shellConfig == nil {
			return nil, &SessionError{Code: "SHELL_NOT_FOUND", Message: err.Error()}
		}
	}

	// 创建PTY进程
	ptyProc := NewPtyProcess()
	if err := ptyProc.Start(shellConfig.Path, shellConfig.Args, 24, 80); err != nil {
		return nil, &SessionError{
			Code:    "SHELL_START_FAILED",
			Message: fmt.Sprintf("failed to start shell: %v", err),
		}
	}

	// 生成会话ID
	sessionID := generateSessionID()

	// 创建会话
	session := newSession(sessionID, name, ptyProc)

	// 启动PTY输出读取goroutine
	ctx, cancel := context.WithCancel(context.Background())
	session.cancelFn = cancel
	go session.startOutputReader(ctx)

	// 存入会话池
	p.sessions.Store(sessionID, session)

	log.Printf("[SessionPool] created session %s (name=%s, shell=%s, pid=%d)",
		sessionID, name, shellConfig.Path, ptyProc.Pid())

	return session, nil
}

// Get 获取指定会话
func (p *SessionPool) Get(id string) (*Session, bool) {
	val, ok := p.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Session), true
}

// List 获取所有活跃会话列表
func (p *SessionPool) List() []*Session {
	var list []*Session
	p.sessions.Range(func(key, value interface{}) bool {
		list = append(list, value.(*Session))
		return true
	})
	return list
}

// Close 关闭指定会话，终止子进程并释放资源
func (p *SessionPool) Close(id string) error {
	val, ok := p.sessions.Load(id)
	if !ok {
		return ErrSessionNotFound
	}

	session := val.(*Session)

	// 取消输出读取goroutine
	if session.cancelFn != nil {
		session.cancelFn()
	}

	// 关闭PTY进程
	if session.Pty != nil {
		if err := session.Pty.Close(); err != nil {
			log.Printf("[SessionPool] close PTY for session %s failed: %v", id, err)
		}
	}

	// 关闭所有WebSocket连接
	session.mu.Lock()
	for ws := range session.conns {
		_ = ws.Close()
	}
	session.conns = make(map[*WSConn]*ConnInfo)
	session.mu.Unlock()

	// 从池中移除
	p.sessions.Delete(id)

	log.Printf("[SessionPool] closed session %s", id)
	return nil
}

// Rename 重命名会话
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
	defer session.mu.Unlock()
	session.Name = newName

	log.Printf("[SessionPool] renamed session %s to %q", id, newName)
	return nil
}

// Cleanup 清理已退出会话的资源
func (p *SessionPool) Cleanup() {
	p.sessions.Range(func(key, value interface{}) bool {
		session := value.(*Session)
		if session.Status == SessionExited {
			// 取消输出读取goroutine
			if session.cancelFn != nil {
				session.cancelFn()
			}
			// 关闭PTY
			if session.Pty != nil {
				_ = session.Pty.Close()
			}
			// 关闭所有WebSocket
			session.mu.Lock()
			for ws := range session.conns {
				_ = ws.Close()
			}
			session.conns = make(map[*WSConn]*ConnInfo)
			session.mu.Unlock()

			p.sessions.Delete(key)
			log.Printf("[SessionPool] cleaned up exited session %s", key)
		}
		return true
	})
}

// ==================== 辅助函数 ====================

// generateSessionID 生成会话ID
func generateSessionID() string {
	// 使用crypto/rand生成UUID v4
	return generateUUID()
}

// generateUUID 生成UUID格式的会话ID
func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 回退：使用时间戳
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	// 设置版本4和变体位
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
