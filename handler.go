package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ==================== API响应结构 ====================

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type SessionDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	ConnCount int    `json:"conn_count"`
}

type CreateSessionRequest struct {
	Name  *string `json:"name"`
	Shell *string `json:"shell"`
}

type RenameSessionRequest struct {
	Name string `json:"name"`
}

const (
	CodeSuccess       = 0
	CodeBadRequest    = 40001
	CodeUnauthorized  = 40100
	CodeNotFound      = 40401
	CodeConflict      = 40901
	CodeInternalError = 50001
)

// ==================== Handler ====================

type Handler struct {
	pool      *SessionPool
	tokenAuth *TokenAuth
	upgrader  websocket.Upgrader
}

func NewHandler(pool *SessionPool, tokenAuth *TokenAuth) *Handler {
	return &Handler{
		pool:      pool,
		tokenAuth: tokenAuth,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api")
	api.Use(h.tokenAuth.GinMiddleware())
	{
		api.GET("/sessions", h.HandleSessions)
		api.POST("/sessions", h.HandleCreateSession)
		api.DELETE("/sessions/:id", h.HandleCloseSession)
		api.PUT("/sessions/:id/rename", h.HandleRenameSession)
	}

	r.GET("/ws", h.HandleWebSocket)
}

func (h *Handler) HandleSessions(c *gin.Context) {
	sessions := h.pool.List()
	dtos := make([]SessionDTO, 0, len(sessions))

	for _, s := range sessions {
		s.mu.Lock()
		dtos = append(dtos, SessionDTO{
			ID:        s.ID,
			Name:      s.Name,
			Status:    string(s.Status),
			CreatedAt: s.CreatedAt.Unix(),
			ConnCount: len(s.conns),
		})
		s.mu.Unlock()
	}

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    dtos,
	})
}

func (h *Handler) HandleCreateSession(c *gin.Context) {
	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = CreateSessionRequest{}
	}

	var name string
	if req.Name != nil && *req.Name != "" {
		name = *req.Name
	}

	var shell string
	if req.Shell != nil && *req.Shell != "" {
		shell = *req.Shell
	}

	session, err := h.pool.Create(name, shell)
	if err != nil {
		status := http.StatusInternalServerError
		code := CodeInternalError

		if se, ok := err.(*SessionError); ok {
			switch se.Code {
			case "SHELL_NOT_FOUND", "SHELL_START_FAILED":
				status = http.StatusBadRequest
				code = CodeBadRequest
			case "INVALID_SESSION_NAME":
				status = http.StatusBadRequest
				code = CodeBadRequest
			}
		}

		c.JSON(status, APIResponse{
			Code:    code,
			Message: err.Error(),
		})
		return
	}

	session.mu.Lock()
	dto := SessionDTO{
		ID:        session.ID,
		Name:      session.Name,
		Status:    string(session.Status),
		CreatedAt: session.CreatedAt.Unix(),
		ConnCount: len(session.conns),
	}
	session.mu.Unlock()

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    dto,
	})
}

func (h *Handler) HandleCloseSession(c *gin.Context) {
	id := c.Param("id")

	if err := h.pool.Close(id); err != nil {
		status := http.StatusNotFound
		code := CodeNotFound

		if se, ok := err.(*SessionError); ok {
			if se.Code == "SESSION_NOT_FOUND" {
				status = http.StatusNotFound
				code = CodeNotFound
			}
		}

		c.JSON(status, APIResponse{
			Code:    code,
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "session closed",
	})
}

func (h *Handler) HandleRenameSession(c *gin.Context) {
	id := c.Param("id")

	var req RenameSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Code:    CodeBadRequest,
			Message: "name is required",
		})
		return
	}

	if err := h.pool.Rename(id, req.Name); err != nil {
		status := http.StatusNotFound
		code := CodeNotFound

		if se, ok := err.(*SessionError); ok {
			switch se.Code {
			case "SESSION_NOT_FOUND":
				status = http.StatusNotFound
				code = CodeNotFound
			case "INVALID_SESSION_NAME":
				status = http.StatusBadRequest
				code = CodeBadRequest
			}
		}

		c.JSON(status, APIResponse{
			Code:    code,
			Message: err.Error(),
		})
		return
	}

	session, ok := h.pool.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, APIResponse{
			Code:    CodeNotFound,
			Message: "session not found",
		})
		return
	}

	session.mu.Lock()
	dto := SessionDTO{
		ID:        session.ID,
		Name:      session.Name,
		Status:    string(session.Status),
		CreatedAt: session.CreatedAt.Unix(),
		ConnCount: len(session.conns),
	}
	session.mu.Unlock()

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    dto,
	})
}

// HandleWebSocket GET /ws
// 先升级连接，客户端首条消息必须是 auth
func (h *Handler) HandleWebSocket(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] upgrade failed: %v", err)
		return
	}

	ws := NewWSConn(conn)

	// 等待认证消息（5秒超时）
	if err := ws.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = ws.Close()
		return
	}

	_, raw, err := ws.ReadMessage()
	if err != nil {
		log.Printf("[WebSocket] auth read failed: %v", err)
		_ = ws.Close()
		return
	}

	// 重置读取超时
	_ = ws.conn.SetReadDeadline(time.Time{})

	msg, err := ParseV1Message(raw)
	if err != nil || msg.Type != MsgAuth {
		_ = ws.WriteJSON(NewErrorMessage("AUTH_REQUIRED", "first message must be auth"))
		_ = ws.Close()
		return
	}

	authPayload, ok := msg.GetAuth()
	if !ok {
		_ = ws.WriteJSON(NewErrorMessage("AUTH_REQUIRED", "invalid auth payload"))
		_ = ws.Close()
		return
	}

	result := h.tokenAuth.Validate(authPayload.Token)
	if result == ValidateInvalid {
		_ = ws.WriteJSON(NewErrorMessage("UNAUTHORIZED", "invalid token"))
		_ = ws.Close()
		return
	}

	readOnly := result == ValidateReadOnly

	sessionID := authPayload.SessionID
	var session *Session

	if sessionID != "" {
		var found bool
		session, found = h.pool.Get(sessionID)
		if !found {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_NOT_FOUND", "session not found"))
			_ = ws.Close()
			return
		}

		session.mu.Lock()
		if session.Status == SessionExited {
			session.mu.Unlock()
			_ = ws.WriteJSON(NewErrorMessage("SESSION_EXPIRED", "session process has exited"))
			_ = ws.Close()
			return
		}
		session.mu.Unlock()
	} else {
		session, err = h.pool.Create("", "")
		if err != nil {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_CREATE_FAILED", err.Error()))
			_ = ws.Close()
			return
		}
	}

	// 使用客户端提供的初始尺寸，未提供则回退到默认值
	initRows := authPayload.Rows
	initCols := authPayload.Cols
	if initRows == 0 || initCols == 0 {
		initRows = 24
		initCols = 80
	}

	// 注册连接
	session.AddConn(ws, initRows, initCols, readOnly)

	// 发送会话信息
	_ = ws.WriteJSON(NewSessionInfoMessage(session, ws, readOnly))

	// 发送缓冲区内容（Binary Frame）
	if output := session.GetOutput(); len(output) > 0 {
		_ = ws.WriteMessage(websocket.BinaryMessage, EncodeBinaryFrame(BinaryTypeOutput, output))
	}

	// 广播连接者列表和焦点状态
	session.BroadcastConnList()
	if session.FocusConn == ws {
		session.BroadcastFocusChange()
	}

	log.Printf("[WebSocket] connected to session %s (readOnly=%v)", session.ID, readOnly)

	// 创建速率限制器
	rateLimiter := NewTokenBucket(100*1024, 500*1024)

	// 进入消息读取循环
	h.handleWSMessages(ws, session, readOnly, rateLimiter)
}

func (h *Handler) handleWSMessages(ws *WSConn, session *Session, readOnly bool, rateLimiter *TokenBucket) {
	defer func() {
		newPtyRows, newPtyCols, shouldNotify := session.RemoveConn(ws)
		if shouldNotify {
			session.BroadcastMessage(NewPtyResizeMessage(newPtyRows, newPtyCols))
		}
		session.BroadcastConnList()
		if session.FocusConn == nil || session.FocusConn == ws {
			session.BroadcastFocusChange()
		}
		_ = ws.Close()
		log.Printf("[WebSocket] disconnected from session %s", session.ID)
	}()

	const readTimeout = 60 * time.Second
	_ = ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
	ws.conn.SetPongHandler(func(appData string) error {
		_ = ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	for {
		msgType, raw, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[WebSocket] unexpected close for session %s: %v", session.ID, err)
			}
			return
		}

		// Binary Frame: 仅用于 input
		if msgType == websocket.BinaryMessage {
			bType, data, decodeErr := DecodeBinaryFrame(raw)
			if decodeErr != nil {
				log.Printf("[WebSocket] invalid binary frame from session %s: %v", session.ID, decodeErr)
				continue
			}
			if bType == BinaryTypeInput {
				h.handleInput(ws, session, readOnly, rateLimiter, data)
			}
			continue
		}

		// Text Frame: JSON 消息
		vmsg, err := ParseV1Message(raw)
		if err != nil {
			log.Printf("[WebSocket] invalid message from session %s: %v", session.ID, err)
			continue
		}

		switch vmsg.Type {
		case MsgResize:
			h.handleResize(ws, session, vmsg)
		case MsgPing:
			_ = ws.WriteJSON(NewPongMessage())
			_ = ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
		case MsgTakeFocus:
			h.handleTakeFocus(ws, session)
		case MsgReleaseFocus:
			h.handleReleaseFocus(ws, session)
		default:
			log.Printf("[WebSocket] unknown message type %q from session %s", vmsg.Type, session.ID)
		}
	}
}

func (h *Handler) handleInput(ws *WSConn, session *Session, readOnly bool, rateLimiter *TokenBucket, data []byte) {
	if len(data) == 0 {
		return
	}
	if readOnly {
		_ = ws.WriteJSON(NewErrorMessage("READ_ONLY", "this connection is read-only"))
		return
	}
	if !session.CanInput(ws) {
		_ = ws.WriteJSON(NewErrorMessage("NO_FOCUS", "you do not have input focus"))
		return
	}
	if !rateLimiter.Allow(len(data)) {
		log.Printf("[WebSocket] rate limit exceeded for session %s", session.ID)
		_ = ws.WriteJSON(NewErrorMessage("RATE_LIMITED", "input rate limit exceeded"))
		_ = ws.Close()
		return
	}
	if session.Pty != nil {
		if _, err := session.Pty.Write(data); err != nil {
			log.Printf("[WebSocket] failed to write to PTY for session %s: %v", session.ID, err)
		}
	}
}

func (h *Handler) handleResize(ws *WSConn, session *Session, vmsg *V1Message) {
	payload, ok := vmsg.GetResize()
	if !ok {
		log.Printf("[WebSocket] invalid resize for session %s", session.ID)
		return
	}

	newPtyRows, newPtyCols, ptyChanged := session.UpdateConnSize(ws, payload.Rows, payload.Cols)
	if ptyChanged {
		session.BroadcastMessage(NewPtyResizeMessage(newPtyRows, newPtyCols))
	}
}

func (h *Handler) handleTakeFocus(ws *WSConn, session *Session) {
	if session.TakeFocus(ws) {
		session.BroadcastFocusChange()
		session.broadcastSessionInfo()
	} else {
		_ = ws.WriteJSON(NewErrorMessage("FOCUS_DENIED", "unable to take focus"))
	}
}

func (h *Handler) handleReleaseFocus(ws *WSConn, session *Session) {
	session.ReleaseFocus(ws)
	session.BroadcastFocusChange()
	session.broadcastSessionInfo()
}
