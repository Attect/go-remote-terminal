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
	Code    int         `json:"code"`    // 业务状态码：0成功，非0失败
	Message string      `json:"message"` // 描述信息
	Data    interface{} `json:"data"`    // 业务数据
}

// SessionDTO 会话数据传输对象
type SessionDTO struct {
	ID        string `json:"id"`         // 会话ID
	Name      string `json:"name"`       // 会话名称
	Status    string `json:"status"`     // 会话状态
	CreatedAt int64  `json:"created_at"` // 创建时间戳(Unix)
}

// CreateSessionRequest 创建会话请求
type CreateSessionRequest struct {
	Name  *string `json:"name"`  // 会话名称（可选）
	Shell *string `json:"shell"` // Shell路径（可选）
}

// RenameSessionRequest 重命名会话请求
type RenameSessionRequest struct {
	Name string `json:"name"` // 新名称
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
	pool      *SessionPool       // 会话池
	tokenAuth *TokenAuth         // Token认证器
	upgrader  websocket.Upgrader // WebSocket升级器
}

// NewHandler 创建处理器
func NewHandler(pool *SessionPool, tokenAuth *TokenAuth) *Handler {
	return &Handler{
		pool:      pool,
		tokenAuth: tokenAuth,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// 允许所有来源，Token认证负责安全
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

// RegisterRoutes 注册所有路由到Gin引擎
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	// API路由组，需要Token认证
	api := r.Group("/api")
	api.Use(h.tokenAuth.GinMiddleware())
	{
		api.GET("/sessions", h.HandleSessions)
		api.POST("/sessions", h.HandleCreateSession)
		api.DELETE("/sessions/:id", h.HandleCloseSession)
		api.PUT("/sessions/:id/rename", h.HandleRenameSession)
	}

	// WebSocket路由（Token在query参数中验证）
	r.GET("/ws", h.HandleWebSocket)
}

// HandleSessions GET /api/sessions - 获取会话列表
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

// HandleCreateSession POST /api/sessions - 创建新会话
func (h *Handler) HandleCreateSession(c *gin.Context) {
	var req CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 请求体为空时使用默认值
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

// HandleCloseSession DELETE /api/sessions/:id - 关闭会话
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

// HandleRenameSession PUT /api/sessions/:id/rename - 重命名会话
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

	// 返回更新后的会话信息
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

// HandleWebSocket GET /ws - WebSocket升级
// query参数: token(必需), session_id(可选，用于重连)
func (h *Handler) HandleWebSocket(c *gin.Context) {
	// 验证Token
	if !h.tokenAuth.ValidateOrAbort(c) {
		return
	}

	// 升级为WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] upgrade failed: %v", err)
		return
	}

	ws := NewWSConn(conn)
	sessionID := c.Query("session_id")

	var session *Session

	if sessionID != "" {
		// 重连模式：尝试附加到已有会话
		var found bool
		session, found = h.pool.Get(sessionID)
		if !found {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_NOT_FOUND", "session not found"))
			_ = ws.Close()
			return
		}

		// 检查会话状态
		session.mu.Lock()
		if session.Status == SessionExited {
			session.mu.Unlock()
			_ = ws.WriteJSON(NewErrorMessage("SESSION_EXPIRED", "session process has exited"))
			_ = ws.Close()
			return
		}
		session.mu.Unlock()

		// 尝试绑定写入端
		if err := session.AttachWriter(ws); err != nil {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_BUSY", "session already has a writer"))
			_ = ws.Close()
			return
		}

		// 发送缓冲区内容（重连回显）
		if output := session.GetOutput(); len(output) > 0 {
			_ = ws.WriteJSON(NewOutputMessage(output))
		}
	} else {
		// 新建模式：创建新会话
		session, err = h.pool.Create("", "")
		if err != nil {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_CREATE_FAILED", err.Error()))
			_ = ws.Close()
			return
		}

		// 绑定写入端
		if err := session.AttachWriter(ws); err != nil {
			_ = ws.WriteJSON(NewErrorMessage("SESSION_BUSY", "session already has a writer"))
			_ = ws.Close()
			return
		}
	}

	// 发送会话信息
	_ = ws.WriteJSON(NewSessionInfoMessage(session))

	log.Printf("[WebSocket] connected to session %s", session.ID)

	// 进入消息读取循环
	h.handleWSMessages(ws, session)
}

// handleWSMessages 处理WebSocket消息循环
func (h *Handler) handleWSMessages(ws *WSConn, session *Session) {
	defer func() {
		// 断开时解除绑定
		session.DetachWriter(ws)
		_ = ws.Close()
		log.Printf("[WebSocket] disconnected from session %s", session.ID)
	}()

	// 设置读取超时和ping/pong处理
	// 60秒内无消息则认为连接异常，防止半开连接长期占用资源
	const readTimeout = 60 * time.Second
	ws.conn.SetReadDeadline(time.Now().Add(readTimeout))
	ws.conn.SetPongHandler(func(appData string) error {
		// 收到pong响应，刷新读取超时
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

		// 解析消息
		msg, err := ParseMessage(message)
		if err != nil {
			log.Printf("[WebSocket] invalid message from session %s: %v", session.ID, err)
			continue
		}

		// 根据消息类型处理
		switch msg.Type {
		case MsgInput:
			h.handleInput(ws, session, msg)
		case MsgResize:
			h.handleResize(ws, session, msg)
		case MsgPing:
			_ = ws.WriteJSON(NewPongMessage())
			// 收到ping也算连接活跃，刷新读取超时
			ws.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		default:
			log.Printf("[WebSocket] unknown message type %q from session %s", msg.Type, session.ID)
		}
	}
}

// handleInput 处理终端输入消息
func (h *Handler) handleInput(ws *WSConn, session *Session, msg *WSMessage) {
	data, err := msg.DecodeData()
	if err != nil {
		log.Printf("[WebSocket] failed to decode input data for session %s: %v", session.ID, err)
		return
	}

	if len(data) > 0 && session.Pty != nil {
		if _, err := session.Pty.Write(data); err != nil {
			log.Printf("[WebSocket] failed to write to PTY for session %s: %v", session.ID, err)
		}
	}
}

// handleResize 处理终端尺寸变更消息
func (h *Handler) handleResize(ws *WSConn, session *Session, msg *WSMessage) {
	rows, cols, err := msg.GetResize()
	if err != nil {
		log.Printf("[WebSocket] invalid resize message from session %s: %v", session.ID, err)
		return
	}

	if session.Pty != nil {
		if err := session.Pty.Resize(rows, cols); err != nil {
			log.Printf("[WebSocket] failed to resize PTY for session %s: %v", session.ID, err)
		}
	}
}
