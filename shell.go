package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// ShellConfig Shell配置
type ShellConfig struct {
	Path string   // Shell可执行文件路径
	Args []string // Shell启动参数
}

// DetectShell 根据当前操作系统自动检测并返回Shell配置
func DetectShell() *ShellConfig {
	switch runtime.GOOS {
	case "windows":
		return detectWindowsShell()
	case "darwin":
		return detectMacOSShell()
	default: // linux及其他
		return detectLinuxShell()
	}
}

// DetectShellWithOverride 使用用户指定的Shell，失败时回退到默认
func DetectShellWithOverride(shellPath string) (*ShellConfig, error) {
	if shellPath == "" {
		return DetectShell(), nil
	}

	// 检查指定的Shell是否存在且可执行
	if _, err := exec.LookPath(shellPath); err != nil {
		// 指定的Shell不可用，回退到默认
		defaultShell := DetectShell()
		return defaultShell, fmt.Errorf("specified shell %q not found, using default %q: %w",
			shellPath, defaultShell.Path, err)
	}

	return &ShellConfig{
		Path: shellPath,
		Args: []string{},
	}, nil
}

// detectWindowsShell 检测Windows Shell
// 优先使用PowerShell，失败回退到cmd.exe
func detectWindowsShell() *ShellConfig {
	// 尝试PowerShell
	psPath, err := exec.LookPath("powershell.exe")
	if err == nil {
		return &ShellConfig{
			Path: psPath,
			Args: []string{"-NoLogo", "-ExecutionPolicy", "Bypass"},
		}
	}

	// 尝试pwsh (PowerShell Core)
	pwshPath, err := exec.LookPath("pwsh.exe")
	if err == nil {
		return &ShellConfig{
			Path: pwshPath,
			Args: []string{"-NoLogo"},
		}
	}

	// 回退到cmd.exe
	cmdPath, err := exec.LookPath("cmd.exe")
	if err == nil {
		return &ShellConfig{
			Path: cmdPath,
			Args: []string{},
		}
	}

	// 最终回退
	return &ShellConfig{
		Path: "cmd.exe",
		Args: []string{},
	}
}

// detectLinuxShell 检测Linux Shell
// 使用$SHELL环境变量，回退到/bin/bash
func detectLinuxShell() *ShellConfig {
	if shell := os.Getenv("SHELL"); shell != "" {
		if _, err := exec.LookPath(shell); err == nil {
			return &ShellConfig{
				Path: shell,
				Args: []string{},
			}
		}
	}

	// 回退到/bin/bash
	if _, err := exec.LookPath("/bin/bash"); err == nil {
		return &ShellConfig{
			Path: "/bin/bash",
			Args: []string{},
		}
	}

	// 最终回退到/bin/sh
	return &ShellConfig{
		Path: "/bin/sh",
		Args: []string{},
	}
}

// detectMacOSShell 检测macOS Shell
// 优先zsh，回退bash
func detectMacOSShell() *ShellConfig {
	// 优先使用$SHELL环境变量
	if shell := os.Getenv("SHELL"); shell != "" {
		if _, err := exec.LookPath(shell); err == nil {
			return &ShellConfig{
				Path: shell,
				Args: []string{},
			}
		}
	}

	// 尝试/bin/zsh
	if _, err := exec.LookPath("/bin/zsh"); err == nil {
		return &ShellConfig{
			Path: "/bin/zsh",
			Args: []string{},
		}
	}

	// 尝试/bin/bash
	if _, err := exec.LookPath("/bin/bash"); err == nil {
		return &ShellConfig{
			Path: "/bin/bash",
			Args: []string{},
		}
	}

	// 最终回退到/bin/sh
	return &ShellConfig{
		Path: "/bin/sh",
		Args: []string{},
	}
}
