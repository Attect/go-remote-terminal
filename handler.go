package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ==================== API响应结构 ====================

// APIResponse 统一API响应格式
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// SessionDTO 会话数据传输对象
type SessionDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

// CreateSessionRequest 创建会话请求
type CreateSessionRequest struct {
	Name  *string `json:"name"`
	Shell *string `json:"shell"`
}

// RenameSessionRequest 重命名会话请求
type RenameSessionRequest struct {
	Name string `json:"name"`
}

// ==================== 业务状态码 ====================

const (
	CodeSuccess       = 0
	CodeBadRequest    = 40001
	CodeUnauthorized  = 40100
	CodeNotFound      = 40401
	CodeConflict      = 40901
	CodeInternalError = 50001
)

// ==================== Handler ====================

// Handler HTTP/WebSocket处理器
type Handler struct {
	pool      *SessionPool
	tokenAuth *TokenAuth
	upgrader  websocket.Upgrader
}

// NewHandler 创建处理器
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

// RegisterRoutes 注册所有路由到Gin引擎
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

// HandleSessions GET /api/sessions
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
		})
		s.mu.Unlock()
	}

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    dtos,
	})
}

// HandleCreateSession POST /api/sessions
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
	}
	session.mu.Unlock()

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    dto,
	})
}

// HandleCloseSession DELETE /api/sessions/:id
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

// HandleRenameSession PUT /api/sessions/:id/rename
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
	}
	session.mu.Unlock()

	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    dto,
	})
}

// HandleWebSocket GET /ws
// query参数: token(必需), session_id(可选，用于加入已有会话)
func (h *Handler) HandleWebSocket(c *gin.Context) {
	if !h.tokenAuth.ValidateOrAbort(c) {
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] upgrade failed: %v", err)
		return
	}

	ws := NewWSConn(conn)
	sessionID := c.Query("session_id")

	var session *Session

	if sessionID != "" {
		// 加入已有会话
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
		// 创建新会话
		session, err = h.pool.Create("", "")
		if err != nil {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_CREATE_FAILED", err.Error()))
			_ = ws.Close()
			return
		}
	}

	// 立即注册连接到会话（默认尺寸24x80），确保不丢失任何PTY输出
	session.AddConn(ws, 24, 80)

	// 发送会话信息（客户端需要先收到此消息才能正确处理后续输出）
	_ = ws.WriteJSON(NewSessionInfoMessage(session))

	// 发送缓冲区内容（重连/加入时回显已有输出）
	if output := session.GetOutput(); len(output) > 0 {
		_ = ws.WriteJSON(NewOutputMessage(output))
	}

	log.Printf("[WebSocket] connected to session %s", session.ID)

	// 进入消息读取循环
	h.handleWSMessages(ws, session)
}

// handleWSMessages 处理WebSocket消息循环
func (h *Handler) handleWSMessages(ws *WSConn, session *Session) {
	defer func() {
		// 断开时从会话移除连接，通知其他连接PTY尺寸可能变更
		newPtyRows, newPtyCols, shouldNotify := session.RemoveConn(ws)
		if shouldNotify {
			session.BroadcastMessage(NewPtyResizeMessage(newPtyRows, newPtyCols))
		}
		_ = ws.Close()
		log.Printf("[WebSocket] disconnected from session %s", session.ID)
	}()

	const readTimeout = 60 * time.Second
	ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
	ws.conn.SetPongHandler(func(appData string) error {
		ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[WebSocket] unexpected close for session %s: %v", session.ID, err)
			}
			return
		}

		msg, err := ParseMessage(message)
		if err != nil {
			log.Printf("[WebSocket] invalid message from session %s: %v", session.ID, err)
			continue
		}

		switch msg.Type {
		case MsgInput:
			h.handleInput(session, msg)
		case MsgResize:
			h.handleResize(ws, session, msg)
		case MsgPing:
			_ = ws.WriteJSON(NewPongMessage())
			ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
		default:
			log.Printf("[WebSocket] unknown message type %q from session %s", msg.Type, session.ID)
		}
	}
}

// handleInput 处理终端输入消息
func (h *Handler) handleInput(session *Session, msg *WSMessage) {
	data, err := msg.DecodeData()
	if err != nil {
		log.Printf("[WebSocket] failed to decode input for session %s: %v", session.ID, err)
		return
	}

	if len(data) > 0 && session.Pty != nil {
		if _, err := session.Pty.Write(data); err != nil {
			log.Printf("[WebSocket] failed to write to PTY for session %s: %v", session.ID, err)
		}
	}
}

// handleResize 处理终端尺寸变更消息
// 连接已在HandleWebSocket中注册，此处仅更新尺寸
func (h *Handler) handleResize(ws *WSConn, session *Session, msg *WSMessage) {
	rows, cols, err := msg.GetResize()
	if err != nil {
		log.Printf("[WebSocket] invalid resize for session %s: %v", session.ID, err)
		return
	}

	newPtyRows, newPtyCols, ptyChanged := session.UpdateConnSize(ws, rows, cols)
	if ptyChanged {
		session.BroadcastMessage(NewPtyResizeMessage(newPtyRows, newPtyCols))
	}
}
