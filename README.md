# Go Remote Terminal

Go Remote Terminal 是一个轻量级、跨平台的 Web 终端服务程序。只需在目标机器上运行一个二进制文件，即可通过浏览器从任意设备远程访问本地 Shell，无需安装任何客户端。

## 功能特性

- **跨平台支持**：支持 Windows 10/11、macOS、Linux。可在任意平台上交叉编译出所有目标平台的二进制文件。
- **纯浏览器访问**：无需安装客户端 App，桌面端、平板、手机均可通过浏览器访问。
- **会话持久化**：浏览器断开连接后，后台 Shell 进程继续运行。重新连接即可恢复之前的会话状态。
- **多用户共享协作**：
  - 同一 Session 支持多个客户端同时连接
  - 每个连接分配随机名称和颜色标识
  - 单一输入焦点控制，其他连接为只读观察模式
  - 支持主动抢占焦点
  - 实时显示连接数和焦点归属
- **TUI 完美支持**：基于 `xterm.js` 完整支持 ANSI 转义序列，可流畅运行 `vim`、`htop`、`winget` 等 TUI 程序。
- **双 Token 安全认证**：支持管理 Token（完整权限）和只读 Token（仅接收输出）。
- **移动端优化**：
  - 虚拟键盘支持 `Esc`、`Tab`、`Ctrl`、`Alt`、`Shift`、方向键和粘贴
  - 长按修饰键可锁定状态，再次点击或长按解锁
  - 快捷命令抽屉，支持自定义常用命令
- **终端搜索**：`Ctrl+Shift+F` 呼出搜索框。
- **终端导出**：一键将终端输出保存为文本文件。
- **速率限制**：每个连接独立令牌桶限流（100KB/s 持续，500KB 突发）。
- **MCP 服务（SSE）**：完整支持 MCP 2024-11-05 规范的 SSE 传输。AI Agent 可通过 8 个标准工具远程创建、控制和检视终端（`terminal_create`、`terminal_send_input`、`terminal_get_screen` 等）。
- **高效协议**：v1 混合协议，控制消息用 JSON，输入输出用 Binary Frame，零 Base64 开销。

## 快速开始

```bash
# 编译
go build -o go-remote-terminal .

# 运行（指定 Token）
./go-remote-terminal -t your-secure-token

# 或使用环境变量
GRT_TOKEN=your-secure-token ./go-remote-terminal
```

然后在浏览器中打开 `http://localhost:8080` 并输入 Token 即可。

## Docker 运行

```bash
docker build -t go-remote-terminal .
docker run -p 8080:8080 -e GRT_TOKEN=your-token go-remote-terminal
```

## 命令行参数

```
用法: go-remote-terminal [选项]

选项:
  --host string        监听地址 (默认 "0.0.0.0")
  --port int           监听端口 (默认 8080)
  -t, --token string   管理 Token（必填）
  --ro-token string    只读 Token（可选）
```

## 环境变量

| 变量名 | 说明 |
|--------|------|
| `GRT_HOST` | 监听地址 |
| `GRT_PORT` | 监听端口 |
| `GRT_TOKEN` | 管理 Token |
| `GRT_RO_TOKEN` | 只读 Token |

## 源码构建

```bash
# 本地构建
go build .

# 交叉编译
GOOS=linux   GOARCH=amd64 go build -o dist/go-remote-terminal-linux-amd64
GOOS=linux   GOARCH=arm64 go build -o dist/go-remote-terminal-linux-arm64
GOOS=darwin  GOARCH=amd64 go build -o dist/go-remote-terminal-darwin-amd64
GOOS=darwin  GOARCH=arm64 go build -o dist/go-remote-terminal-darwin-arm64
GOOS=windows GOARCH=amd64 go build -o dist/go-remote-terminal-windows-amd64.exe
```

## MCP 配置与连接

本服务内置 MCP (Model Context Protocol) 2024-11-05 服务端，AI Agent 可通过 SSE 传输方式远程管理终端。

### 连接端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/mcp/sse` | `GET` | 建立 SSE 长连接，需携带 Bearer Token |
| `/mcp/message?sid=<id>` | `POST` | 发送 JSON-RPC 请求（SSE transport，通过 sid 认证） |

### 连接流程

1. Agent 向 `GET /mcp/sse` 发送请求，Header 携带 `Authorization: Bearer <管理Token>`
2. 服务端返回 SSE 流，首条事件为 `event: endpoint`，data 为消息 POST 端点（如 `/mcp/message?sid=abc123`）
3. Agent 向该 POST 端点发送 JSON-RPC 请求（如 `tools/list`、`tools/call`）
4. 服务端通过 SSE 流的 `event: message` 返回 JSON-RPC 响应

### MCP 工具列表

| 工具名 | 功能 |
|--------|------|
| `terminal_environment_info` | 获取环境信息（默认 shell、操作系统、架构） |
| `terminal_create` | 创建终端（参数：name, opener, purpose, rows, cols） |
| `terminal_list` | 查询所有已启用的终端 |
| `terminal_send_input` | 发送输入（`input_type`: text / key，支持方向键、Ctrl、Alt 等） |
| `terminal_get_output` | 获取最后 N 行输出（自动去除 ANSI 控制符） |
| `terminal_get_screen` | 获取当前可见屏幕内容（适用于 TUI，自动去除 ANSI 控制符） |
| `terminal_close` | 关闭终端 |
| `terminal_rename` | 重命名终端 |

### 注意事项

- MCP 终端创建后**尺寸固定**（默认 40×120），不随前端页面查看尺寸变化
- Agent 输入**直接写 PTY**，不参与 WebSocket 用户的焦点竞争
- MCP 创建的终端**可被前端页面查看和连接**，页面用户需申请焦点后才能输入

## 系统架构

```
┌─────────────┐      WebSocket      ┌─────────────────────────────────────┐
│   浏览器     │ ◄─────────────────► │  Go Remote Terminal 服务端          │
│  (xterm.js) │   HTTP (静态页面)    │  ├─ Gin HTTP 服务器                  │
└─────────────┘                     │  ├─ 会话池 (sync.Map)                │
       │                            │  ├─ PTY 处理器 (creack/pty)          │
       │ SSE                          │  ├─ 速率限制器 (令牌桶)               │
       ▼                            │  ├─ 焦点管理器                        │
┌─────────────┐                     │  ├─ MCP 服务 (SSE + JSON-RPC)        │
│  AI Agent   │ ◄─────────────────► │  └─ VTScreen 虚拟终端模拟器           │
│   (MCP)     │                     └─────────────────────────────────────┘
└─────────────┘
```

## 技术栈

- **后端**: Go 1.20+, Gin, Gorilla WebSocket, creack/pty
- **前端**: 原生 JavaScript, xterm.js 5.3.0, xterm-addon-fit, xterm-addon-search
- **协议**: v1 混合协议（控制消息用 JSON 文本帧，输入输出用二进制帧）

## 开源协议

[Apache License 2.0](LICENSE)
