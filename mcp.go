package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== MCP Session 管理 ====================

type MCPSession struct {
	ID         string
	Writer     http.ResponseWriter
	Flusher    http.Flusher
	Mu         sync.Mutex
	Closed     bool
	initDone   bool
	protocolVersion string
}

func (m *MCPSession) SendEvent(event, data string) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	if m.Closed {
		return fmt.Errorf("session closed")
	}
	if event != "" {
		fmt.Fprintf(m.Writer, "event: %s\n", event)
	}
	fmt.Fprintf(m.Writer, "data: %s\n\n", data)
	m.Flusher.Flush()
	return nil
}

func (m *MCPSession) Close() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Closed = true
}

var (
	mcpSessions     = make(map[string]*MCPSession)
	mcpSessionsMu   sync.RWMutex
	mcpSessionCounter int64
	mcpCounterMu    sync.Mutex
)

func generateMCPSessionID() string {
	mcpCounterMu.Lock()
	defer mcpCounterMu.Unlock()
	mcpSessionCounter++
	return fmt.Sprintf("mcp-%d-%d", time.Now().Unix(), mcpSessionCounter)
}

func getMCPSession(id string) (*MCPSession, bool) {
	mcpSessionsMu.RLock()
	defer mcpSessionsMu.RUnlock()
	s, ok := mcpSessions[id]
	return s, ok
}

func storeMCPSession(s *MCPSession) {
	mcpSessionsMu.Lock()
	defer mcpSessionsMu.Unlock()
	mcpSessions[s.ID] = s
}

func removeMCPSession(id string) {
	mcpSessionsMu.Lock()
	defer mcpSessionsMu.Unlock()
	if s, ok := mcpSessions[id]; ok {
		s.Close()
		delete(mcpSessions, id)
	}
}

// ==================== JSON-RPC 结构 ====================

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type JSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

func newJSONRPCError(id interface{}, code int, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
}

func newJSONRPCResult(id interface{}, result interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// ==================== SSE Handler ====================

func (h *Handler) HandleMCPSSE(c *gin.Context) {
	// 设置SSE响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Code:    CodeInternalError,
			Message: "streaming not supported",
		})
		return
	}

	sid := generateMCPSessionID()
	mcpSess := &MCPSession{
		ID:      sid,
		Writer:  c.Writer,
		Flusher: flusher,
	}
	storeMCPSession(mcpSess)
	defer removeMCPSession(sid)

	// 发送 endpoint 事件
	endpointURL := fmt.Sprintf("/mcp/message?sid=%s", sid)
	if err := mcpSess.SendEvent("endpoint", endpointURL); err != nil {
		log.Printf("[MCP] send endpoint failed: %v", err)
		return
	}

	log.Printf("[MCP] SSE connection established: %s", sid)

	// 保持连接，定期发送注释保持连接活跃
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[MCP] SSE client disconnected: %s", sid)
			return
		case <-ticker.C:
			mcpSess.Mu.Lock()
			if mcpSess.Closed {
				mcpSess.Mu.Unlock()
				return
			}
			fmt.Fprintf(mcpSess.Writer, ": keepalive\n\n")
			mcpSess.Flusher.Flush()
			mcpSess.Mu.Unlock()
		}
	}
}

// ==================== Streamable HTTP Handler ====================

func (h *Handler) HandleMCPHTTP(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, newJSONRPCError(nil, -32700, "Parse error"))
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, newJSONRPCError(nil, -32700, "Parse error: invalid JSON"))
		return
	}

	if req.JSONRPC != "2.0" {
		c.JSON(http.StatusBadRequest, newJSONRPCError(req.ID, -32600, "Invalid Request"))
		return
	}

	resp := h.processMCPHTTPRequest(&req)
	if resp != nil {
		c.JSON(http.StatusOK, resp)
	} else {
		c.Status(http.StatusAccepted)
	}
}

func (h *Handler) processMCPHTTPRequest(req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return h.handleInitializeHTTP(req)
	case "initialized":
		return nil
	case "tools/list":
		return h.handleToolsList(req)
	case "tools/call":
		return h.handleToolsCall(req)
	default:
		errResp := newJSONRPCError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
		return &errResp
	}
}

func (h *Handler) handleInitializeHTTP(req *JSONRPCRequest) *JSONRPCResponse {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		resp := newJSONRPCError(req.ID, -32602, "Invalid params")
		return &resp
	}

	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: struct {
			Tools struct{} `json:"tools"`
		}{Tools: struct{}{}},
		ServerInfo: struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{
			Name:    "go-remote-terminal-mcp",
			Version: "1.0.0",
		},
	}

	resp := newJSONRPCResult(req.ID, result)
	return &resp
}

// ==================== Message Handler (SSE transport) ====================

func (h *Handler) HandleMCPMessage(c *gin.Context) {
	sid := c.Query("sid")
	if sid == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Code:    CodeBadRequest,
			Message: "missing sid parameter",
		})
		return
	}

	mcpSess, ok := getMCPSession(sid)
	if !ok {
		c.JSON(http.StatusNotFound, APIResponse{
			Code:    CodeNotFound,
			Message: "MCP session not found",
		})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{
			Code:    CodeBadRequest,
			Message: "failed to read request body",
		})
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		// 尝试发送错误响应
		resp := newJSONRPCError(nil, -32700, "Parse error: invalid JSON")
		sendMCPResponse(mcpSess, resp)
		c.Status(http.StatusAccepted)
		return
	}

	if req.JSONRPC != "2.0" {
		resp := newJSONRPCError(req.ID, -32600, "Invalid Request: jsonrpc must be 2.0")
		sendMCPResponse(mcpSess, resp)
		c.Status(http.StatusAccepted)
		return
	}

	// 处理请求
	resp := h.processMCPRequest(mcpSess, &req)
	if resp != nil {
		sendMCPResponse(mcpSess, *resp)
	}

	c.Status(http.StatusAccepted)
}

func sendMCPResponse(mcpSess *MCPSession, resp JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[MCP] marshal response failed: %v", err)
		return
	}
	if err := mcpSess.SendEvent("message", string(data)); err != nil {
		log.Printf("[MCP] send response failed: %v", err)
	}
}



// processMCPRequest 处理 JSON-RPC 请求，返回响应（通知返回nil）
func (h *Handler) processMCPRequest(mcpSess *MCPSession, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(mcpSess, req)
	case "initialized":
		// 通知，无需响应
		log.Printf("[MCP] client initialized: %s", mcpSess.ID)
		mcpSess.initDone = true
		return nil
	case "tools/list":
		return h.handleToolsList(req)
	case "tools/call":
		return h.handleToolsCall(req)
	default:
		errResp := newJSONRPCError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
		return &errResp
	}
}

// ==================== MCP 方法实现 ====================

type InitializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct{} `json:"capabilities"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools struct{} `json:"tools"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

func (h *Handler) handleInitialize(mcpSess *MCPSession, req *JSONRPCRequest) *JSONRPCResponse {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		resp := newJSONRPCError(req.ID, -32602, "Invalid params")
		return &resp
	}

	mcpSess.protocolVersion = params.ProtocolVersion
	if mcpSess.protocolVersion == "" {
		mcpSess.protocolVersion = "2024-11-05"
	}

	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: struct {
			Tools struct{} `json:"tools"`
		}{Tools: struct{}{}},
		ServerInfo: struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{
			Name:    "go-remote-terminal-mcp",
			Version: "1.0.0",
		},
	}

	resp := newJSONRPCResult(req.ID, result)
	return &resp
}

func (h *Handler) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	tools := AllMCPTools()
	result := map[string]interface{}{
		"tools": tools,
	}
	resp := newJSONRPCResult(req.ID, result)
	return &resp
}

type ToolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (h *Handler) handleToolsCall(req *JSONRPCRequest) *JSONRPCResponse {
	var params ToolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		resp := newJSONRPCError(req.ID, -32602, "Invalid params")
		return &resp
	}

	result := HandleMCPTool(h.pool, params.Name, params.Arguments)
	resp := newJSONRPCResult(req.ID, result)
	return &resp
}
