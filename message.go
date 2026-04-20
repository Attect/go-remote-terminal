package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// MessageType WebSocket消息类型
type MessageType string

const (
	// 客户端→服务端
	MsgInput  MessageType = "input"  // 终端输入字符
	MsgResize MessageType = "resize" // 终端尺寸变更

	// 服务端→客户端
	MsgOutput      MessageType = "output"       // 终端输出数据
	MsgError       MessageType = "error"        // 错误信息
	MsgSessionInfo MessageType = "session_info" // 会话元信息

	// 双向
	MsgPing MessageType = "ping" // 心跳检测
	MsgPong MessageType = "pong" // 心跳响应
)

// WSMessage WebSocket消息通用结构
// 采用扁平结构，不同类型消息使用不同字段组合，避免嵌套JSON
type WSMessage struct {
	Type    MessageType `json:"type"`
	Data    string      `json:"data,omitempty"`    // Base64编码（input/output类型）
	Rows    uint16      `json:"rows,omitempty"`    // resize类型：行数
	Cols    uint16      `json:"cols,omitempty"`    // resize类型：列数
	ID      string      `json:"id,omitempty"`      // session_info类型：会话ID
	Name    string      `json:"name,omitempty"`    // session_info类型：会话名称
	Status  string      `json:"status,omitempty"`  // session_info类型：会话状态
	Code    string      `json:"code,omitempty"`    // error类型：错误码
	Message string      `json:"message,omitempty"` // error类型：错误描述
}

// NewInputMessage 创建终端输入消息
func NewInputMessage(data []byte) WSMessage {
	return WSMessage{
		Type: MsgInput,
		Data: base64.StdEncoding.EncodeToString(data),
	}
}

// NewOutputMessage 创建终端输出消息
func NewOutputMessage(data []byte) WSMessage {
	return WSMessage{
		Type: MsgOutput,
		Data: base64.StdEncoding.EncodeToString(data),
	}
}

// NewResizeMessage 创建resize消息
func NewResizeMessage(rows, cols uint16) WSMessage {
	return WSMessage{
		Type: MsgResize,
		Rows: rows,
		Cols: cols,
	}
}

// NewErrorMessage 创建错误消息
func NewErrorMessage(code, message string) WSMessage {
	return WSMessage{
		Type:    MsgError,
		Code:    code,
		Message: message,
	}
}

// NewSessionInfoMessage 创建会话信息消息
func NewSessionInfoMessage(s *Session) WSMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return WSMessage{
		Type:   MsgSessionInfo,
		ID:     s.ID,
		Name:   s.Name,
		Status: string(s.Status),
	}
}

// NewPongMessage 创建pong消息
func NewPongMessage() WSMessage {
	return WSMessage{Type: MsgPong}
}

// ParseMessage 解析WebSocket消息
func ParseMessage(raw []byte) (*WSMessage, error) {
	var msg WSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("invalid message format: %w", err)
	}
	return &msg, nil
}

// DecodeData 解码Base64数据（用于input/output消息）
func (m *WSMessage) DecodeData() ([]byte, error) {
	if m.Data == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(m.Data)
}

// GetResize 获取resize消息的尺寸信息
// 优先使用直接的Rows/Cols字段，其次解析Data字段中的JSON
func (m *WSMessage) GetResize() (rows, cols uint16, err error) {
	if m.Rows > 0 && m.Cols > 0 {
		return m.Rows, m.Cols, nil
	}

	// 尝试从Data字段解析JSON格式的resize数据
	if m.Data != "" {
		var resizeData struct {
			Rows uint16 `json:"rows"`
			Cols uint16 `json:"cols"`
		}
		if err := json.Unmarshal([]byte(m.Data), &resizeData); err == nil {
			if resizeData.Rows > 0 && resizeData.Cols > 0 {
				return resizeData.Rows, resizeData.Cols, nil
			}
		}
	}

	return 0, 0, fmt.Errorf("invalid resize message: missing rows/cols")
}
