# 跨平台Web终端远程控制系统 — 需求规格说明书

> 版本: v1.0  
> 创建日期: 2026-04-21  
> 创建人: requirement-analyst  
> 来源文档: 设计.txt、ai/user_say.md

---

## 一、项目概述

开发一个轻量级、跨平台的Web终端远程控制系统。用户在Windows 10/11、Linux或macOS上运行后台服务程序，即可通过手机或电脑浏览器远程访问该终端。系统采用单文件二进制分发模式，无需安装客户端App。

### 项目约束

| 约束项 | 约束内容 |
|--------|----------|
| 开发语言 | Go 1.20+ |
| Web框架 | github.com/gin-gonic/gin |
| WebSocket | github.com/gorilla/websocket |
| 伪终端 | github.com/creack/pty |
| 前端终端 | xterm.js + xterm-addon-fit |
| 分发模式 | 单文件二进制，静态链接，无外部DLL依赖 |
| 目标平台 | Windows 10/11 (amd64)、macOS (amd64/arm64)、Linux (amd64) |

---

## 二、功能需求清单

### F01 跨平台Shell适配

- 功能目标: 根据运行操作系统自动选择并启动对应的Shell程序
- 输入定义: 
  - 系统自动检测 `runtime.GOOS`，无需用户手动配置
- 输出定义: 
  - 启动对应Shell子进程，返回PTY文件描述符
- 边界条件:
  - BC-01: Linux下 `$SHELL` 环境变量未设置时，默认使用 `/bin/bash`
  - BC-02: macOS下既无 `/bin/zsh` 也无 `/bin/bash` 时，返回错误提示
  - BC-03: Windows下 PowerShell 不可用时，回退至 `cmd.exe`
- 异常处理:
  - Shell启动失败时返回明确错误信息，不创建空会话
  - 错误码: `SHELL_NOT_FOUND`、`SHELL_START_FAILED`
- 验收标准:
  - AC-01: Windows环境启动 `powershell.exe -NoLogo -ExecutionPolicy Bypass`
  - AC-02: Linux环境启动 `$SHELL` 或 `/bin/bash`
  - AC-03: macOS环境启动 `/bin/zsh` 或 `/bin/bash`
- 优先级: P0
- 关联任务: TASK-001

### F02 PTY会话管理

- 功能目标: 管理多个终端会话的生命周期，包括创建、切换、关闭、重命名
- 输入定义:
  - 创建会话: 无必需参数（使用默认Shell）；可选参数: `name`(会话名称)、`shell`(指定Shell路径)
  - 切换会话: `session_id`(目标会话ID)
  - 关闭会话: `session_id`(目标会话ID)
  - 重命名会话: `session_id` + `new_name`
- 输出定义:
  - 创建会话: 返回 `session_id` 和会话元信息
  - 切换会话: 返回目标会话的当前终端输出
  - 关闭会话: 返回操作确认
  - 重命名会话: 返回操作确认
- 边界条件:
  - BC-01: 系统资源不足时拒绝创建新会话，返回明确提示
  - BC-02: 关闭不存在的session_id时返回 `SESSION_NOT_FOUND` 错误
  - BC-03: 会话名称长度限制: 1-50字符，超出截断或拒绝
  - BC-04: 会话名称允许重复（不同会话可有相同名称）
- 异常处理:
  - 错误码: `SESSION_NOT_FOUND`、`SESSION_CREATE_FAILED`、`INVALID_SESSION_NAME`
- 验收标准:
  - AC-01: 可同时创建多个PTY会话
  - AC-02: 使用 `sync.Map` 存储活跃会话，维护 `SessionID -> Process` 映射
  - AC-03: 切换会话时前端正确显示目标会话终端内容
  - AC-04: 关闭会话时子进程被正确终止，资源被释放
  - AC-05: 重命名会话后侧边栏立即更新显示
- 优先级: P0
- 关联任务: TASK-001

### F03 会话持久化与重连

- 功能目标: WebSocket断开连接后，后台Shell进程及运行中的任务不中断；重连后可恢复到原会话
- 输入定义:
  - WebSocket连接时携带 `session_id`(可选，用于重连已有会话)
- 输出定义:
  - 新连接: 创建新PTY会话并绑定
  - 重连: 将WebSocket绑定到已有进程的输入输出流
- 边界条件:
  - BC-01: WebSocket断开时，**严禁**杀死子进程
  - BC-02: 重连时 `session_id` 对应的进程已自然退出，应通知前端并提示创建新会话
  - BC-03: 同一会话同一时刻只允许一个WebSocket写入（防止输入冲突）
  - BC-04: 同一会话允许多个WebSocket只读连接（观察模式，可选实现）
- 异常处理:
  - 错误码: `SESSION_EXPIRED`(进程已退出)、`SESSION_BUSY`(已有写入者连接)
- 验收标准:
  - AC-01: 手机连接运行 `ping 8.8.8.8 -t`，关闭浏览器10秒后电脑连接同一Session ID，ping命令仍在继续输出
  - AC-02: 断连后子进程状态为运行中（非僵尸进程）
  - AC-03: 重连后终端输出从当前状态继续，无需重新启动Shell
- 优先级: P0
- 关联任务: TASK-001

### F04 WebSocket通信

- 功能目标: 建立前端与后端之间的双向实时通信通道，传输终端输入输出数据
- 输入定义:
  - 连接URL: `ws://{host}:{port}/ws?token={ACCESS_TOKEN}&session_id={SESSION_ID}(可选)`
  - 前端→后端消息类型:
    - `input`: 终端输入字符
    - `resize`: 终端尺寸变更 `{rows, cols}`
  - 后端→前端消息类型:
    - `output`: 终端输出数据
    - `error`: 错误信息
    - `session_info`: 会话元信息
- 输出定义:
  - 实时双向数据流
- 边界条件:
  - BC-01: WebSocket连接中断后自动重连（前端实现，含指数退避策略）
  - BC-02: 大量输出数据时的背压处理（避免内存溢出）
  - BC-03: WebSocket消息大小限制（建议1MB）
- 异常处理:
  - 连接异常断开时，前端尝试自动重连
  - 重连失败超过3次后提示用户手动刷新
- 验收标准:
  - AC-01: 终端输入实时传达到PTY进程
  - AC-02: PTY输出实时显示在前端终端
  - AC-03: 窗口大小变更时，resize事件正确发送并生效
- 优先级: P0
- 关联任务: TASK-001

### F05 Token安全认证

- 功能目标: 通过Token机制防止未授权访问Web终端
- 输入定义:
  - 配置方式: 配置文件 或 启动参数 `--token` / `-t`
  - 验证时机: WebSocket握手阶段
- 输出定义:
  - 验证通过: 建立WebSocket连接
  - 验证失败: 拒绝连接，返回HTTP 401/403
- 边界条件:
  - BC-01: Token为空或未配置时，系统拒绝所有WebSocket连接（安全优先）
  - BC-02: Token长度建议8字符以上
  - BC-03: Token区分大小写
  - BC-04: 同一Token支持多个并发连接
- 异常处理:
  - 错误Token: 连接被拒绝，不泄露服务端信息
  - 无Token: 连接被拒绝
- 验收标准:
  - AC-01: 错误Token无法建立WebSocket连接
  - AC-02: 正确Token可正常建立连接
  - AC-03: 未配置Token时系统启动报错或拒绝连接
- 优先级: P0
- 关联任务: TASK-001

### F06 终端渲染(xterm.js)

- 功能目标: 在浏览器中渲染终端界面，正确处理ANSI转义码，支持TUI程序
- 输入定义:
  - PTY输出的原始字节流（含ANSI转义码）
  - 用户键盘输入事件
- 输出定义:
  - 渲染后的终端界面
  - 键盘输入字符流（发送至后端）
- 边界条件:
  - BC-01: 窗口大小变化时自动调整终端尺寸（xterm-addon-fit）
  - BC-02: 高DPI屏幕下的渲染适配
  - BC-03: 大量输出时的渲染性能（避免卡顿）
- 异常处理:
  - xterm.js初始化失败时显示友好错误提示
- 验收标准:
  - AC-01: 在网页终端中运行 `vim` 或 `htop`(Linux/Mac) 或 `winget`(Win)，界面正常显示
  - AC-02: 光标移动无乱码
  - AC-03: ANSI颜色正确渲染
  - AC-04: TUI程序界面元素对齐正确
- 优先级: P0
- 关联任务: TASK-001

### F07 移动端虚拟键盘

- 功能目标: 在移动设备上提供虚拟键盘栏，支持TUI操作所需的特殊按键和组合键
- 输入定义:
  - 用户对虚拟按键的点击/长按事件
- 输出定义:
  - 对应按键序列发送至PTY进程
- 功能细节:
  - 检测方式: User-Agent判断移动端设备
  - 必需按键: `Esc`, `Tab`, `Ctrl`, `Alt`, `↑`, `↓`, `←`, `→`
  - 组合键支持: 按住修饰键(Ctrl/Alt)后再按其他键，发送组合键序列
  - 按住状态: 修饰键支持锁定/按住状态，视觉上需区分激活/未激活
  - 显隐控制: 虚拟键盘支持显示/隐藏切换
- 边界条件:
  - BC-01: 虚拟键盘不应遮挡终端关键内容（可滚动或调整终端区域）
  - BC-02: 横屏/竖屏切换时键盘布局自适应
  - BC-03: 虚拟键盘与系统软键盘可共存
- 异常处理:
  - 移动端检测失败时，默认不显示虚拟键盘（可手动触发）
- 验收标准:
  - AC-01: 移动端访问时底部显示虚拟键盘栏
  - AC-02: 虚拟键盘可正常显示/隐藏
  - AC-03: 按住Ctrl后再按C键，发送Ctrl+C组合键
  - AC-04: 方向键在vim中可正常移动光标
- 优先级: P1
- 关联任务: TASK-001

### F08 会话侧边栏

- 功能目标: 左侧边栏显示所有活跃终端会话列表，提供会话管理操作入口
- 输入定义:
  - 会话列表数据（从后端API获取）
  - 用户操作事件（新建/切换/关闭/重命名）
- 输出定义:
  - 会话列表UI，包含会话名称、状态指示、操作按钮
- 功能细节:
  - 显示当前所有活跃终端会话
  - 当前活跃会话高亮标识
  - 新建会话按钮
  - 切换会话：点击目标会话
  - 关闭会话：关闭按钮，需二次确认
  - 重命名会话：双击或右键菜单触发编辑
- 边界条件:
  - BC-01: 侧边栏可折叠/展开，避免在移动端占用过多空间
  - BC-02: 会话数量较多时支持滚动
  - BC-03: 关闭会话的二次确认防止误操作
- 异常处理:
  - 会话列表获取失败时显示错误提示并提供重试
- 验收标准:
  - AC-01: 侧边栏正确显示所有活跃会话
  - AC-02: 新建会话后列表立即更新
  - AC-03: 切换会话后终端内容正确切换
  - AC-04: 关闭会话后列表移除该项
  - AC-05: 重命名后侧边栏显示新名称
- 优先级: P1
- 关联任务: TASK-001

### F09 终端输出导出

- 功能目标: 将当前终端的输出内容导出为文本文件
- 输入定义:
  - 用户点击导出按钮
  - 可选: 选择导出范围（全部/选区）
- 输出定义:
  - 下载文本文件（.txt），文件名建议格式: `terminal_output_{session_name}_{timestamp}.txt`
- 边界条件:
  - BC-01: 终端无输出时导出空文件或提示无内容
  - BC-02: 大量输出时的导出性能（不应阻塞UI）
  - BC-03: 导出内容需清理ANSI转义码，仅保留纯文本
- 异常处理:
  - 导出失败时提示用户重试
- 验收标准:
  - AC-01: 点击导出按钮可正确下载终端输出文本文件
  - AC-02: 导出文件内容与终端显示一致（去除ANSI控制码）
  - AC-03: 文件编码为UTF-8
- 优先级: P2
- 关联任务: TASK-001

### F10 HTTP API接口

- 功能目标: 提供RESTful API供前端进行会话管理操作
- 输入定义:
  - 所有API请求Header需携带 `Authorization: Bearer {token}` 或在查询参数中携带token
- 输出定义:
  - JSON格式响应
- API清单:

| 方法 | 路径 | 说明 | 请求体 | 响应体 |
|------|------|------|--------|--------|
| GET | /api/sessions | 获取会话列表 | - | `{sessions: [{id, name, created_at, status}]}` |
| POST | /api/sessions | 创建新会话 | `{name?, shell?}` | `{id, name, created_at}` |
| DELETE | /api/sessions/:id | 关闭会话 | - | `{message}` |
| PUT | /api/sessions/:id/rename | 重命名会话 | `{name}` | `{id, name}` |
| GET | /ws | WebSocket升级 | query: `token`, `session_id?` | WebSocket连接 |

- 边界条件:
  - BC-01: 所有API需验证Token，未授权返回401
  - BC-02: API响应格式统一: `{code, message, data}`
- 异常处理:
  - 标准HTTP状态码: 200(成功), 400(参数错误), 401(未授权), 404(不存在), 500(内部错误)
- 验收标准:
  - AC-01: 所有API按上述规范正常工作
  - AC-02: 未授权请求被正确拒绝
- 优先级: P0
- 关联任务: TASK-001

### F11 交叉编译与构建

- 功能目标: 提供构建脚本，在Linux/Windows环境下编译所有目标平台的二进制文件
- 输入定义:
  - 构建脚本（build.sh / Makefile）
- 输出定义:
  - 目标产物:

| 平台 | GOOS | GOARCH | 产物名 |
|------|------|--------|--------|
| Windows | windows | amd64 | go-remote-terminal-windows-amd64.exe |
| macOS Intel | darwin | amd64 | go-remote-terminal-darwin-amd64 |
| macOS Apple | darwin | arm64 | go-remote-terminal-darwin-arm64 |
| Linux | linux | amd64 | go-remote-terminal-linux-amd64 |

- 边界条件:
  - BC-01: CGO_ENABLED=0 确保静态链接
  - BC-02: 使用 `-ldflags "-s -w"` 减小二进制体积
  - BC-03: 前端静态资源通过 `embed` 嵌入二进制文件
- 验收标准:
  - AC-01: 在Linux环境执行构建脚本可生成所有4个平台的二进制文件
  - AC-02: 生成的二进制文件可直接运行，无外部依赖
  - AC-03: Windows产物为.exe后缀
- 优先级: P1
- 关联任务: TASK-001

---

## 三、非功能需求

### NF01 性能

- 单服务器支持至少10个并发PTY会话
- 终端输出延迟 < 100ms（局域网环境）
- 内存占用: 空闲状态 < 50MB，每会话增量 < 10MB

### NF02 安全

- Token不以明文记录在日志中
- WebSocket连接必须经过Token验证
- 服务默认监听 `0.0.0.0`，但支持配置绑定地址
- 不暴露系统内部错误详情给客户端

### NF03 可用性

- 浏览器兼容: Chrome 80+、Safari 14+、Firefox 80+
- 移动端兼容: iOS Safari 14+、Android Chrome 80+
- WebSocket断连后前端自动重连（指数退避，最多3次）

### NF04 可维护性

- 代码结构按附录目录规范组织
- 关键逻辑需有注释
- 前端静态资源通过Go embed嵌入，无需独立部署

---

## 四、技术风险与约束

| 风险ID | 风险描述 | 严重度 | 缓解措施 |
|--------|----------|--------|----------|
| R-001 | creack/pty 在Windows下的兼容性可能存在限制（该库主要为Unix设计） | 高 | 需验证Windows PTY实现方案，可能需要使用 `conpty` 或 `winpty` 替代方案 |
| R-002 | Windows下交叉编译CGO依赖问题 | 中 | 使用 `CGO_ENABLED=0` 纯Go编译，但需确认pty库是否依赖CGO |
| R-003 | 大量终端输出时的内存管理 | 中 | 实现输出缓冲区上限，超限丢弃旧数据 |
| R-004 | 移动端浏览器WebSocket连接不稳定 | 中 | 前端实现自动重连机制，后端支持断连保活 |

---

## 五、数据模型

### Session

```
Session {
    ID        string    // 会话唯一标识，UUID格式
    Name      string    // 会话显示名称
    Cmd       *exec.Cmd // 关联的Shell子进程
    Pty       io.ReadWriter // PTY文件描述符
    CreatedAt time.Time // 创建时间
    Status    string    // 会话状态: active / exited
    Output    []byte    // 终端输出缓冲(用于导出和重连回显)
}
```

### SessionPool

```
SessionPool {
    sessions  sync.Map  // map[sessionID]*Session
    mutex     sync.Mutex
}
```

---

## 六、项目目录结构

```
/project-root
  ├── main.go            # 入口文件，包含路由和配置
  ├── session.go         # 会话管理逻辑 (Pool, Attach, Detach)
  ├── pty.go             # 跨平台PTY实现 (利用build tags区分win/unix)
  ├── pty_windows.go     # Windows专用PTY实现
  ├── pty_unix.go        # Unix/Linux/macOS专用PTY实现
  ├── handler.go         # WebSocket和HTTP处理器
  ├── auth.go            # Token认证逻辑
  ├── static/            # 前端静态资源
  │   ├── index.html     # 前端页面
  │   ├── css/           # 样式文件
  │   └── js/            # JavaScript文件
  ├── build.sh           # 交叉编译脚本(Linux/macOS)
  ├── build.bat          # 交叉编译脚本(Windows)
  ├── go.mod
  └── go.sum
```

---

## 七、验收测试场景

### 场景1: TUI测试
1. 启动服务，浏览器连接
2. 在终端中运行 `vim`(Linux/Mac) 或 `winget`(Win)
3. 预期: 界面正常显示，光标移动无乱码

### 场景2: 断连保活测试
1. 手机浏览器连接，运行 `ping 8.8.8.8 -t`
2. 关闭手机浏览器
3. 等待10秒
4. 电脑浏览器连接同一Session ID
5. 预期: ping命令仍在继续输出，未中断

### 场景3: 安全认证测试
1. 使用错误Token尝试连接
2. 预期: WebSocket连接被拒绝
3. 使用正确Token连接
4. 预期: 连接成功

### 场景4: 移动端虚拟键盘测试
1. 移动端浏览器访问
2. 验证虚拟键盘自动显示
3. 验证隐藏/显示切换
4. 按住Ctrl后按C键
5. 预期: 发送Ctrl+C组合键

### 场景5: 会话管理测试
1. 新建3个会话
2. 在会话间切换，验证内容正确
3. 重命名一个会话
4. 关闭一个会话，验证列表更新

### 场景6: 导出功能测试
1. 在终端中执行若干命令产生输出
2. 点击导出按钮
3. 预期: 下载的文本文件内容与终端输出一致

---

## 变更日志

| 日期 | 变更类型 | 变更描述 | 变更人 |
|------|----------|----------|--------|
| 2026-04-21 | 新建 | 基于设计.txt创建初始需求规格说明书 | requirement-analyst |
