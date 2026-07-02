package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ==================== MCP Tool 定义 ====================

// MCPTool 表示一个MCP工具
type MCPTool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

// ToolSchema JSON Schema for tool input
type ToolSchema struct {
	Type       string                  `json:"type"`
	Properties map[string]ToolProperty `json:"properties"`
	Required   []string                `json:"required"`
}

// ToolProperty JSON Schema property
type ToolProperty struct {
	Type        string      `json:"type,omitempty"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// ToolResult 工具调用结果
type ToolResult struct {
	Content           []ToolContent `json:"content"`
	StructuredContent interface{}   `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
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

func structuredTextResult(text string, structured interface{}) ToolResult {
	return ToolResult{
		Content:           []ToolContent{{Type: "text", Text: text}},
		StructuredContent: structured,
	}
}

func errorResult(errMsg string) ToolResult {
	return ToolResult{
		Content: []ToolContent{{Type: "text", Text: errMsg}},
		IsError: true,
	}
}

// AllMCPTools 返回所有可用的MCP工具定义
// create 工具的描述会根据当前系统检测到的可用Shell动态生成
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
			Description: "创建并启动一个新的命令行终端（Shell）。当你需要在服务器上执行命令、运行脚本、操作文件系统或进行任何Shell交互时，必须首先调用此工具创建终端。创建成功后，使用 send_input 发送命令，使用 get_output 获取结果。\\n\\n" + GetAvailableShellsDescription(),
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"name":              {Type: "string", Description: "终端名称（必填，建议使用中文名称，如'编译环境'、'文件查看'）"},
					"opener":            {Type: "string", Description: "打开者名称（如AI Agent名称）"},
					"purpose":           {Type: "string", Description: "打开目的/用途描述"},
					"shell":             {Type: "string", Description: "要使用的Shell名称标识（可选），如 pwsh、cmd、bash、zsh 等。若不指定，则使用系统默认Shell。可用Shell列表见工具描述。", Default: ""},
					"rows":              {Type: "integer", Description: "终端行数（可选，默认40）", Default: 40},
					"cols":              {Type: "integer", Description: "终端列数（可选，默认120）", Default: 120},
					"working_directory": {Type: "string", Description: "终端初始工作目录"},
				},
				Required: []string{"name", "opener", "purpose", "working_directory"},
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
			Description: "向已创建的终端发送原始输入（按键或文本），不自动回车、不等待输出。适合逐键输入、交互式程序操作和需要精确控制输入流的场景。若要执行一条命令，请使用 send_command。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
					"input_type": {Type: "string", Description: "输入类型: text 或 key", Enum: []string{"text", "key"}},
					"data":       {Type: "string", Description: "输入内容。text类型时直接发送文本；key类型时填写键名如 Enter, ArrowUp, Ctrl+C, Escape 等"},
				},
				Required: []string{"session_id", "input_type", "data"},
			},
		},
		{
			Name:        "send_command",
			Description: "发送一条命令并立即回车执行，但不等待输出。适合已经决定执行命令、随后再用 wait_output 观察结果的场景，可显著降低误把等待和发送绑定在一起的风险。返回结构化确认信息，推荐后续调用 wait_output。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id": {Type: "string", Description: "终端会话ID"},
					"command":    {Type: "string", Description: "要发送的命令，会自动追加回车执行"},
				},
				Required: []string{"session_id", "command"},
			},
		},
		{
			Name:        "wait_output",
			Description: "不发送任何输入，只等待指定终端输出静默后返回当前稳定视图。适合 send_command 启动后的长命令、构建、下载等任务。返回 text 摘要和 structuredContent(output.v1)。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id":        {Type: "string", Description: "终端会话ID"},
					"wait_timeout_ms":   {Type: "integer", Description: "等待超时时间（毫秒），默认30000", Default: 30000},
					"idle_threshold_ms": {Type: "integer", Description: "输出静默阈值（毫秒），默认1000", Default: 1000},
					"lines":             {Type: "integer", Description: "返回的最大行数（默认80）", Default: 80},
					"raw_output":        {Type: "boolean", Description: "是否返回原始输出清理结果。默认false", Default: false},
					"full_output":       {Type: "boolean", Description: "是否尽量返回RingBuffer中的完整输出。默认false", Default: false},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "get_output",
			Description: "获取终端命令执行的输出结果。默认返回最终屏幕或去重后的尾部稳定文本，并附带截断、行数、动态刷新检测等元信息；同时提供 structuredContent 便于AI解析。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id":  {Type: "string", Description: "终端会话ID"},
					"lines":       {Type: "integer", Description: "获取行数（可选，默认50）", Default: 50},
					"raw_output":  {Type: "boolean", Description: "是否返回完整原始输出清理结果。默认false，返回上下文友好的稳定视图", Default: false},
					"full_output": {Type: "boolean", Description: "是否尽量返回RingBuffer中的完整输出。默认false；true时自动启用raw_output并取消默认行数/字符截断", Default: false},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "get_screen",
			Description: "获取终端当前可见屏幕的完整渲染内容（适用于 top、vim 等TUI程序），并附带输出元信息与 structuredContent。",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"session_id":  {Type: "string", Description: "终端会话ID"},
					"raw_output":  {Type: "boolean", Description: "是否回退返回完整原始输出清理结果。默认false，优先返回最终屏幕", Default: false},
					"full_output": {Type: "boolean", Description: "是否尽量返回RingBuffer中的完整输出。默认false；true时自动启用raw_output并取消默认行数/字符截断", Default: false},
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
	case "send_command":
		return handleTerminalSendCommand(pool, args)
	case "wait_output":
		return handleTerminalWaitOutput(pool, args)
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
	return structuredTextResult(info, map[string]interface{}{
		"tool": "environment_info",
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
		"shell": map[string]interface{}{
			"path": shell.Path,
			"args": shell.Args,
		},
	})
}

func handleTerminalCreate(pool *SessionPool, args map[string]interface{}) ToolResult {
	name := getStringArg(args, "name")
	opener := getStringArg(args, "opener")
	purpose := getStringArg(args, "purpose")
	shellName := getStringArg(args, "shell")
	rows := getIntArg(args, "rows", 40)
	cols := getIntArg(args, "cols", 120)
	workDir := getStringArg(args, "working_directory")

	if opener == "" || purpose == "" {
		return errorResult("opener 和 purpose 是必填参数")
	}
	if workDir == "" {
		return errorResult("working_directory 是必填参数")
	}

	session, err := pool.CreateWithMeta(name, shellName, opener, purpose, uint16(rows), uint16(cols), workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("创建终端失败: %v", err))
	}

	shellInfo := "默认Shell"
	if shellName != "" {
		if s := FindAvailableShellByName(shellName); s != nil {
			shellInfo = fmt.Sprintf("%s (%s)", s.DisplayName, s.Path)
		} else {
			shellInfo = shellName
		}
	}

	result := fmt.Sprintf("终端创建成功\n会话ID: %s\n名称: %s\n打开者: %s\n目的: %s\nShell: %s\n尺寸: %dx%d\n工作目录: %s",
		session.ID, session.Name, session.Opener, session.Purpose, shellInfo, rows, cols, workDir)
	return structuredTextResult(result, map[string]interface{}{
		"tool":       "create",
		"sessionId":  session.ID,
		"name":       session.Name,
		"opener":     session.Opener,
		"purpose":    session.Purpose,
		"shell":      shellInfo,
		"rows":       rows,
		"cols":       cols,
		"workingDir": workDir,
		"status":     string(session.Status),
	})
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
	return structuredTextResult(sb.String(), map[string]interface{}{
		"tool":     "list",
		"count":    len(sessions),
		"sessions": sessions,
	})
}

func handleTerminalSendInput(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	inputType := getStringArg(args, "input_type")
	data := getStringArg(args, "data")

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

	return structuredTextResult("输入已发送", map[string]interface{}{
		"schema":    "grt.mcp.input.v1",
		"kind":      "terminal_input",
		"tool":      "send_input",
		"sessionId": sessionID,
		"inputType": inputType,
		"sent":      true,
		"nextTool":  "get_screen",
	})
}

func handleTerminalSendCommand(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	command := getStringArg(args, "command")

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}
	if command == "" {
		return errorResult("command 是必填参数")
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

	if err := session.MCPWriteSubmit([]byte(command)); err != nil {
		return errorResult(fmt.Sprintf("发送命令失败: %v", err))
	}

	view := RenderMCPOutput(session, session.GetOutput(), mcpOutputOptions(20, false, false))
	structured := buildMCPOutputPayload("send_command", sessionID, command, view)
	structured.Completed = false
	structured.Wait = false
	structured.Suggestion.PreferredTool = "wait_output"
	structured.Suggestion.Reason = "命令已发送，下一步应等待输出而不是重复发送"
	structured.Suggestion.ShortHint = "先 wait_output，再看结果"

	return structuredTextResult("命令已发送，请使用 wait_output 观察结果。", structured)
}

func handleTerminalWaitOutput(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	waitTimeoutMs := getIntArg(args, "wait_timeout_ms", 30000)
	idleThresholdMs := getIntArg(args, "idle_threshold_ms", 1000)
	lines := getIntArg(args, "lines", 80)
	rawOutput := getBoolArg(args, "raw_output", false)
	fullOutput := getBoolArg(args, "full_output", false)

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

	maxWait := time.Duration(waitTimeoutMs) * time.Millisecond
	idleThreshold := time.Duration(idleThresholdMs) * time.Millisecond
	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}
	if idleThreshold <= 0 {
		idleThreshold = time.Second
	}
	if idleThreshold > maxWait {
		idleThreshold = maxWait / 4
		if idleThreshold < 100*time.Millisecond {
			idleThreshold = 100 * time.Millisecond
		}
	}

	_, completed := session.WaitForIdle(idleThreshold, maxWait)
	view := RenderMCPOutput(session, session.GetOutput(), mcpOutputOptions(lines, rawOutput, fullOutput))
	structured := buildMCPOutputPayload("wait_output", sessionID, "", view)
	structured.Completed = completed
	structured.WaitTimeout = waitTimeoutMs

	if completed {
		return structuredTextResult(FormatMCPOutput("终端已静默，当前输出：", view), structured)
	}
	return structuredTextResult(FormatMCPOutput("等待超时，当前输出：", view)+"\n[提示：任务可能仍在运行，可继续调用 wait_output 或 get_screen]", structured)
}

func handleTerminalGetOutput(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	lines := getIntArg(args, "lines", 50)
	rawOutput := getBoolArg(args, "raw_output", false)
	fullOutput := getBoolArg(args, "full_output", false)

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	session, ok := pool.Get(sessionID)
	if !ok {
		return errorResult("终端不存在")
	}

	output := session.GetOutput()
	view := RenderMCPOutput(session, output, mcpOutputOptions(lines, rawOutput, fullOutput))
	return structuredTextResult(FormatMCPOutput("终端输出：", view), buildMCPOutputPayload("get_output", sessionID, "", view))
}

func handleTerminalGetScreen(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")
	rawOutput := getBoolArg(args, "raw_output", false)
	fullOutput := getBoolArg(args, "full_output", false)

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	session, ok := pool.Get(sessionID)
	if !ok {
		return errorResult("终端不存在")
	}

	if rawOutput || fullOutput {
		view := RenderMCPOutput(session, session.GetOutput(), mcpOutputOptions(200, rawOutput, fullOutput))
		return structuredTextResult(FormatMCPOutput("终端原始输出：", view), buildMCPOutputPayload("get_screen", sessionID, "", view))
	}

	if session.vtScreen != nil {
		view := RenderMCPOutput(session, []byte(session.vtScreen.GetScreen()), MCPOutputOptions{Lines: int(session.FixedRows), PreferScreen: false})
		view.Meta.FromScreen = true
		return structuredTextResult(FormatMCPOutput("终端当前屏幕：", view), buildMCPOutputPayload("get_screen", sessionID, "", view))
	}

	// 如果没有VT模拟器，回退到获取输出最后N行
	view := RenderMCPOutput(session, session.GetOutput(), MCPOutputOptions{Lines: 80, PreferScreen: false})
	return structuredTextResult(FormatMCPOutput("终端当前输出：", view), buildMCPOutputPayload("get_screen", sessionID, "", view))
}

func handleTerminalClose(pool *SessionPool, args map[string]interface{}) ToolResult {
	sessionID := getStringArg(args, "session_id")

	if sessionID == "" {
		return errorResult("session_id 是必填参数")
	}

	if err := pool.Close(sessionID); err != nil {
		return errorResult(fmt.Sprintf("关闭终端失败: %v", err))
	}
	return structuredTextResult("终端已关闭", map[string]interface{}{
		"tool":      "close",
		"sessionId": sessionID,
		"closed":    true,
	})
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
	return structuredTextResult(fmt.Sprintf("终端已重命名为: %s", newName), map[string]interface{}{
		"tool":      "rename",
		"sessionId": sessionID,
		"newName":   newName,
		"renamed":   true,
	})
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

func mcpOutputOptions(lines int, rawOutput, fullOutput bool) MCPOutputOptions {
	if fullOutput {
		return MCPOutputOptions{Lines: 1 << 30, MaxChars: 1 << 30, Raw: true, PreferScreen: false}
	}
	return MCPOutputOptions{Lines: lines, Raw: rawOutput, PreferScreen: true}
}

func buildMCPOutputPayload(toolName, sessionID, command string, view MCPOutputView) MCPOutputEnvelope {
	return MCPOutputEnvelope{
		Schema:    "grt.mcp.output.v1",
		Kind:      "terminal_output",
		Tool:      toolName,
		SessionID: sessionID,
		Command:   command,
		Completed: !view.Meta.Truncated,
		Output:    view,
		Suggestion: MCPOutputSuggestion{
			PreferredTool: preferredMCPTool(view),
			Reason:        suggestedMCPReason(view),
			ShortHint:     suggestedMCPHint(view),
		},
	}
}

func preferredMCPTool(view MCPOutputView) string {
	if view.Meta.FromScreen {
		return "get_screen"
	}
	if view.Meta.DynamicDetected {
		return "wait_output"
	}
	if view.Meta.Truncated {
		return "wait_output"
	}
	return "get_output"
}

func suggestedMCPReason(view MCPOutputView) string {
	if view.Meta.FromScreen {
		return "当前是最终屏幕视图，最适合观察 TUI 状态"
	}
	if view.Meta.DynamicDetected {
		return "检测到动态刷新或进度条，建议只等待稳定输出"
	}
	if view.Meta.Truncated {
		return "结果已截断，建议继续等待或获取更完整输出"
	}
	return "当前输出已经稳定"
}

func suggestedMCPHint(view MCPOutputView) string {
	if view.Meta.FromScreen {
		return "优先看屏幕最后一帧"
	}
	if view.Meta.DynamicDetected {
		return "先用 wait_output 收敛，再决定是否 full_output"
	}
	if view.Meta.Truncated {
		return "若要完整日志，使用 full_output=true"
	}
	return "继续使用 get_output 即可"
}

// appendInputNewline 追加回车符（所有平台统一使用 \r，由 MCPWriteSubmit 负责处理平台差异）
func appendInputNewline(data []byte) []byte {
	return append(data, '\r')
}
