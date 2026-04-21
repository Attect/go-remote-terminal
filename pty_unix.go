//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// UnixPtyProcess 基于creack/pty的Unix PTY实现
type UnixPtyProcess struct {
	ptmx    *os.File   // PTY主端文件描述符
	cmd     *exec.Cmd  // Shell子进程
	running bool       // 进程运行状态
	mu      sync.Mutex // 保护running字段
}

// NewPtyProcess 创建新的PTY进程实例（Unix实现）
func NewPtyProcess() PtyProcess {
	return &UnixPtyProcess{}
}

// Start 启动PTY进程
func (p *UnixPtyProcess) Start(cmd string, args []string, rows, cols uint16) error {
	p.cmd = exec.Command(cmd, args...)
	p.cmd.Env = os.Environ()

	// 确保UTF-8 locale已设置，解决中文显示问题
	if !envHasUTF8Locale(p.cmd.Env) {
		p.cmd.Env = append(p.cmd.Env, "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")
	}

	ptmx, err := pty.StartWithSize(p.cmd, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
	if err != nil {
		return err
	}

	p.ptmx = ptmx
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()
	return nil
}

// envHasUTF8Locale 检查环境变量中是否已设置UTF-8 locale
func envHasUTF8Locale(env []string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, "LANG=") || strings.HasPrefix(e, "LC_ALL=") {
			if strings.Contains(strings.ToUpper(e), "UTF") {
				return true
			}
		}
	}
	return false
}

// Read 从PTY读取输出数据
func (p *UnixPtyProcess) Read(b []byte) (int, error) {
	return p.ptmx.Read(b)
}

// Write 向PTY写入输入数据
func (p *UnixPtyProcess) Write(b []byte) (int, error) {
	return p.ptmx.Write(b)
}

// Resize 调整PTY终端尺寸
func (p *UnixPtyProcess) Resize(rows, cols uint16) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Wait 等待PTY进程退出
func (p *UnixPtyProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	if err != nil {
		// 尝试从ProcessState获取退出码
		if p.cmd.ProcessState != nil {
			return p.cmd.ProcessState.ExitCode(), err
		}
		return -1, err
	}
	return 0, nil
}

// Close 关闭PTY，释放资源
func (p *UnixPtyProcess) Close() error {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()

	var firstErr error
	if p.ptmx != nil {
		if err := p.ptmx.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if p.cmd != nil && p.cmd.Process != nil {
		// 发送SIGTERM尝试优雅关闭，然后SIGKILL强制关闭
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	return firstErr
}

// Pid 返回PTY关联的子进程PID
func (p *UnixPtyProcess) Pid() int {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// IsRunning 检查PTY进程是否仍在运行
func (p *UnixPtyProcess) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return false
	}
	// 通过signal 0检测进程是否存活
	if p.cmd != nil && p.cmd.Process != nil {
		err := p.cmd.Process.Signal(syscall.Signal(0))
		if err != nil {
			p.running = false
			return false
		}
	}
	return true
}
