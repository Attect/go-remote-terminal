# AGENTS.md — go-remote-terminal

## 项目背景

这是一个用 Go 编写的轻量级跨平台 Web 终端服务器。核心能力：在目标机器上启动一个 HTTP 服务，通过浏览器 WebSocket 连接到一个本地 PTY Shell，实现远程终端访问。

## 目录结构

```
.
├── main.go           # 入口：配置解析、Gin 引擎、优雅关闭
├── config.go         # 命令行参数与环境变量配置
├── auth.go           # Bearer Token 认证（Admin / ReadOnly）
├── handler.go        # Gin HTTP handler + WebSocket handler
├── session.go        # Session（终端会话）、SessionPool、RingBuffer
├── pty.go            # PtyProcess 跨平台接口
├── pty_unix.go       # Linux/macOS PTY 实现（creack/pty）
├── pty_windows.go    # Windows PTY 实现（conpty）
├── shell.go          # Shell 自动检测（Windows PowerShell → cmd / Linux $SHELL → bash / macOS zsh → bash）
├── message.go        # WebSocket v1 协议消息定义
├── ratelimit.go      # 令牌桶速率限制
├── mcp.go            # MCP SSE 传输层 + JSON-RPC 2.0
├── mcp_tools.go      # 8 个 MCP 工具实现
├── mcp_input.go      # 键盘模拟输入映射
├── ansi.go           # ANSI 控制符去除 + VTScreen 虚拟终端
├── static/           # 前端静态资源（index.html, css, js）
└── build.sh / build.bat / Dockerfile
```

## 构建

```bash
# 本地编译
go build .

# Windows 交叉编译（从任意平台）
GOOS=windows GOARCH=amd64 go build -o go-remote-terminal-windows-amd64.exe .
```

## 运行

```bash
# 必须指定管理 Token
./go-remote-terminal -t your-secure-token

# 或环境变量
GRT_TOKEN=your-secure-token ./go-remote-terminal
```

访问 `http://localhost:8080`，输入 Token 进入终端页面。

## MCP 服务（SSE 传输）

项目已集成 MCP (Model Context Protocol) 2024-11-05 规范的 SSE 传输服务，为 AI Agent 提供终端管理能力。

### 端点

- `GET /mcp/sse` — 建立 SSE 长连接（需 Bearer Token）
- `POST /mcp/message?sid=<id>` — 发送 JSON-RPC 请求

### 工具列表

工具名无前缀：

| 工具 | 说明 |
|------|------|
| `environment_info` | 获取当前运行环境的基础信息（OS、架构、默认Shell）。建议首次使用终端前调用 |
| `create` | **创建终端（执行命令的第一步）**。当你需要在服务器上执行命令、运行脚本或进行Shell交互时，必须首先调用此工具 |
| `list` | 列出所有活跃终端，用于查看已有会话 |
| `send_input` | **发送命令并执行**。创建终端后，使用此工具发送命令文本（如 `ls -la`）或模拟按键（Ctrl+C、Enter等） |
| `get_output` | **获取命令输出**。发送命令后，调用此工具读取返回的文本结果（去ANSI，默认最后50行） |
| `get_screen` | 获取终端当前可见屏幕（适用于 top/vim 等TUI程序），自动去除ANSI控制符 |
| `close` | 关闭终端并释放资源，操作完成后应主动调用 |
| `rename` | 重命名终端，便于管理多个会话 |

### 使用工作流

AI Agent 执行命令的标准流程：
1. `terminal_create` — 创建终端（指定 working_directory、opener、purpose）
2. `terminal_send_input` — 发送命令（input_type=text, submit=true）
3. `terminal_get_output` — 读取输出结果
4. `terminal_close` — 操作完成后关闭终端释放资源

### MCP 终端与普通终端的区别

- MCP 终端有**固定尺寸**（默认 40×120），不随前端页面用户连接尺寸变化
- Agent 输入**直接写 PTY**，不参与 WebSocket 焦点竞争
- MCP 终端**可被前端页面查看和连接**，WebSocket 用户需申请焦点后才能输入
- 所有终端（无论 MCP 还是 WebSocket 创建）共享同一个 SessionPool

## 编码约定

- Go 1.20，不使用泛型
- 错误处理：优先返回具体错误，日志记录使用 `log.Printf("[模块] 格式", ...)`
- 并发：Session 内使用 `sync.Mutex`，SessionPool 使用 `sync.Map`
- 新增功能尽量保持向后兼容，不破坏现有 WebSocket / REST API
