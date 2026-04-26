# Go Remote Terminal

[English](#english) | [中文](#chinese)

<a name="english"></a>
## English

A lightweight, cross-platform Web Terminal server written in Go. Access your local shell from any device via a browser — no client installation required.

### Features

- **Cross-Platform**: Supports Windows 10/11, macOS, and Linux. Build for any target platform from any host.
- **Zero-Client**: Pure browser-based access. Works on desktop, tablet, and mobile.
- **Session Persistence**: Shell processes keep running after the browser disconnects. Reconnect anytime to resume.
- **Multi-User Sharing**: Multiple clients can connect to the same session simultaneously with:
  - Per-connection random name & color identifiers
  - Single focus owner (input control), others are read-only observers
  - Focus stealing via "Take Control" button
  - Real-time connection count badges
- **TUI Support**: Full ANSI escape sequence support via `xterm.js`. Run `vim`, `htop`, `winget`, etc. flawlessly.
- **Security**: Token-based authentication with admin and read-only token levels.
- **Mobile-Ready**: Virtual keyboard with `Esc`, `Tab`, `Ctrl`, `Alt`, `Shift`, arrows, and paste support. Long-press to lock modifier keys.
- **Quick Commands**: Customizable command drawer with localStorage persistence.
- **Search**: In-terminal search with `Ctrl+Shift+F`.
- **Export**: Save terminal output to a text file.
- **Rate Limiting**: Per-connection token bucket (100KB/s sustained, 500KB burst).
- **Protocol v1**: Hybrid JSON + Binary WebSocket protocol for minimal overhead.

### Quick Start

```bash
# Build
go build -o go-remote-terminal .

# Run with a token
./go-remote-terminal -t your-secure-token

# Or use environment variables
GRT_TOKEN=your-secure-token ./go-remote-terminal
```

Then open `http://localhost:8080` in your browser and enter the token.

### Docker

```bash
docker build -t go-remote-terminal .
docker run -p 8080:8080 -e GRT_TOKEN=your-token go-remote-terminal
```

### Usage

```
Usage: go-remote-terminal [options]

Options:
  -h, --host string    Listen host (default "0.0.0.0")
  -p, --port string    Listen port (default "8080")
  -t, --token string   Admin access token (required)
  --ro-token string    Read-only token (optional)
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `GRT_HOST` | Listen host |
| `GRT_PORT` | Listen port |
| `GRT_TOKEN` | Admin token |
| `GRT_RO_TOKEN` | Read-only token |

### Building from Source

```bash
# Local build
go build .

# Cross-compilation
GOOS=linux   GOARCH=amd64 go build -o dist/go-remote-terminal-linux-amd64
GOOS=linux   GOARCH=arm64 go build -o dist/go-remote-terminal-linux-arm64
GOOS=darwin  GOARCH=amd64 go build -o dist/go-remote-terminal-darwin-amd64
GOOS=darwin  GOARCH=arm64 go build -o dist/go-remote-terminal-darwin-arm64
GOOS=windows GOARCH=amd64 go build -o dist/go-remote-terminal-windows-amd64.exe
```

### Architecture

```
┌─────────────┐      WebSocket      ┌─────────────────────────────────────┐
│   Browser   │ ◄─────────────────► │  Go Remote Terminal Server          │
│  (xterm.js) │   HTTP (static)     │  ├─ Gin HTTP Server                 │
└─────────────┘                     │  ├─ Session Pool (sync.Map)         │
                                    │  ├─ PTY Handler (creack/pty)        │
                                    │  ├─ Rate Limiter (token bucket)     │
                                    │  └─ Focus Manager                   │
                                    └─────────────────────────────────────┘
```

### Tech Stack

- **Backend**: Go 1.20+, Gin, Gorilla WebSocket, creack/pty
- **Frontend**: Vanilla JS, xterm.js 5.3.0, xterm-addon-fit, xterm-addon-search
- **Protocol**: v1 Hybrid (JSON text frames for control, binary frames for I/O)

### License

[Apache License 2.0](LICENSE)

---

<a name="chinese"></a>
## 中文

Go Remote Terminal 是一个轻量级、跨平台的 Web 终端服务程序。只需在目标机器上运行一个二进制文件，即可通过浏览器从任意设备远程访问本地 Shell，无需安装任何客户端。

### 功能特性

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
- **高效协议**：v1 混合协议，控制消息用 JSON，输入输出用 Binary Frame，零 Base64 开销。

### 快速开始

```bash
# 编译
go build -o go-remote-terminal .

# 运行（指定 Token）
./go-remote-terminal -t your-secure-token

# 或使用环境变量
GRT_TOKEN=your-secure-token ./go-remote-terminal
```

然后在浏览器中打开 `http://localhost:8080` 并输入 Token 即可。

### Docker 运行

```bash
docker build -t go-remote-terminal .
docker run -p 8080:8080 -e GRT_TOKEN=your-token go-remote-terminal
```

### 命令行参数

```
用法: go-remote-terminal [选项]

选项:
  -h, --host string    监听地址 (默认 "0.0.0.0")
  -p, --port string    监听端口 (默认 "8080")
  -t, --token string   管理 Token（必填）
  --ro-token string    只读 Token（可选）
```

### 环境变量

| 变量名 | 说明 |
|--------|------|
| `GRT_HOST` | 监听地址 |
| `GRT_PORT` | 监听端口 |
| `GRT_TOKEN` | 管理 Token |
| `GRT_RO_TOKEN` | 只读 Token |

### 源码构建

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

### 系统架构

```
┌─────────────┐      WebSocket      ┌─────────────────────────────────────┐
│   浏览器     │ ◄─────────────────► │  Go Remote Terminal 服务端          │
│  (xterm.js) │   HTTP (静态页面)    │  ├─ Gin HTTP 服务器                  │
└─────────────┘                     │  ├─ 会话池 (sync.Map)                │
                                    │  ├─ PTY 处理器 (creack/pty)          │
                                    │  ├─ 速率限制器 (令牌桶)               │
                                    │  └─ 焦点管理器                        │
                                    └─────────────────────────────────────┘
```

### 技术栈

- **后端**: Go 1.20+, Gin, Gorilla WebSocket, creack/pty
- **前端**: 原生 JavaScript, xterm.js 5.3.0, xterm-addon-fit, xterm-addon-search
- **协议**: v1 混合协议（控制消息用 JSON 文本帧，输入输出用二进制帧）

### 开源协议

[Apache License 2.0](LICENSE)
