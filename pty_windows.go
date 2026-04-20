//go:build windows

package main

import (
	"context"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
)

// quoteArg 对Windows命令行参数进行引号包裹
// 含空格或特殊字符的参数用双引号包裹，已包裹的不重复处理
func quoteArg(arg string) string {
	if arg == "" {
		return `""`
	}
	// 已被双引号包裹则直接返回
	if strings.HasPrefix(arg, `"`) && strings.HasSuffix(arg, `"`) {
		return arg
	}
	// 含空格或特殊字符时需要引号
	if strings.ContainsAny(arg, " \t\"") {
		return `"` + arg + `"`
	}
	return arg
}

// WindowsPtyProcess 基于conpty的Windows PTY实现
type WindowsPtyProcess struct {
	cpty    *conpty.ConPty // Windows ConPTY实例
	pid     int            // 子进程PID
	running bool           // 进程运行状态
	mu      sync.Mutex     // 保护running字段
}

// NewPtyProcess 创建新的PTY进程实例（Windows实现）
func NewPtyProcess() PtyProcess {
	return &WindowsPtyProcess{}
}

// Start 启动PTY进程
func (p *WindowsPtyProcess) Start(cmd string, args []string, rows, cols uint16) error {
	// conpty.Start需要完整的命令行字符串
	// 对命令路径和参数进行引号转义，处理含空格的路径
	commandLine := quoteArg(cmd)
	for _, arg := range args {
		commandLine += " " + quoteArg(arg)
	}

	cpty, err := conpty.Start(commandLine,
		conpty.ConPtyDimensions(int(cols), int(rows)),
	)
	if err != nil {
		return err
	}

	p.cpty = cpty
	p.pid = cpty.Pid()
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()
	return nil
}

// Read 从PTY读取输出数据
func (p *WindowsPtyProcess) Read(b []byte) (int, error) {
	return p.cpty.Read(b)
}

// Write 向PTY写入输入数据
func (p *WindowsPtyProcess) Write(b []byte) (int, error) {
	return p.cpty.Write(b)
}

// Resize 调整PTY终端尺寸
// conpty的Resize参数为(width, height)，即(cols, rows)
func (p *WindowsPtyProcess) Resize(rows, cols uint16) error {
	return p.cpty.Resize(int(cols), int(rows))
}

// Wait 等待PTY进程退出
func (p *WindowsPtyProcess) Wait() (int, error) {
	exitCode, err := p.cpty.Wait(context.Background())
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	if err != nil {
		return -1, err
	}
	return int(exitCode), nil
}

// Close 关闭PTY，释放资源
func (p *WindowsPtyProcess) Close() error {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	if p.cpty != nil {
		return p.cpty.Close()
	}
	return nil
}

// Pid 返回PTY关联的子进程PID
func (p *WindowsPtyProcess) Pid() int {
	return p.pid
}

// IsRunning 检查PTY进程是否仍在运行
func (p *WindowsPtyProcess) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}
