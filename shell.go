package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ShellConfig Shell配置
type ShellConfig struct {
	Path string   // Shell可执行文件路径
	Args []string // Shell启动参数
}

// AvailableShell 表示一个检测到的可用Shell
type AvailableShell struct {
	Name        string // Shell名称标识，如 "pwsh", "cmd", "bash"
	DisplayName string // 人类可读名称，如 "PowerShell 7"
	Path        string // 可执行文件路径
	Args        []string
}

var (
	// cachedAvailableShells 缓存启动时检测到的可用Shell列表
	cachedAvailableShells []*AvailableShell
)

// GetDefaultShell 返回当前系统检测到的默认Shell配置（用于MCP环境信息）
func GetDefaultShell() *ShellConfig {
	return DetectShell()
}

// DetectAvailableShells 检测并返回当前系统所有可用的Shell
// 结果按推荐优先级排序
func DetectAvailableShells() []*AvailableShell {
	if cachedAvailableShells != nil {
		return cachedAvailableShells
	}

	var shells []*AvailableShell
	switch runtime.GOOS {
	case "windows":
		shells = detectWindowsShells()
	case "darwin":
		shells = detectMacOSShells()
	default: // linux及其他
		shells = detectLinuxShells()
	}

	cachedAvailableShells = shells
	return shells
}

// GetAvailableShellsDescription 返回可用Shell的描述文本，用于MCP工具提示
func GetAvailableShellsDescription() string {
	shells := DetectAvailableShells()
	if len(shells) == 0 {
		return "未检测到可用Shell"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("当前系统检测到 %d 个可用Shell（按推荐优先级排序）：\n", len(shells)))
	for i, s := range shells {
		sb.WriteString(fmt.Sprintf("  %d. %s (%s)\n", i+1, s.DisplayName, s.Path))
	}
	sb.WriteString("\n创建终端时可通过 shell 参数指定要使用的Shell（使用上述名称标识，如 \"pwsh\", \"cmd\", \"bash\"等）。若不指定，则使用默认Shell（列表中的第一个）。")
	return sb.String()
}

// FindAvailableShellByName 根据名称查找可用Shell
func FindAvailableShellByName(name string) *AvailableShell {
	if name == "" {
		return nil
	}
	name = strings.ToLower(strings.TrimSpace(name))
	for _, s := range DetectAvailableShells() {
		if strings.ToLower(s.Name) == name {
			return s
		}
	}
	return nil
}

// DetectShell 根据当前操作系统自动检测并返回默认Shell配置
func DetectShell() *ShellConfig {
	shells := DetectAvailableShells()
	if len(shells) > 0 {
		return &ShellConfig{
			Path: shells[0].Path,
			Args: shells[0].Args,
		}
	}
	// 最终回退
	return &ShellConfig{Path: "sh", Args: []string{}}
}

// DetectShellWithOverride 使用用户指定的Shell，失败时回退到默认
func DetectShellWithOverride(shellPath string) (*ShellConfig, error) {
	if shellPath == "" {
		return DetectShell(), nil
	}

	// 先尝试按名称匹配可用Shell
	if s := FindAvailableShellByName(shellPath); s != nil {
		return &ShellConfig{
			Path: s.Path,
			Args: s.Args,
		}, nil
	}

	// 否则当作路径处理
	if _, err := exec.LookPath(shellPath); err != nil {
		defaultShell := DetectShell()
		return defaultShell, fmt.Errorf("specified shell %q not found, using default %q: %w",
			shellPath, defaultShell.Path, err)
	}

	return &ShellConfig{
		Path: shellPath,
		Args: []string{},
	}, nil
}

// ==================== Windows Shell 检测 ====================

func detectWindowsShells() []*AvailableShell {
	var shells []*AvailableShell

	// 1. PowerShell 7 (pwsh) - 优先推荐
	ps7DefaultPath := filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe")
	if _, err := os.Stat(ps7DefaultPath); err == nil {
		shells = append(shells, &AvailableShell{
			Name:        "pwsh",
			DisplayName: "PowerShell 7",
			Path:        ps7DefaultPath,
			Args:        newPowerShellArgs(),
		})
	}
	if pwshPath, err := exec.LookPath("pwsh.exe"); err == nil {
		// 避免重复添加
		if !shellPathExists(shells, pwshPath) {
			shells = append(shells, &AvailableShell{
				Name:        "pwsh",
				DisplayName: "PowerShell 7",
				Path:        pwshPath,
				Args:        newPowerShellArgs(),
			})
		}
	}

	// 2. Windows PowerShell (系统自带)
	if psPath, err := exec.LookPath("powershell.exe"); err == nil {
		if !shellPathExists(shells, psPath) {
			shells = append(shells, &AvailableShell{
				Name:        "powershell",
				DisplayName: "Windows PowerShell",
				Path:        psPath,
				Args:        newPowerShellArgs(),
			})
		}
	}

	// 3. CMD
	if cmdPath, err := exec.LookPath("cmd.exe"); err == nil {
		shells = append(shells, &AvailableShell{
			Name:        "cmd",
			DisplayName: "CMD",
			Path:        cmdPath,
			Args:        []string{"/k", "chcp", "65001"},
		})
	}

	// 4. WSL Bash
	if wslPath, err := exec.LookPath("wsl.exe"); err == nil {
		// 检测 WSL 内部是否有 bash
		if hasWSLBash(wslPath) {
			shells = append(shells, &AvailableShell{
				Name:        "wsl",
				DisplayName: "WSL Bash",
				Path:        wslPath,
				Args:        []string{"bash", "-l"},
			})
		}
	}

	// 如果什么都没检测到，至少给 cmd 作为最终回退
	if len(shells) == 0 {
		shells = append(shells, &AvailableShell{
			Name:        "cmd",
			DisplayName: "CMD",
			Path:        "cmd.exe",
			Args:        []string{"/k", "chcp", "65001"},
		})
	}

	return shells
}

func shellPathExists(shells []*AvailableShell, path string) bool {
	for _, s := range shells {
		if strings.EqualFold(s.Path, path) {
			return true
		}
	}
	return false
}

func hasWSLBash(wslPath string) bool {
	// 尝试执行 wsl which bash，检测是否有 bash
	cmd := exec.Command(wslPath, "which", "bash")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func newPowerShellArgs() []string {
	return []string{
		"-NoLogo",
		"-ExecutionPolicy", "Bypass",
		"-NoExit",
		"-Command",
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
			"$OutputEncoding = [System.Text.Encoding]::UTF8; " +
			"[Console]::InputEncoding = [System.Text.Encoding]::UTF8",
	}
}

// ==================== Linux Shell 检测 ====================

func detectLinuxShells() []*AvailableShell {
	var shells []*AvailableShell
	candidates := []struct {
		name        string
		displayName string
		paths       []string
		args        []string
	}{
		{"bash", "Bash", []string{"bash", "/bin/bash", "/usr/bin/bash"}, []string{}},
		{"zsh", "Zsh", []string{"zsh", "/bin/zsh", "/usr/bin/zsh"}, []string{}},
		{"sh", "Sh", []string{"sh", "/bin/sh", "/usr/bin/sh"}, []string{}},
	}

	for _, c := range candidates {
		for _, p := range c.paths {
			if path, err := exec.LookPath(p); err == nil {
				if !shellPathExists(shells, path) {
					shells = append(shells, &AvailableShell{
						Name:        c.name,
						DisplayName: c.displayName,
						Path:        path,
						Args:        c.args,
					})
				}
				break
			}
		}
	}

	// 检查 $SHELL 环境变量，如果有且未在列表中，将其放在第一位
	if envShell := os.Getenv("SHELL"); envShell != "" {
		found := false
		for _, s := range shells {
			if strings.EqualFold(s.Path, envShell) {
				found = true
				break
			}
		}
		if !found {
			if path, err := exec.LookPath(envShell); err == nil {
				name := filepath.Base(path)
				shells = append([]*AvailableShell{{
					Name:        name,
					DisplayName: name,
					Path:        path,
					Args:        []string{},
				}}, shells...)
			}
		} else {
			// 将 $SHELL 对应的 shell 移到第一位（用户偏好）
			for i, s := range shells {
				if strings.EqualFold(s.Path, envShell) {
					if i > 0 {
						shells = append([]*AvailableShell{shells[i]}, append(shells[:i], shells[i+1:]...)...)
					}
					break
				}
			}
		}
	}

	if len(shells) == 0 {
		shells = append(shells, &AvailableShell{
			Name:        "sh",
			DisplayName: "Sh",
			Path:        "/bin/sh",
			Args:        []string{},
		})
	}

	return shells
}

// ==================== macOS Shell 检测 ====================

func detectMacOSShells() []*AvailableShell {
	var shells []*AvailableShell
	candidates := []struct {
		name        string
		displayName string
		paths       []string
		args        []string
	}{
		{"zsh", "Zsh", []string{"zsh", "/bin/zsh", "/usr/bin/zsh"}, []string{}},
		{"bash", "Bash", []string{"bash", "/bin/bash", "/usr/bin/bash"}, []string{}},
		{"sh", "Sh", []string{"sh", "/bin/sh", "/usr/bin/sh"}, []string{}},
	}

	for _, c := range candidates {
		for _, p := range c.paths {
			if path, err := exec.LookPath(p); err == nil {
				if !shellPathExists(shells, path) {
					shells = append(shells, &AvailableShell{
						Name:        c.name,
						DisplayName: c.displayName,
						Path:        path,
						Args:        c.args,
					})
				}
				break
			}
		}
	}

	// 检查 $SHELL 环境变量，同 Linux 逻辑
	if envShell := os.Getenv("SHELL"); envShell != "" {
		found := false
		for _, s := range shells {
			if strings.EqualFold(s.Path, envShell) {
				found = true
				break
			}
		}
		if !found {
			if path, err := exec.LookPath(envShell); err == nil {
				name := filepath.Base(path)
				shells = append([]*AvailableShell{{
					Name:        name,
					DisplayName: name,
					Path:        path,
					Args:        []string{},
				}}, shells...)
			}
		} else {
			for i, s := range shells {
				if strings.EqualFold(s.Path, envShell) {
					if i > 0 {
						shells = append([]*AvailableShell{shells[i]}, append(shells[:i], shells[i+1:]...)...)
					}
					break
				}
			}
		}
	}

	if len(shells) == 0 {
		shells = append(shells, &AvailableShell{
			Name:        "zsh",
			DisplayName: "Zsh",
			Path:        "/bin/zsh",
			Args:        []string{},
		})
	}

	return shells
}

// SortShellsByPreference 根据名称排序Shell，但不改变原始优先级顺序
// 仅用于展示时按名称分组
func SortShellsByPreference(shells []*AvailableShell) []*AvailableShell {
	result := make([]*AvailableShell, len(shells))
	copy(result, shells)
	sort.SliceStable(result, func(i, j int) bool {
		order := map[string]int{
			"pwsh": 1, "powershell": 2, "cmd": 3, "wsl": 4,
			"zsh": 5, "bash": 6, "sh": 7,
		}
		oi, oki := order[result[i].Name]
		oj, okj := order[result[j].Name]
		if !oki {
			oi = 99
		}
		if !okj {
			oj = 99
		}
		return oi < oj
	})
	return result
}
