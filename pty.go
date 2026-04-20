package main

// PtyProcess 定义跨平台PTY进程的统一接口
// 通过Go build tags在编译期选择具体实现
type PtyProcess interface {
	// Start 启动PTY进程
	//   - cmd: Shell命令路径
	//   - args: 命令参数
	//   - rows, cols: 初始终端尺寸
	Start(cmd string, args []string, rows, cols uint16) error

	// Read 从PTY读取输出数据
	Read(p []byte) (n int, err error)

	// Write 向PTY写入输入数据
	Write(p []byte) (n int, err error)

	// Resize 调整PTY终端尺寸
	Resize(rows, cols uint16) error

	// Wait 等待PTY进程退出，返回退出码
	Wait() (exitCode int, err error)

	// Close 关闭PTY，释放资源
	Close() error

	// Pid 返回PTY关联的子进程PID
	Pid() int

	// IsRunning 检查PTY进程是否仍在运行
	IsRunning() bool
}
