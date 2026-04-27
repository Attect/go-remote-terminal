package main

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion 当前协议版本
const ProtocolVersion = 1

// MessageType WebSocket消息类型
type MessageType string

const (
	// 客户端→服务端
	MsgAuth   MessageType = "auth"   // 认证消息（连接后首条）
	MsgInput  MessageType = "input"  // 终端输入（Binary Frame）
	MsgResize MessageType = "resize" // 终端尺寸变更
	MsgTakeFocus    MessageType = "take_focus"    // 申请输入焦点
	MsgReleaseFocus MessageType = "release_focus" // 释放输入焦点

	// 服务端→客户端
	MsgOutput      MessageType = "output"       // 终端输出（Binary Frame）
	MsgError       MessageType = "error"        // 错误信息
	MsgSessionInfo MessageType = "session_info" // 会话元信息
	MsgConnList    MessageType = "conn_list"    // 连接者列表变更
	MsgFocusChange MessageType = "focus_change" // 焦点变更通知
	MsgPtyResize   MessageType = "pty_resize"   // PTY尺寸变更通知

	// 双向
	MsgPing MessageType = "ping" // 心跳检测
	MsgPong MessageType = "pong" // 心跳响应
)

// Binary frame 类型标识
const (
	BinaryTypeInput  = 0x01
	BinaryTypeOutput = 0x02
)

// ==================== V1Message 版本化JSON消息 ====================

// V1Message 协议顶层消息结构
type V1Message struct {
	Version int         `json:"v"`       // 协议版本
	Type    MessageType `json:"type"`    // 消息类型
	Payload interface{} `json:"payload"` // 类型相关负载
}

// MarshalJSON 自定义序列化，确保payload被正确嵌入
func (m V1Message) MarshalJSON() ([]byte, error) {
	type envelope V1Message
	return json.Marshal(envelope(m))
}

// ==================== Payload 结构 ====================

// AuthPayload 认证请求
type AuthPayload struct {
	Token     string `json:"token"`               // 访问令牌
	SessionID string `json:"session_id,omitempty"` // 期望加入的会话ID
	Rows      uint16 `json:"rows,omitempty"`      // 客户端终端行数
	Cols      uint16 `json:"cols,omitempty"`      // 客户端终端列数
}

// ResizePayload 尺寸变更
type ResizePayload struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// ErrorPayload 错误信息
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ConnDTO 连接者信息
type ConnDTO struct {
	Name  string `json:"name"`  // 连接者名称
	Color string `json:"color"` // 连接者颜色
	Focus bool   `json:"focus"` // 是否拥有输入焦点
}

// SessionInfoPayload 会话信息
type SessionInfoPayload struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Rows     uint16    `json:"rows"`      // 当前PTY行数
	Cols     uint16    `json:"cols"`      // 当前PTY列数
	Focused  bool      `json:"focused"`   // 当前连接是否拥有焦点
	ReadOnly bool      `json:"read_only"` // 当前连接是否为只读
	Conns    []ConnDTO `json:"conns"`     // 所有连接者
}

// ConnListPayload 连接者列表变更
type ConnListPayload struct {
	Conns []ConnDTO `json:"conns"`
}

// FocusChangePayload 焦点变更通知
type FocusChangePayload struct {
	ConnName string `json:"conn_name"` // 获得焦点的连接名，空表示无焦点
}

// ==================== 便捷构造函数 ====================

// NewAuthMessage 创建认证消息
func NewAuthMessage(payload AuthPayload) V1Message {
	return V1Message{Version: ProtocolVersion, Type: MsgAuth, Payload: payload}
}

// NewResizeMessage 创建resize消息
func NewResizeMessage(rows, cols uint16) V1Message {
	return V1Message{Version: ProtocolVersion, Type: MsgResize, Payload: ResizePayload{Rows: rows, Cols: cols}}
}

// NewErrorMessage 创建错误消息
func NewErrorMessage(code, message string) V1Message {
	return V1Message{Version: ProtocolVersion, Type: MsgError, Payload: ErrorPayload{Code: code, Message: message}}
}

// NewSessionInfoMessage 创建会话信息消息
func NewSessionInfoMessage(s *Session, ws *WSConn, readOnly bool) V1Message {
	s.mu.Lock()
	payload := SessionInfoPayload{
		ID:       s.ID,
		Name:     s.Name,
		Status:   string(s.Status),
		Rows:     s.ptyRows,
		Cols:     s.ptyCols,
		ReadOnly: readOnly,
		Conns:    s.connDTOsLocked(),
	}
	if s.FocusConn == ws {
		payload.Focused = true
	}
	s.mu.Unlock()
	return V1Message{Version: ProtocolVersion, Type: MsgSessionInfo, Payload: payload}
}

// NewConnListMessage 创建连接者列表消息
func NewConnListMessage(s *Session) V1Message {
	s.mu.Lock()
	payload := ConnListPayload{Conns: s.connDTOsLocked()}
	s.mu.Unlock()
	return V1Message{Version: ProtocolVersion, Type: MsgConnList, Payload: payload}
}

// NewFocusChangeMessage 创建焦点变更消息
func NewFocusChangeMessage(connName string) V1Message {
	return V1Message{Version: ProtocolVersion, Type: MsgFocusChange, Payload: FocusChangePayload{ConnName: connName}}
}

// NewPongMessage 创建pong消息
func NewPongMessage() V1Message {
	return V1Message{Version: ProtocolVersion, Type: MsgPong}
}

// NewPtyResizeMessage 创建PTY尺寸变更通知消息
func NewPtyResizeMessage(rows, cols uint16) V1Message {
	return V1Message{
		Version: ProtocolVersion,
		Type:    MsgPtyResize,
		Payload: ResizePayload{Rows: rows, Cols: cols},
	}
}

// ==================== 解析函数 ====================

// ParseV1Message 解析版本化JSON消息
func ParseV1Message(raw []byte) (*V1Message, error) {
	var env struct {
		Version int             `json:"v"`
		Type    MessageType     `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("invalid message format: %w", err)
	}
	if env.Version != ProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version: %d", env.Version)
	}

	msg := &V1Message{Version: env.Version, Type: env.Type}

	switch env.Type {
	case MsgAuth:
		var p AuthPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, fmt.Errorf("invalid auth payload: %w", err)
		}
		msg.Payload = p
	case MsgResize:
		var p ResizePayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, fmt.Errorf("invalid resize payload: %w", err)
		}
		msg.Payload = p
	case MsgPing, MsgPong, MsgTakeFocus, MsgReleaseFocus:
		// 无payload
	default:
		// 未知类型，保留原始payload
		msg.Payload = env.Payload
	}

	return msg, nil
}

// ==================== Payload 访问辅助方法 ====================

// GetAuth 获取认证负载
func (m *V1Message) GetAuth() (AuthPayload, bool) {
	p, ok := m.Payload.(AuthPayload)
	return p, ok
}

// GetResize 获取resize负载
func (m *V1Message) GetResize() (ResizePayload, bool) {
	p, ok := m.Payload.(ResizePayload)
	return p, ok
}

// ==================== Binary Frame 编解码 ====================

// EncodeBinaryFrame 编码Binary Frame
// msgType: BinaryTypeInput 或 BinaryTypeOutput
func EncodeBinaryFrame(msgType byte, data []byte) []byte {
	result := make([]byte, 1+len(data))
	result[0] = msgType
	copy(result[1:], data)
	return result
}

// DecodeBinaryFrame 解码Binary Frame
func DecodeBinaryFrame(data []byte) (msgType byte, payload []byte, err error) {
	if len(data) < 1 {
		return 0, nil, fmt.Errorf("binary frame too short")
	}
	return data[0], data[1:], nil
}
