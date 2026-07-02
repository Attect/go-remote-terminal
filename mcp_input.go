package main

import (
	"fmt"
	"strings"
)

// keyMap 键名到ANSI转义序列的映射
var keyMap = map[string][]byte{
	// 基本键
	"Enter":     []byte("\r"),
	"Return":    []byte("\r"),
	"Escape":    []byte("\x1b"),
	"Esc":       []byte("\x1b"),
	"Tab":       []byte("\t"),
	"Backspace": []byte("\x7f"),
	"Space":     []byte(" "),
	"Delete":    []byte("\x1b[3~"),
	"Insert":    []byte("\x1b[2~"),
	"Home":      []byte("\x1b[H"),
	"End":       []byte("\x1b[F"),
	"PageUp":    []byte("\x1b[5~"),
	"PageDown":  []byte("\x1b[6~"),

	// 方向键
	"ArrowUp":    []byte("\x1b[A"),
	"ArrowDown":  []byte("\x1b[B"),
	"ArrowRight": []byte("\x1b[C"),
	"ArrowLeft":  []byte("\x1b[D"),
	"Up":         []byte("\x1b[A"),
	"Down":       []byte("\x1b[B"),
	"Right":      []byte("\x1b[C"),
	"Left":       []byte("\x1b[D"),

	// F1-F12
	"F1":  []byte("\x1bOP"),
	"F2":  []byte("\x1bOQ"),
	"F3":  []byte("\x1bOR"),
	"F4":  []byte("\x1bOS"),
	"F5":  []byte("\x1b[15~"),
	"F6":  []byte("\x1b[17~"),
	"F7":  []byte("\x1b[18~"),
	"F8":  []byte("\x1b[19~"),
	"F9":  []byte("\x1b[20~"),
	"F10": []byte("\x1b[21~"),
	"F11": []byte("\x1b[23~"),
	"F12": []byte("\x1b[24~"),
}

// ctrlMap Ctrl组合键映射
var ctrlMap = map[byte][]byte{
	'A': []byte{0x01}, 'B': []byte{0x02}, 'C': []byte{0x03}, 'D': []byte{0x04},
	'E': []byte{0x05}, 'F': []byte{0x06}, 'G': []byte{0x07}, 'H': []byte{0x08},
	'I': []byte{0x09}, 'J': []byte{0x0a}, 'K': []byte{0x0b}, 'L': []byte{0x0c},
	'M': []byte{0x0d}, 'N': []byte{0x0e}, 'O': []byte{0x0f}, 'P': []byte{0x10},
	'Q': []byte{0x11}, 'R': []byte{0x12}, 'S': []byte{0x13}, 'T': []byte{0x14},
	'U': []byte{0x15}, 'V': []byte{0x16}, 'W': []byte{0x17}, 'X': []byte{0x18},
	'Y': []byte{0x19}, 'Z': []byte{0x1a},
	'[': []byte{0x1b}, '\\': []byte{0x1c}, ']': []byte{0x1d}, '^': []byte{0x1e}, '_': []byte{0x1f},
	'@': []byte{0x00},
}

// ParseKeyInput 将键名字符串转换为对应的字节序列
// 支持格式:
//   - 简单键名: "Enter", "Escape", "ArrowUp", "F1", ...
//   - Ctrl组合: "Ctrl+C", "Ctrl+D", "Ctrl+L", ...
//   - Alt组合: "Alt+x", "Alt+F4", ...
func ParseKeyInput(keyName string) ([]byte, error) {
	keyName = strings.TrimSpace(keyName)
	if keyName == "" {
		return nil, fmt.Errorf("empty key name")
	}

	// 检查是否是预定义键
	if seq, ok := keyMap[keyName]; ok {
		return seq, nil
	}

	// 检查 Ctrl+ 组合
	if strings.HasPrefix(keyName, "Ctrl+") || strings.HasPrefix(keyName, "ctrl+") {
		ch := strings.ToUpper(keyName[5:])
		if len(ch) == 1 {
			if seq, ok := ctrlMap[ch[0]]; ok {
				return seq, nil
			}
		}
		return nil, fmt.Errorf("unsupported Ctrl combination: %s", keyName)
	}

	// 检查 Alt+ 组合
	if strings.HasPrefix(keyName, "Alt+") || strings.HasPrefix(keyName, "alt+") {
		rest := keyName[4:]
		// Alt+方向键等
		if seq, ok := keyMap[rest]; ok {
			return append([]byte{0x1b}, seq...), nil
		}
		// Alt+字母/数字
		if len(rest) == 1 {
			return []byte{0x1b, rest[0]}, nil
		}
		return nil, fmt.Errorf("unsupported Alt combination: %s", keyName)
	}

	// 单字符键名（如 "C", "a", "1"），直接作为普通按键发送
	if len(keyName) == 1 {
		return []byte{keyName[0]}, nil
	}

	return nil, fmt.Errorf("unknown key: %s", keyName)
}
