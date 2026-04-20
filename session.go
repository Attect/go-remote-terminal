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

// ==================== Session ====================

// SessionStatus 会话状态枚举
type SessionStatus string

const (
	SessionActive SessionStatus = "active" // 进程运行中
	SessionExited SessionStatus = "exited" // 进程已退出
)

// Session 表示一个终端会话
type Session struct {
	ID        string             // 会话唯一标识，UUID格式
	Name      string             // 会话显示名称
	Pty       PtyProcess         // 关联的PTY进程
	CreatedAt time.Time          // 创建时间
	Status    SessionStatus      // 会话状态
	mu        sync.Mutex         // 会话级别锁
	outputBuf *RingBuffer        // 终端输出环形缓冲区
	writerWS  *WSConn            // 当前写入者WebSocket连接
	readerWS  []*WSConn          // 只读观察者WebSocket连接列表
	cancelFn  context.CancelFunc // PTY输出读取goroutine取消函数
}

// newSession 创建新会话（内部方法）
func newSession(id, name string, pty PtyProcess) *Session {
	return &Session{
		ID:        id,
		Name:      name,
		Pty:       pty,
		CreatedAt: time.Now(),
		Status:    SessionActive,
		outputBuf: NewRingBuffer(defaultRingBufferSize),
	}
}

// AttachWriter 将WebSocket绑定到会话的写入端
// 同一会话同一时刻只允许一个写入者
func (s *Session) AttachWriter(ws *WSConn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writerWS != nil {
		return ErrSessionBusy
	}

	if s.Status == SessionExited {
		return ErrSessionExpired
	}

	s.writerWS = ws
	return nil
}

// DetachWriter 解除WebSocket的写入绑定
func (s *Session) DetachWriter(ws *WSConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writerWS == ws {
		s.writerWS = nil
	}
}

// AttachReader 添加只读观察者
func (s *Session) AttachReader(ws *WSConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readerWS = append(s.readerWS, ws)
}

// DetachReader 移除只读观察者
func (s *Session) DetachReader(ws *WSConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, reader := range s.readerWS {
		if reader == ws {
			s.readerWS = append(s.readerWS[:i], s.readerWS[i+1:]...)
			break
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

	// 广播到所有WebSocket连接
	msg := NewOutputMessage(data)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writerWS != nil {
		if err := s.writerWS.WriteJSON(msg); err != nil {
			log.Printf("[Session %s] write to writer WS failed: %v", s.ID, err)
		}
	}

	for _, reader := range s.readerWS {
		if err := reader.WriteJSON(msg); err != nil {
			log.Printf("[Session %s] write to reader WS failed: %v", s.ID, err)
		}
	}
}

// GetOutput 获取输出缓冲区内容（用于重连回显和导出）
func (s *Session) GetOutput() []byte {
	return s.outputBuf.ReadAll()
}

// broadcastError 向所有连接广播错误消息
func (s *Session) broadcastError(code, message string) {
	msg := NewErrorMessage(code, message)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writerWS != nil {
		_ = s.writerWS.WriteJSON(msg)
	}
	for _, reader := range s.readerWS {
		_ = reader.WriteJSON(msg)
	}
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
	ErrSessionBusy     = &SessionError{Code: "SESSION_BUSY", Message: "session already has a writer"}
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
	if session.writerWS != nil {
		_ = session.writerWS.Close()
		session.writerWS = nil
	}
	for _, reader := range session.readerWS {
		_ = reader.Close()
	}
	session.readerWS = nil
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
			if session.writerWS != nil {
				_ = session.writerWS.Close()
				session.writerWS = nil
			}
			for _, reader := range session.readerWS {
				_ = reader.Close()
			}
			session.readerWS = nil
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
