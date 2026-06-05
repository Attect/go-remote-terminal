package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// ==================== MCP Tool 定义 ====================

// MCPTool 表示一个MCP工具
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema ToolSchema  `json:"inputSchema"`
}

// ToolSchema JSON Schema for tool input
type ToolSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]ToolProperty `json:"properties"`
	Required   []string               `json:"required"`
}

// ToolProperty JSON Schema property
type ToolProperty struct {
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// ToolResult 工具调用结果
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent 结果内容项
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(text string) ToolResult {
	return ToolResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	}
}

func errorResult(errMsg string) ToolResult {
	return ToolResult{
		Content: []ToolContent{{Type: "text", Text: errMsg}},
		IsError: true,
	}
}

// AllMCPTools 返回所有可用的MCP工具定义
func AllMCPTools() []MCPTool {
	return []MCPTool{
		{
			Name:        "environment_info",
			Description: "获取当前运行环境的基础信息，包括操作系统、架构、默认Shell路径等。建议在首次使用终端功能前调用，以了解目标环境的Shell类型和系统信息。",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]ToolProperty{},
				Required:   []string{},
			},
		},
		{
			Name:        "create",
			Description: "创建并启动一个新的命令行终端（Shell）。当你需要在服务器上执行命令、运行脚本、操作文件系统或进行任何Shell交互时，必须首先调用此工具创建终端。创建成功后，使用 send_input 发送命令，使用 get_output 获取结果。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"name":    {Type: "string", Description: "终端名称（可选，默认自动生成）"},
					"opener":  {Type: "string", Description: "打开者名称（如AI Agent名称）"},
					"purpose": {Type: "string", Description: "打开目的/用途描述"},
					"rows":               {Type: "integer", Description: "终端行数（可选，默认40）", Default: 40},
					"cols":               {Type: "integer", Description: "终端列数（可选，默认120）", Default: 120},
					"working_directory":  {Type: "string", Description: "终端初始工作目录"},
				},
				Required: []string{"opener", "purpose", "working_directory"},
			},
		},
		{
			Name:        "list",
			Description: "列出当前所有活跃的终端会话。当你不确定有哪些终端可用，或需要查看之前创建的终端时调用。",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]ToolProperty{},
				Required:   []string{},
			},
		},
		{
			Name:        "send_input",
			Description: "向已创建的终端发送输入并执行命令。支持发送普通文本命令（如 `ls -la`）或模拟特殊按键（如 Ctrl+C、Enter）。创建终端后，使用此工具来执行具体的操作。当 input_type 为 text 时，可通过 submit 参数控制是否在输入内容后自动追加回车执行。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
					"input_type": {Type: "string", Description: "输入类型: text 或 key", Enum: []string{"text", "key"}},
					"data":       {Type: "string", Description: "输入内容。text类型时直接发送文本；key类型时填写键名如 Enter, ArrowUp, Ctrl+C, Escape 等"},
					"submit":     {Type: "boolean", Description: "（仅 text 类型有效）是否在输入内容后自动追加回车执行。true=输入并执行；false=仅输入不执行（默认）", Default: false},
				},
				Required: []string{"session_id", "input_type", "data"},
			},
		},
		{
			Name:        "get_output",
			Description: "获取终端命令执行的输出结果（自动去除ANSI控制符）。在通过 send_input 发送命令后，调用此工具读取命令返回的文本输出。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
					"lines":      {Type: "integer", Description: "获取行数（可选，默认50）", Default: 50},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "get_screen",
			Description: "获取终端当前可见屏幕的完整渲染内容（适用于 top、vim 等TUI程序）。当命令输出是交互式界面而非普通文本流时，使用此工具代替 get_output。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "close",
			Description: "关闭指定的终端会话并释放资源。操作完成后应主动调用，避免资源泄漏。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "rename",
			Description: "重命名终端会话，便于区分和管理多个终端。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
					"new_name":   {Type: "string", Description: "新名称"},
				},
				Required: []string{"session_id", "new_name"},
			},
		},
	}
}

// ==================== Tool 处理函数 ====================

// HandleMCPTool 分发给具体的工具处理函数
func HandleMCPTool(pool *SessionPool, name string, args map[string]interface{}) ToolResult {
	switch name {
	case "environment_info":
		return handleTerminalEnvironmentInfo()
	case "create":
		return handleTerminalCreate(pool, args)
	case "list":
		return handleTerminalList(pool)
	case "send_input":
		return handleTerminalSendInput(pool, args)
	case "get_output":
		return handleTerminalGetOutput(pool, args)
	case "get_screen":
		return handleTerminalGetScreen(pool, args)
	case "close":
		return handleTerminalClose(pool, args)
	case "rename":
		return handleTerminalRename(pool, args)
	default:
		return errorResult(fmt.Sprintf("unknown tool: %s", name))
	}
}

func handleTerminalEnvironmentInfo() ToolResult {
	shell := GetDefaultShell()
	info := fmt.Sprintf("操作系统: %s\n架构: %s\n默认Shell: %s\nShell参数: %v",
		runtime.GOOS, runtime.GOARCH, shell.Path, shell.Args)
	return textResult(info)
}

func handleTerminalCreate(pool *SessionPool, args map[string]interface{}) ToolResult {
	name := getStringArg(args, "name")
	opener := getStringArg(args, "opener")
	purpose := getStringArg(args, "purpose")
	rows := getIntArg(args, "rows", 40)
	cols := getIntArg(args, "cols", 120)
	workDir := getStringArg(args, "working_directory")

	if opener == "" || purpose == "" {
		return errorResult("opener 和 purpose 是必填参数")
	}
	if workDir == "" {
		return errorResult("working_directory 是必填参数")
	}

	session, err := pool.CreateWithMeta(name, "", opener, purpose, uint16(rows), uint16(cols), workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("创建终端失败: %v", err))
	}

	result := fmt.Sprintf("终端创建成功\n会话ID: %s\n名称: %s\n打开者: %s\n目的: %s\n尺寸: %dx%d\n工作目录: %s",
		session.ID, session.Name, session.Opener, session.Purpose, rows, cols, workDir)
	return textResult(result)
}

func handleTerminalList(pool *SessionPool) ToolResult {
	sessions := pool.List()
	if len(sessions) == 0 {
		return textResult("当前没有已启用的终端")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("共 %d 个终端:\n\n", len(sessions)))
	for _, s := range sessions {
		s.mu.Lock()
		status := string(s.Status)
		name := s.Name
		created := s.CreatedAt.Format("2006-01-02 15:04:05")
		opener := s.Opener
		purpose := s.Purpose
		connCount := len(s.conns)
		fixed := "动态"
		if s.FixedRows > 0 && s.FixedCols > 0 {
			fixed = fmt.Sprintf("%dx%d", s.FixedRows, s.FixedCols)
		}
		s.mu.Unlock()

		sb.WriteString(fmt.Sprintf("ID: %s\n", s.ID))
		sb.WriteString(fmt.Sprintf("  名称: %s\n", name))
		sb.WriteString(fmt.Sprintf("  状态: %s\n", status))
		sb.WriteString(fmt.Sprintf("  创建时间: %s\n", created))
		sb.WriteString(fmt.Sprintf("  打开者: %s\n", opener))
		sb.WriteString(fmt.Sprintf("  目的: %s\n", purpose))
		sb.WriteString(fmt.Sprintf("  尺寸: %s\n", fixed))
		sb.WriteString(fmt.Sprintf("  页面连接数: %d\n", connCount))
		sb.WriteString("\n")
	}
	return textResult(sb.String())
}

func handleTerminalSendInput(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	inputType := getStringArg(args, "input_type")
	data := getStringArg(args, "data")
	submit := getBoolArg(args, "submit", false)

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	session, ok := pool.Get(sessionID)
	if !ok {
		return errorResult("终端不存在")
	}

	session.mu.Lock()
	if session.Status == SessionExited {
		session.mu.Unlock()
		return errorResult("终端进程已退出")
	}
	session.mu.Unlock()

	var inputData []byte
	switch inputType {
	case "text":
		inputData = []byte(data)
		if submit {
			inputData = append(inputData, '\r')
		}
	case "key":
		seq, err := ParseKeyInput(data)
		if err != nil {
			return errorResult(fmt.Sprintf("按键解析失败: %v", err))
		}
		inputData = seq
	default:
		return errorResult("input_type 必须是 text 或 key")
	}

	if err := session.MCPWrite(inputData); err != nil {
		return errorResult(fmt.Sprintf("发送输入失败: %v", err))
	}
	return textResult("输入已发送")
}

func handleTerminalGetOutput(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	lines := getIntArg(args, "lines", 50)

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	session, ok := pool.Get(sessionID)
	if !ok {
		return errorResult("终端不存在")
	}

	output := session.GetOutput()
	clean := StripANSI(output)

	// 按行分割，取最后N行
	allLines := strings.Split(clean, "\n")
	// 过滤空行并处理\r
	var filtered []string
	for _, line := range allLines {
		line = strings.TrimRight(line, "\r")
		filtered = append(filtered, line)
	}

	start := 0
	if len(filtered) > lines {
		start = len(filtered) - lines
	}
	result := strings.Join(filtered[start:], "\n")
	return textResult(result)
}

func handleTerminalGetScreen(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	session, ok := pool.Get(sessionID)
	if !ok {
		return errorResult("终端不存在")
	}

	if session.vtScreen != nil {
		screen := session.vtScreen.GetScreen()
		return textResult(screen)
	}

	// 如果没有VT模拟器，回退到获取输出最后N行
	output := session.GetOutput()
	clean := StripANSI(output)
	return textResult(clean)
}

func handleTerminalClose(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	if err := pool.Close(sessionID); err != nil {
		return errorResult(fmt.Sprintf("关闭终端失败: %v", err))
	}
	return textResult("终端已关闭")
}

func handleTerminalRename(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	newName := getStringArg(args, "new_name")

	if sessionID == "" || newName == "" {
		return errorResult("session_id 和 new_name 是必填参数")
	}

	if err := pool.Rename(sessionID, newName); err != nil {
		return errorResult(fmt.Sprintf("重命名失败: %v", err))
	}
	return textResult(fmt.Sprintf("终端已重命名为: %s", newName))
}

// ==================== 参数辅助函数 ====================

func getStringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case json.Number:
			return val.String()
		default:
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}

func getIntArg(args map[string]interface{}, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case int64:
			return int(val)
		case string:
			if n, err := strconv.Atoi(val); err == nil {
				return n
			}
		case json.Number:
			if n, err := val.Int64(); err == nil {
				return int(n)
			}
		}
	}
	return defaultVal
}

func getBoolArg(args map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			lower := strings.ToLower(val)
			if lower == "true" || lower == "1" || lower == "yes" {
				return true
			}
			if lower == "false" || lower == "0" || lower == "no" {
				return false
			}
		case float64:
			return val != 0
		case int:
			return val != 0
		case json.Number:
			if n, err := val.Int64(); err == nil {
				return n != 0
			}
		}
	}
	return defaultVal
}
