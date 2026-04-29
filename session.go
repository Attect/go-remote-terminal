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
	return w.conn.WriteMessage(messageType, data)
}

func (w *WSConn) WriteJSON(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
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
}

func newSession(id, name string, ptyProc PtyProcess) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		Name:         name,
		Pty:          ptyProc,
		CreatedAt:    now,
		LastConnTime: now,
		Status:       SessionActive,
		outputBuf:    NewRingBuffer(defaultRingBufferSize),
		conns:        make(map[*WSConn]*ConnInfo),
		ptyRows:      24,
		ptyCols:      80,
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
	frame := EncodeBinaryFrame(BinaryTypeOutput, data)

	s.mu.Lock()
	conns := make([]*WSConn, 0, len(s.conns))
	for ws := range s.conns {
		conns = append(conns, ws)
	}
	s.mu.Unlock()

	for _, ws := range conns {
		if err := ws.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			log.Printf("[Session %s] write binary to WS failed: %v", s.ID, err)
		}
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

	ptyProc := NewPtyProcess()
	if err := ptyProc.Start(shellConfig.Path, shellConfig.Args, 24, 80); err != nil {
		return nil, &SessionError{
			Code:    "SHELL_START_FAILED",
			Message: fmt.Sprintf("failed to start shell: %v", err),
		}
	}

	sessionID := generateSessionID()
	session := newSession(sessionID, name, ptyProc)

	ctx, cancel := context.WithCancel(context.Background())
	session.cancelFn = cancel
	go session.startOutputReader(ctx)

	p.sessions.Store(sessionID, session)

	log.Printf("[SessionPool] created session %s (name=%s, shell=%s, pid=%d)",
		sessionID, name, shellConfig.Path, ptyProc.Pid())

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
	defer session.mu.Unlock()
	session.Name = newName

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

func generateSessionID() string {
	return generateUUID()
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
