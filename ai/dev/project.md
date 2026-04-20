# 跨平台Web终端远程控制系统 — 项目架构文档

> 版本: v1.0  
> 创建日期: 2026-04-21  
> 创建人: code-framework  
> 变更日志: 初始版本

---

## 一、技术栈

| 层级 | 技术选型 | 版本 | 说明 |
|------|----------|------|------|
| 语言 | Go | 1.20+ | 后端开发语言，CGO_ENABLED=0纯Go编译 |
| Web框架 | gin-gonic/gin | v1.9+ | HTTP路由、中间件、静态资源托管 |
| WebSocket | gorilla/websocket | v1.5+ | WebSocket连接管理、消息收发 |
| Unix PTY | creack/pty | v1.1.21+ | Linux/macOS伪终端，纯Go无CGO |
| Windows PTY | UserExistsError/conpty | v1.0+ | Windows ConPTY API封装，纯Go无CGO无外部DLL |
| 前端终端 | xterm.js | v5.x | 浏览器端终端渲染 |
| 前端适配 | xterm-addon-fit | v0.8+ | 终端尺寸自适应 |
| 前端搜索 | xterm-addon-search | v0.15+ | 终端内容搜索（可选） |
| 前端构建 | 无框架/原生JS | - | 单页应用，不引入框架依赖 |
| 静态嵌入 | Go embed | - | 前端资源嵌入二进制文件 |

### 技术选型关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Windows PTY | UserExistsError/conpty | 纯Go实现，直接调用Windows ConPTY API(kernel32.dll)，无CGO依赖，无外部DLL，满足单文件二进制分发需求 |
| Unix PTY | creack/pty | Go生态标准PTY库，纯Go实现，无CGO依赖，社区活跃 |
| 跨平台方案 | Go build tags | 编译期平台区分，零运行时开销，类型安全 |
| 会话存储 | sync.Map + 内存 | 轻量级，无需外部数据库，进程内状态管理 |
| 前端方案 | 原生JS | 减少依赖体积，xterm.js本身功能完备，无需框架 |

---

## 二、目录结构

```
/go-remote-terminal
├── main.go                      # 程序入口：配置加载、路由注册、服务启动
├── go.mod                       # Go模块定义
├── go.sum                       # 依赖校验
├── config.go                    # 配置管理：命令行参数、环境变量、默认值
├── auth.go                      # Token认证：验证逻辑、中间件
├── session.go                   # 会话管理核心：Session结构体、SessionPool
├── handler.go                   # HTTP/WebSocket处理器：API路由、WebSocket升级
├── pty.go                       # PTY抽象接口定义（跨平台公共接口）
├── pty_unix.go                  # Unix PTY实现 [build: !windows]
├── pty_windows.go               # Windows ConPTY实现 [build: windows]
├── shell.go                     # Shell适配：根据GOOS选择Shell命令
├── message.go                   # WebSocket消息协议：消息类型定义、序列化
├── static/                      # 前端静态资源（Go embed嵌入）
│   ├── index.html               # 主页面
│   ├── css/
│   │   └── style.css            # 样式：终端、侧边栏、虚拟键盘
│   └── js/
│       ├── app.js               # 主逻辑：连接管理、会话切换
│       ├── terminal.js          # xterm.js封装：初始化、数据绑定
│       ├── keyboard.js          # 虚拟键盘：组合键、移动端检测
│       ├── sidebar.js           # 会话侧边栏：列表渲染、操作交互
│       └── export.js            # 终端输出导出：ANSI清理、文件下载
├── build.sh                     # Linux/macOS交叉编译脚本
├── build.bat                    # Windows交叉编译脚本
├── Makefile                     # 构建快捷命令
└── ai/                          # 工作文档目录（不参与构建）
    ├── dev/
    │   └── project.md           # 本文档
    ├── design_by_user_say.md    # 需求规格说明书
    └── tasks/                   # 任务管理
```

---

## 三、核心模块

### 模块职责划分

| 模块 | 文件 | 职责 |
|------|------|------|
| 入口 | main.go | 配置初始化、路由注册、服务启动、优雅关闭 |
| 配置 | config.go | 命令行参数解析、默认值设置、配置校验 |
| 认证 | auth.go | Token验证逻辑、Gin中间件、WebSocket握手拦截 |
| 会话管理 | session.go | Session生命周期、SessionPool、输出缓冲、重连绑定 |
| 通信处理 | handler.go | HTTP API处理、WebSocket升级、消息路由 |
| PTY抽象 | pty.go | PtyProcess接口定义（跨平台公共契约） |
| Unix PTY | pty_unix.go | creack/pty封装，实现PtyProcess接口 |
| Windows PTY | pty_windows.go | conpty封装，实现PtyProcess接口 |
| Shell适配 | shell.go | runtime.GOOS检测、Shell命令选择、回退策略 |
| 消息协议 | message.go | WebSocket消息类型定义、JSON序列化/反序列化 |
| 前端终端 | js/terminal.js | xterm.js初始化、WebSocket数据绑定、resize处理 |
| 虚拟键盘 | js/keyboard.js | 移动端检测、虚拟键盘渲染、组合键状态机 |
| 会话侧边栏 | js/sidebar.js | 会话列表CRUD、UI交互、实时更新 |
| 输出导出 | js/export.js | 终端buffer读取、ANSI转义码清理、文件下载 |

---

## 四、接口定义

### 4.1 PTY抽象接口（pty.go）

```go
// PtyProcess 定义跨平台PTY进程的统一接口
// 通过Go build tags在编译期选择具体实现
type PtyProcess interface {
    // Start 启动PTY进程
    //   - cmd: Shell命令路径
    //   - args: 命令参数
    //   - rows, cols: 初始终端尺寸
    // 返回: 启动失败时返回错误
    Start(cmd string, args []string, rows, cols uint16) error

    // Read 从PTY读取输出数据
    //   - p: 读缓冲区
    // 返回: 读取字节数和错误
    Read(p []byte) (n int, err error)

    // Write 向PTY写入输入数据
    //   - p: 写入数据
    // 返回: 写入字节数和错误
    Write(p []byte) (n int, err error)

    // Resize 调整PTY终端尺寸
    //   - rows, cols: 新的终端尺寸
    // 返回: 调整失败时返回错误
    Resize(rows, cols uint16) error

    // Wait 等待PTY进程退出
    // 返回: 进程退出码和错误
    Wait() (exitCode int, err error)

    // Close 关闭PTY，释放资源
    // 注意: Close会终止关联的子进程
    Close() error

    // Pid 返回PTY关联的子进程PID
    Pid() int

    // IsRunning 检查PTY进程是否仍在运行
    IsRunning() bool
}

// NewPtyProcess 创建新的PTY进程实例
// 由平台特定文件(pty_unix.go / pty_windows.go)提供实现
func NewPtyProcess() PtyProcess
```

### 4.2 会话管理接口（session.go）

```go
// Session 表示一个终端会话
type Session struct {
    ID        string       // 会话唯一标识，UUID格式
    Name      string       // 会话显示名称
    Pty       PtyProcess   // 关联的PTY进程
    CreatedAt time.Time    // 创建时间
    Status    SessionStatus // 会话状态
    mu        sync.Mutex   // 会话级别锁
    outputBuf *RingBuffer  // 终端输出环形缓冲区（用于重连回显和导出）
    writerWS  *WSConn      // 当前写入者WebSocket连接（仅允许一个）
    readerWS  []*WSConn    // 只读观察者WebSocket连接列表
    cancelFn  context.CancelFunc // PTY输出读取goroutine取消函数
}

// SessionStatus 会话状态枚举
type SessionStatus string

const (
    SessionActive  SessionStatus = "active"  // 进程运行中
    SessionExited  SessionStatus = "exited"  // 进程已退出
)

// SessionPool 管理所有活跃会话
type SessionPool struct {
    sessions sync.Map // map[sessionID]*Session
}

// NewSessionPool 创建会话池
func NewSessionPool() *SessionPool

// Create 创建新会话
//   - name: 会话名称（可选，默认自动生成）
//   - shell: Shell路径（可选，默认自动检测）
// 返回: 创建的Session和错误
func (p *SessionPool) Create(name, shell string) (*Session, error)

// Get 获取指定会话
//   - id: 会话ID
// 返回: Session和是否存在
func (p *SessionPool) Get(id string) (*Session, bool)

// List 获取所有活跃会话列表
func (p *SessionPool) List() []*Session

// Close 关闭指定会话，终止子进程并释放资源
//   - id: 会话ID
// 返回: 错误信息
func (p *SessionPool) Close(id string) error

// Rename 重命名会话
//   - id: 会话ID
//   - newName: 新名称
// 返回: 错误信息
func (p *SessionPool) Rename(id, newName string) error

// AttachWriter 将WebSocket绑定到会话的写入端
//   - id: 会话ID
//   - ws: WebSocket连接
// 返回: 错误信息（SESSION_BUSY等）
func (s *Session) AttachWriter(ws *WSConn) error

// DetachWriter 解除WebSocket的写入绑定
//   - ws: 要解除的WebSocket连接
func (s *Session) DetachWriter(ws *WSConn)

// AttachReader 添加只读观察者
func (s *Session) AttachReader(ws *WSConn)

// DetachReader 移除只读观察者
func (s *Session) DetachReader(ws *WSConn)

// WriteOutput 向会话的输出缓冲区写入数据
// 同时推送到所有已连接的WebSocket
func (s *Session) WriteOutput(data []byte)

// GetOutput 获取输出缓冲区内容（用于重连回显和导出）
func (s *Session) GetOutput() []byte

// Cleanup 清理已退出会话的资源
func (p *SessionPool) Cleanup()
```

### 4.3 认证接口（auth.go）

```go
// TokenAuth Token认证器
type TokenAuth struct {
    token string // 配置的访问令牌
}

// NewTokenAuth 创建Token认证器
//   - token: 访问令牌，为空时拒绝所有连接
func NewTokenAuth(token string) *TokenAuth

// Validate 验证Token是否匹配
//   - provided: 客户端提供的Token
// 返回: 是否验证通过
func (a *TokenAuth) Validate(provided string) bool

// GinMiddleware 返回Gin中间件，用于HTTP API认证
// 从Header的Authorization: Bearer {token} 或 query参数token中提取
func (a *TokenAuth) GinMiddleware() gin.HandlerFunc

// WebSocketAuthFunc 返回WebSocket升级时的认证函数
// 从query参数token中提取并验证
func (a *TokenAuth) WebSocketAuthFunc() func(r *http.Request) bool

// IsConfigured 检查Token是否已配置
func (a *TokenAuth) IsConfigured() bool
```

### 4.4 WebSocket消息协议（message.go）

```go
// MessageType WebSocket消息类型
type MessageType string

const (
    // 客户端→服务端
    MsgInput     MessageType = "input"     // 终端输入字符
    MsgResize    MessageType = "resize"    // 终端尺寸变更

    // 服务端→客户端
    MsgOutput    MessageType = "output"    // 终端输出数据
    MsgError     MessageType = "error"     // 错误信息
    MsgSessionInfo MessageType = "session_info" // 会话元信息

    // 双向
    MsgPing      MessageType = "ping"      // 心跳检测
    MsgPong      MessageType = "pong"      // 心跳响应
)

// WSMessage WebSocket消息通用结构
type WSMessage struct {
    Type MessageType `json:"type"`           // 消息类型
    Data string      `json:"data"`           // 消息数据（Base64编码的原始字节，用于input/output）
    // 或JSON编码的结构化数据，用于其他类型
}

// ResizeData resize消息的数据结构
type ResizeData struct {
    Rows uint16 `json:"rows"` // 行数
    Cols uint16 `json:"cols"` // 列数
}

// SessionInfoData session_info消息的数据结构
type SessionInfoData struct {
    ID        string `json:"id"`         // 会话ID
    Name      string `json:"name"`       // 会话名称
    Status    string `json:"status"`     // 会话状态
    CreatedAt int64  `json:"created_at"` // 创建时间戳
}

// ErrorData error消息的数据结构
type ErrorData struct {
    Code    string `json:"code"`    // 错误码
    Message string `json:"message"` // 错误描述
}

// NewInputMessage 创建终端输入消息
func NewInputMessage(data []byte) WSMessage

// NewOutputMessage 创建终端输出消息
func NewOutputMessage(data []byte) WSMessage

// NewResizeMessage 创建resize消息
func NewResizeMessage(rows, cols uint16) WSMessage

// NewErrorMessage 创建错误消息
func NewErrorMessage(code, message string) WSMessage

// NewSessionInfoMessage 创建会话信息消息
func NewSessionInfoMessage(session *Session) WSMessage

// ParseMessage 解析WebSocket消息
func ParseMessage(raw []byte) (*WSMessage, error)
```

### 4.5 HTTP API接口（handler.go）

```go
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

// Handler HTTP/WebSocket处理器
type Handler struct {
    pool     *SessionPool  // 会话池
    tokenAuth *TokenAuth   // Token认证器
    upgrader  *websocket.Upgrader // WebSocket升级器
}

// NewHandler 创建处理器
func NewHandler(pool *SessionPool, tokenAuth *TokenAuth) *Handler

// RegisterRoutes 注册所有路由到Gin引擎
func (h *Handler) RegisterRoutes(r *gin.Engine)

// HandleSessions GET /api/sessions - 获取会话列表
func (h *Handler) HandleSessions(c *gin.Context)

// HandleCreateSession POST /api/sessions - 创建新会话
func (h *Handler) HandleCreateSession(c *gin.Context)

// HandleCloseSession DELETE /api/sessions/:id - 关闭会话
func (h *Handler) HandleCloseSession(c *gin.Context)

// HandleRenameSession PUT /api/sessions/:id/rename - 重命名会话
func (h *Handler) HandleRenameSession(c *gin.Context)

// HandleWebSocket GET /ws - WebSocket升级
// query参数: token(必需), session_id(可选，用于重连)
func (h *Handler) HandleWebSocket(c *gin.Context)
```

### 4.6 配置接口（config.go）

```go
// Config 应用配置
type Config struct {
    Host  string // 监听地址，默认 "0.0.0.0"
    Port  int    // 监听端口，默认 8080
    Token string // 访问令牌，必需
}

// ParseConfig 解析配置
// 优先级: 命令行参数 > 环境变量 > 默认值
// 环境变量: GRT_HOST, GRT_PORT, GRT_TOKEN
func ParseConfig() (*Config, error)

// Validate 校验配置合法性
func (c *Config) Validate() error
```

### 4.7 Shell适配接口（shell.go）

```go
// ShellConfig Shell配置
type ShellConfig struct {
    Path string   // Shell可执行文件路径
    Args []string // Shell启动参数
}

// DetectShell 根据当前操作系统自动检测并返回Shell配置
// Windows: powershell.exe -NoLogo -ExecutionPolicy Bypass
//   回退: cmd.exe
// Linux: $SHELL环境变量 或 /bin/bash
// macOS: /bin/zsh 或 /bin/bash
func DetectShell() *ShellConfig

// DetectShellWithOverride 使用用户指定的Shell，失败时回退到默认
//   - shellPath: 用户指定的Shell路径，为空时使用默认
func DetectShellWithOverride(shellPath string) (*ShellConfig, error)
```

---

## 五、数据模型

### 5.1 核心数据结构

```
┌─────────────────────────────────────────────────────────┐
│                      SessionPool                        │
│  ┌──────────────────────────────────────────────────┐   │
│  │              sync.Map[string, *Session]           │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                          │
                          │ 管理
                          ▼
┌─────────────────────────────────────────────────────────┐
│                       Session                           │
│                                                         │
│  ID: string (UUID)                                      │
│  Name: string                                           │
│  Status: SessionStatus (active/exited)                  │
│  CreatedAt: time.Time                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  PtyProcess  │  │ RingBuffer   │  │  WS连接管理   │  │
│  │  (接口)      │  │ (输出缓冲)   │  │ writerWS     │  │
│  │              │  │              │  │ readerWS[]   │  │
│  │  Read/Write  │  │ 容量上限:    │  │              │  │
│  │  Resize      │  │ 1MB          │  │ 写入:仅1个   │  │
│  │  Wait/Close  │  │              │  │ 只读:多个    │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
│  cancelFn: context.CancelFunc                          │
│  mu: sync.Mutex                                        │
└─────────────────────────────────────────────────────────┘
                          │
                          │ 持有
                          ▼
┌─────────────────────────────────────────────────────────┐
│  PtyProcess (接口)                                      │
│  ┌────────────────────┐  ┌─────────────────────────┐   │
│  │  UnixPtyProcess    │  │  WindowsPtyProcess      │   │
│  │  (pty_unix.go)     │  │  (pty_windows.go)       │   │
│  │                    │  │                         │   │
│  │  内部:             │  │  内部:                   │   │
│  │  creack/pty        │  │  UserExistsError/conpty │   │
│  │  *os.File          │  │  *conpty.ConPty         │   │
│  │  *exec.Cmd         │  │                         │   │
│  └────────────────────┘  └─────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### 5.2 RingBuffer输出缓冲区

```go
// RingBuffer 环形缓冲区，用于存储终端输出
// 超过容量时自动覆盖最旧的数据
type RingBuffer struct {
    buf    []byte     // 底层缓冲区
    size   int        // 缓冲区总容量
    head   int        // 读位置
    tail   int        // 写位置
    count  int        // 当前数据量
    mu     sync.Mutex // 并发锁
}

// NewRingBuffer 创建环形缓冲区
//   - size: 缓冲区大小（字节），默认1MB
func NewRingBuffer(size int) *RingBuffer

// Write 写入数据（覆盖最旧数据）
func (rb *RingBuffer) Write(data []byte) (int, error)

// ReadAll 读取所有缓冲数据
func (rb *RingBuffer) ReadAll() []byte

// Reset 重置缓冲区
func (rb *RingBuffer) Reset()
```

### 5.3 WSConn WebSocket连接封装

```go
// WSConn WebSocket连接封装，增加写保护
type WSConn struct {
    conn *websocket.Conn // 底层gorilla/websocket连接
    mu   sync.Mutex      // 写锁（gorilla/websocket非线程安全）
}

// NewWSConn 创建WebSocket连接封装
func NewWSConn(conn *websocket.Conn) *WSConn

// WriteMessage 线程安全地写入WebSocket消息
func (w *WSConn) WriteMessage(messageType int, data []byte) error

// WriteJSON 线程安全地写入JSON消息
func (w *WSConn) WriteJSON(v interface{}) error

// ReadMessage 读取消息
func (w *WSConn) ReadMessage() (int, []byte, error)

// Close 关闭连接
func (w *WSConn) Close() error
```

---

## 六、数据流

### 6.1 终端交互数据流

```
┌──────────┐    WebSocket     ┌──────────┐    Read/Write    ┌──────────┐
│          │  ────input────▶  │          │  ────Write────▶  │          │
│  xterm.js│                  │  Handler │                   │   PTY    │
│  (前端)  │  ◀──output────   │ (后端)   │  ◀──Read─────    │ Process  │
└──────────┘                  └──────────┘                   └──────────┘
     │                             │
     │                             │ 同时写入
     │                             ▼
     │                        ┌──────────┐
     │                        │RingBuffer│
     │                        │(输出缓冲)│
     │                        └──────────┘
     │                             │
     │     重连时回显               │ 导出时读取
     │◀────────────────────────────│──────────────▶ 导出文件
```

### 6.2 WebSocket连接生命周期

```
客户端                        服务端
  │                             │
  │── ws://host/ws?token=X ───▶│ Token验证
  │                             │ ├── 失败 → 401关闭
  │                             │ └── 成功 → 升级为WebSocket
  │◀── session_info ──────────│ 发送当前会话信息
  │                             │
  │     ┌──── 交互循环 ────┐    │
  │     │                   │    │
  │     │── input ─────────▶│────│──▶ Pty.Write()
  │     │                   │    │
  │     │◀ output ──────────│◀───│◀── Pty.Read() → RingBuffer.Write()
  │     │                   │    │
  │     │── resize ────────▶│──▶ │──▶ Pty.Resize()
  │     │                   │    │
  │     │── ping ──────────▶│    │
  │     │◀ pong ───────────│    │
  │     │                   │    │
  │     └───────────────────┘    │
  │                             │
  │──── 连接断开 ──────────────│
  │                             │ DetachWriter()
  │                             │ PTY进程继续运行 ✅
  │                             │
  │── 重连 ws://host/ws?       │
  │   token=X&session_id=Y ──▶│ AttachWriter()
  │                             │ 读取RingBuffer回显
  │◀ output(缓冲区内容) ──────│
  │                             │ 继续正常交互循环
```

### 6.3 会话管理数据流

```
前端(侧边栏)                    后端API                   SessionPool
    │                             │                          │
    │── GET /api/sessions ───────▶│── List() ───────────────▶│
    │◀── [{id,name,status}] ─────│◀── sessions ────────────│
    │                             │                          │
    │── POST /api/sessions ──────▶│── Create() ─────────────▶│
    │   {name:"终端1"}            │   ├── NewPtyProcess()    │
    │                             │   ├── Pty.Start()        │
    │                             │   ├── 启动输出读取goroutine
    │◀── {id,name,created_at} ──│◀── session ──────────────│
    │                             │                          │
    │── PUT /api/sessions/:id ──▶│── Rename() ─────────────▶│
    │   /rename {name:"新名"}    │◀── ok ──────────────────│
    │                             │                          │
    │── DELETE /api/sessions/:id▶│── Close() ──────────────▶│
    │                             │   ├── Pty.Close()        │
    │                             │   └── 资源释放            │
    │◀── {message:"ok"} ────────│◀── ok ──────────────────│
```

---

## 七、跨平台PTY架构（R-001风险解决方案）

### 7.1 架构方案

采用**Go build tags + 接口抽象**方案，编译期区分平台：

```
                    ┌──────────────┐
                    │  PtyProcess  │  (接口定义 pty.go)
                    │   Interface  │
                    └──────┬───────┘
                           │
              ┌────────────┴────────────┐
              │                         │
    ┌─────────▼─────────┐    ┌──────────▼──────────┐
    │  pty_unix.go      │    │  pty_windows.go     │
    │  //go:build !win  │    │  //go:build windows │
    │                   │    │                      │
    │  UnixPtyProcess   │    │  WindowsPtyProcess   │
    │  ┌─────────────┐  │    │  ┌──────────────┐   │
    │  │ creack/pty  │  │    │  │ UserExistsError│  │
    │  │ *os.File    │  │    │  │ /conpty       │  │
    │  │ *exec.Cmd   │  │    │  │ *conpty.ConPty│  │
    │  └─────────────┘  │    │  └──────────────┘   │
    └───────────────────┘    └─────────────────────┘
```

### 7.2 Unix实现 (pty_unix.go)

```go
//go:build !windows

package main

import (
    "os/exec"
    "github.com/creack/pty"
)

type UnixPtyProcess struct {
    ptyFile *os.File   // PTY文件描述符
    cmd     *exec.Cmd  // Shell子进程
}

func NewPtyProcess() PtyProcess {
    return &UnixPtyProcess{}
}

func (p *UnixPtyProcess) Start(cmd string, args []string, rows, cols uint16) error {
    p.cmd = exec.Command(cmd, args...)
    // 设置pty启动尺寸
    ptyOpts := []pty.SysProcAttr{}
    ptmx, err := pty.StartWithSize(p.cmd, &pty.Winsize{
        Rows: rows,
        Cols: cols,
    })
    if err != nil {
        return err
    }
    p.ptyFile = ptmx
    return nil
}
// ... Read, Write, Resize, Wait, Close, Pid, IsRunning 实现
```

### 7.3 Windows实现 (pty_windows.go)

```go
//go:build windows

package main

import (
    "github.com/UserExistsError/conpty"
)

type WindowsPtyProcess struct {
    conpty *conpty.ConPty // Windows ConPTY实例
    pid    int            // 子进程PID
}

func NewPtyProcess() PtyProcess {
    return &WindowsPtyProcess{}
}

func (p *WindowsPtyProcess) Start(cmd string, args []string, rows, cols uint16) error {
    commandLine := cmd
    for _, arg := range args {
        commandLine += " " + arg
    }
    cpty, err := conpty.Start(commandLine,
        conpty.ConPtyDimensions(int(cols), int(rows)),
    )
    if err != nil {
        return err
    }
    p.conpty = cpty
    p.pid = cpty.Pid()
    return nil
}
// ... Read, Write, Resize, Wait, Close, Pid, IsRunning 实现
```

### 7.4 CGO兼容性（R-002风险解决方案）

| 依赖库 | CGO依赖 | CGO_ENABLED=0 | 验证结果 |
|--------|---------|---------------|----------|
| creack/pty | ❌ 无CGO | ✅ 可编译 | 使用Unix syscall，纯Go实现 |
| UserExistsError/conpty | ❌ 无CGO | ✅ 可编译 | 使用golang.org/x/sys/windows，纯Go实现 |
| gorilla/websocket | ❌ 无CGO | ✅ 可编译 | 纯Go实现 |
| gin-gonic/gin | ❌ 无CGO | ✅ 可编译 | 纯Go实现 |

**结论**: 全部依赖均为纯Go实现，CGO_ENABLED=0编译完全可行。

---

## 八、会话持久化架构

### 8.1 断连保活机制

```
WebSocket断开                    Session状态
    │                               │
    │  1. 检测到连接关闭              │
    │  ──────────────────────────▶  │ Session.DetachWriter(ws)
    │                               │ PTY进程继续运行 ✅
    │                               │ PTY输出goroutine继续运行 ✅
    │                               │ RingBuffer继续缓存输出 ✅
    │                               │
    │  （时间流逝...进程持续输出）     │
    │                               │
    │  2. 新WebSocket连接携带         │
    │     session_id重连             │
    │  ──────────────────────────▶  │ Session.AttachWriter(newWs)
    │                               │ ├── 验证会话存在且进程存活
    │  ◀── RingBuffer内容回显 ──────│ ├── 读取缓冲区，发送回显
    │                               │ └── 绑定新WebSocket到写入端
    │                               │
    │  3. 正常交互循环恢复            │
    │  ◀── 实时output ─────────────│
```

### 8.2 进程退出检测

```go
// startOutputReader 启动PTY输出读取goroutine
// 该goroutine在Session创建时启动，WebSocket断开后继续运行
func (s *Session) startOutputReader(ctx context.Context) {
    buf := make([]byte, 4096)
    for {
        select {
        case <-ctx.Done():
            return
        default:
            n, err := s.Pty.Read(buf)
            if err != nil {
                // PTY进程退出
                s.mu.Lock()
                s.Status = SessionExited
                s.mu.Unlock()
                // 通知所有已连接的WebSocket
                s.broadcastError("SESSION_EXITED", "Shell process has exited")
                return
            }
            if n > 0 {
                // 写入缓冲区 + 推送到WebSocket
                s.WriteOutput(buf[:n])
            }
        }
    }
}
```

### 8.3 废弃会话清理策略

| 触发条件 | 清理行为 |
|----------|----------|
| PTY进程自然退出 | 设置status=exited，保留RingBuffer供重连查看退出信息 |
| 用户主动关闭会话 | 释放PTY资源、清除RingBuffer、从Pool中移除 |
| 会话退出且无WebSocket连接 | 5分钟后自动清理（可配置） |
| 进程退出后重连 | 提示SESSION_EXPIRED，引导创建新会话 |

---

## 九、WebSocket通信协议

### 9.1 消息格式

所有消息使用JSON格式，通过WebSocket文本帧传输：

```json
{
    "type": "消息类型",
    "data": "消息数据"
}
```

对于二进制数据（终端输入/输出），`data`字段使用Base64编码。

### 9.2 消息类型定义

#### 客户端→服务端

| 类型 | data格式 | 说明 |
|------|----------|------|
| input | Base64字符串 | 终端输入字符，如 `{"type":"input","data":"bHM="}` (ls) |
| resize | `{"rows":24,"cols":80}` | 终端尺寸变更 |
| ping | 空 | 心跳检测 |

#### 服务端→客户端

| 类型 | data格式 | 说明 |
|------|----------|------|
| output | Base64字符串 | 终端输出数据 |
| error | `{"code":"ERROR_CODE","message":"描述"}` | 错误信息 |
| session_info | `{"id":"uuid","name":"终端1","status":"active","created_at":1713686400}` | 会话元信息，连接建立后首先发送 |
| pong | 空 | 心跳响应 |

### 9.3 错误码定义

| 错误码 | 说明 | 触发场景 |
|--------|------|----------|
| SHELL_NOT_FOUND | Shell未找到 | 指定的Shell路径不存在 |
| SHELL_START_FAILED | Shell启动失败 | PTY创建/启动过程出错 |
| SESSION_NOT_FOUND | 会话不存在 | 操作的session_id无效 |
| SESSION_CREATE_FAILED | 会话创建失败 | 资源不足或PTY创建失败 |
| SESSION_EXPIRED | 会话已过期 | 重连时进程已退出 |
| SESSION_BUSY | 会话忙碌 | 已有写入者连接（同一会话仅允许一个写入者） |
| INVALID_SESSION_NAME | 无效会话名称 | 名称长度超限或格式错误 |
| AUTH_FAILED | 认证失败 | Token无效或缺失 |
| INTERNAL_ERROR | 内部错误 | 服务端未预期错误 |

### 9.4 WebSocket连接参数

```
ws://{host}:{port}/ws?token={ACCESS_TOKEN}&session_id={SESSION_ID}
```

| 参数 | 必需 | 说明 |
|------|------|------|
| token | 是 | 访问令牌 |
| session_id | 否 | 会话ID，提供时为重连模式，不提供时创建新会话 |

---

## 十、前端架构

### 10.1 模块结构

```
static/js/
├── app.js          # 主控制器：全局状态、WebSocket连接管理、模块协调
├── terminal.js     # xterm.js封装：终端初始化、数据绑定、resize处理
├── keyboard.js     # 虚拟键盘：移动端检测、按键渲染、组合键状态机
├── sidebar.js      # 会话侧边栏：列表渲染、CRUD操作、事件绑定
└── export.js       # 终端导出：buffer读取、ANSI清理、文件下载
```

### 10.2 模块职责

```javascript
// app.js - 主控制器
const App = {
    ws: null,              // WebSocket连接实例
    currentSessionId: null, // 当前活跃会话ID
    token: null,           // 访问令牌
    reconnectAttempts: 0,  // 重连尝试次数
    maxReconnectAttempts: 3,

    init(),                // 初始化：Token输入、连接建立
    connect(sessionId),    // 建立WebSocket连接
    disconnect(),          // 断开连接
    reconnect(),           // 自动重连（指数退避）
    handleMessage(msg),    // 消息路由分发
    sendInput(data),       // 发送终端输入
    sendResize(rows, cols),// 发送resize事件
};
```

```javascript
// terminal.js - xterm.js封装
const Terminal = {
    term: null,            // xterm.js实例
    fitAddon: null,        // fit插件实例

    init(container),       // 初始化xterm.js和fit插件
    write(data),           // 写入输出数据
    focus(),               // 聚焦终端
    clear(),               // 清空终端
    getBuffer(),           // 获取终端buffer内容（用于导出）
    dispose(),             // 销毁实例
    onInput(callback),     // 注册输入回调
    onResize(callback),    // 注册resize回调,
};
```

```javascript
// keyboard.js - 虚拟键盘
const Keyboard = {
    visible: false,
    activeModifiers: new Set(), // 当前激活的修饰键

    init(),                // 初始化：检测移动端、渲染键盘
    isMobile(),            // User-Agent检测移动端
    toggle(),              // 切换显示/隐藏
    handleKey(key),        // 处理按键点击
    handleModifier(key),   // 处理修饰键(Ctrl/Alt)切换
    sendCombination(mod, key), // 发送组合键序列
};
```

### 10.3 前端连接状态机

```
                    ┌─────────────┐
                    │ DISCONNECTED│
                    └──────┬──────┘
                           │ connect()
                           ▼
                    ┌─────────────┐
                    │ CONNECTING  │
                    └──────┬──────┘
                      ┌────┴────┐
                   成功│        │失败
                      ▼         ▼
              ┌──────────┐  ┌──────────┐
              │ CONNECTED│  │ RECONNECT│
              └────┬─────┘  │ (退避重试)│
                   │        └─────┬────┘
            ws.onclose│              │超过3次
                   ▼         ┌──────▼──────┐
              ┌──────────┐  │   FAILED    │
              │ RECONNECT│  │ (需手动刷新) │
              │ (自动)   │  └─────────────┘
              └──────────┘
```

---

## 十一、HTTP API路由定义

| 方法 | 路径 | 中间件 | 说明 |
|------|------|--------|------|
| GET | / | - | 静态页面（Go embed） |
| GET | /static/*filepath | - | 静态资源（Go embed） |
| GET | /api/sessions | TokenAuth | 获取会话列表 |
| POST | /api/sessions | TokenAuth | 创建新会话 |
| DELETE | /api/sessions/:id | TokenAuth | 关闭会话 |
| PUT | /api/sessions/:id/rename | TokenAuth | 重命名会话 |
| GET | /ws | TokenAuth(WebSocket) | WebSocket升级 |

### API响应格式

```json
// 成功响应
{
    "code": 0,
    "message": "success",
    "data": { ... }
}

// 错误响应
{
    "code": 40100,
    "message": "Invalid token",
    "data": null
}
```

### 业务状态码定义

| 状态码 | 含义 |
|--------|------|
| 0 | 成功 |
| 40001 | 参数错误 |
| 40100 | 未授权/Token无效 |
| 40401 | 资源不存在（会话等） |
| 40901 | 状态冲突（会话忙碌等） |
| 50001 | 内部错误 |

---

## 十二、构建与部署

### 12.1 Go embed配置

```go
//go:embed static
var staticFS embed.FS
```

所有前端资源编译时嵌入二进制文件，运行时无需外部文件。

### 12.2 交叉编译矩阵

| 平台 | GOOS | GOARCH | 产物名 | Build Tags |
|------|------|--------|--------|------------|
| Windows | windows | amd64 | go-remote-terminal-windows-amd64.exe | (自动) |
| macOS Intel | darwin | amd64 | go-remote-terminal-darwin-amd64 | (自动) |
| macOS Apple | darwin | arm64 | go-remote-terminal-darwin-arm64 | (自动) |
| Linux | linux | amd64 | go-remote-terminal-linux-amd64 | (自动) |

### 12.3 编译参数

```bash
CGO_ENABLED=0 go build -ldflags "-s -w" -o {output} .
```

- `CGO_ENABLED=0`: 禁用CGO，确保静态链接
- `-s -w`: 去除调试信息，减小二进制体积

---

## 十三、扩展点

| 扩展点 | 位置 | 说明 |
|--------|------|------|
| PTY后端 | PtyProcess接口 | 可替换为其他PTY实现（如winpty），只需实现接口 |
| 认证方式 | TokenAuth | 可扩展为OAuth/JWT等认证方式 |
| 输出存储 | RingBuffer | 可替换为持久化存储（文件/数据库），支持更大缓冲 |
| 前端主题 | xterm.js主题配置 | 可扩展主题切换功能 |
| 会话观察 | readerWS列表 | 已预留只读观察者模式，可实现多屏协作 |
| 插件系统 | xterm-addon-* | 可按需加载xterm插件（搜索、webgl渲染等） |
| 配置来源 | config.go | 可扩展从配置文件读取配置 |

---

## 十四、风险记录

| 风险ID | 描述 | 严重度 | 缓解措施 | 状态 |
|--------|------|--------|----------|------|
| R-001 | creack/pty在Windows下不兼容 | 高 | 采用build tags方案，Windows使用UserExistsError/conpty（纯Go ConPTY封装） | ✅ 已解决 |
| R-002 | Windows交叉编译CGO依赖 | 中 | 所有依赖均为纯Go实现，CGO_ENABLED=0完全可行 | ✅ 已解决 |
| R-003 | 大量终端输出的内存管理 | 中 | RingBuffer环形缓冲区，容量上限1MB，超限自动覆盖旧数据 | ✅ 已设计 |
| R-004 | 移动端WebSocket连接不稳定 | 中 | 前端自动重连（指数退避，最多3次），后端断连保活 | ✅ 已设计 |

---

## 变更日志

| 日期 | 版本 | 变更描述 | 变更人 |
|------|------|----------|--------|
| 2026-04-21 | v1.0 | 初始架构文档，包含技术选型、接口定义、数据模型、跨平台方案 | code-framework |
