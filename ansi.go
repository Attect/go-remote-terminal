package main

import (
	"bytes"
	"sync"
	"unicode/utf8"
)

// ==================== ANSI 去除 ====================

// ansiPattern 匹配 ANSI 转义序列
// 包含: ESC[...m (SGR), ESC[...H/f/J/K (CSI), ESC]...\x07 (OSC), ESC[...A/B/C/D (光标移动)
// 这是一个简化版的状态机，直接扫描字节流去除转义序列
func StripANSI(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i := 0; i < len(data); {
		c := data[i]
		if c == 0x1b && i+1 < len(data) {
			next := data[i+1]
			if next == '[' {
				// CSI 序列 ESC[ ... 字母或 ~ 或 @ 结束
				j := i + 2
				for j < len(data) {
					b := data[j]
					if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' || b == '@' {
						j++
						break
					}
					j++
				}
				i = j
				continue
			} else if next == ']' {
				// OSC 序列 ESC] ... BEL 或 ESC\\ 结束
				j := i + 2
				for j < len(data) {
					if data[j] == 0x07 {
						j++
						break
					}
					if data[j] == 0x1b && j+1 < len(data) && data[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			} else if next == '(' || next == ')' || next == '*' || next == '+' || next == '-' || next == '.' {
				// 三字符序列 ESC ( X
				i += 3
				continue
			} else if next >= 0x40 && next <= 0x5f {
				// 两字符序列 ESC X
				i += 2
				continue
			}
		}
		// 跳过其他控制字符（保留换行回车制表）
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			i++
			continue
		}
		buf.WriteByte(c)
		i++
	}
	return buf.String()
}

// NormalizeCR 模拟终端的 \r 行为，将回车控制符转换为干净的文本
// \r\n → 普通换行
// 单独的 \r → 清空当前行（模拟回车不换行，后续内容覆盖当前行）
// \n → 普通换行
func NormalizeCR(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	var buf bytes.Buffer
	var line []byte
	i := 0
	for i < len(data) {
		c := data[i]
		if c == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				// \r\n：当作普通换行
				buf.Write(line)
				buf.WriteByte('\n')
				line = line[:0]
				i += 2
				continue
			}
			// 单独 \r：清空当前行（回车不换行，后续覆盖）
			line = line[:0]
		} else if c == '\n' {
			buf.Write(line)
			buf.WriteByte('\n')
			line = line[:0]
		} else {
			line = append(line, c)
		}
		i++
	}
	buf.Write(line)
	return buf.Bytes()
}

// CleanTerminalOutput 去除ANSI控制序列并归一化\r回车符，返回干净的文本
func CleanTerminalOutput(data []byte) string {
	stripped := StripANSI(data)
	normalized := NormalizeCR([]byte(stripped))
	return string(normalized)
}

// ==================== 轻量 VT 模拟器 ====================

// VTScreen 轻量虚拟终端，维护字符网格
// 仅处理最常用的控制序列，用于 terminal_get_screen 捕获当前可见屏幕内容
type VTScreen struct {
	rows   int
	cols   int
	grid   [][]rune // 字符网格
	cursor struct{ row, col int }
	mu     sync.Mutex
}

func NewVTScreen(rows, cols int) *VTScreen {
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}
	s := &VTScreen{
		rows: rows,
		cols: cols,
	}
	s.resetGrid()
	return s
}

func (s *VTScreen) resetGrid() {
	s.grid = make([][]rune, s.rows)
	for i := range s.grid {
		s.grid[i] = make([]rune, s.cols)
		for j := range s.grid[i] {
			s.grid[i][j] = ' '
		}
	}
	s.cursor.row = 0
	s.cursor.col = 0
}

// Feed 消费原始 PTY 输出，更新虚拟屏幕
func (s *VTScreen) Feed(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == 0x1b && i+1 < len(data) {
			next := data[i+1]
			if next == '[' {
				// CSI 序列
				seq, newI := s.parseCSI(data, i+2)
				i = newI
				s.handleCSI(seq)
				continue
			} else if next == ']' {
				// OSC 序列，跳过
				j := i + 2
				for j < len(data) {
					if data[j] == 0x07 {
						j++
						break
					}
					if data[j] == 0x1b && j+1 < len(data) && data[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j - 1
				continue
			}
			// 其他 ESC 序列，跳过下一个字符
			i++
			continue
		}

		switch c {
		case '\n':
			s.cursor.row++
			if s.cursor.row >= s.rows {
				// 需要滚动：上移一行，清空最后一行
				copy(s.grid, s.grid[1:])
				s.grid[s.rows-1] = make([]rune, s.cols)
				for j := range s.grid[s.rows-1] {
					s.grid[s.rows-1][j] = ' '
				}
				s.cursor.row = s.rows - 1
			}
		case '\r':
			s.cursor.col = 0
		case '\t':
			// 移动到下一个8列边界
			s.cursor.col = ((s.cursor.col / 8) + 1) * 8
			if s.cursor.col >= s.cols {
				s.cursor.col = s.cols - 1
			}
		case '\b':
			if s.cursor.col > 0 {
				s.cursor.col--
			}
		case '\x07': // BEL
			// 忽略
		default:
			if c < 0x20 {
				// 其他控制字符忽略
				continue
			}
			// 可打印字符（含UTF-8多字节）
			r, size := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && size == 1 {
				// 无效UTF-8，当做单个字节处理
				r = rune(c)
				size = 1
			}
			if s.cursor.row < s.rows && s.cursor.col < s.cols {
				s.grid[s.cursor.row][s.cursor.col] = r
				s.cursor.col++
				if s.cursor.col >= s.cols {
					s.cursor.col = 0
					s.cursor.row++
					if s.cursor.row >= s.rows {
						copy(s.grid, s.grid[1:])
						s.grid[s.rows-1] = make([]rune, s.cols)
						for j := range s.grid[s.rows-1] {
							s.grid[s.rows-1][j] = ' '
						}
						s.cursor.row = s.rows - 1
					}
				}
				i += size - 1 // 循环会自动+1，所以这里减1
			}
		}
	}
}

// parseCSI 解析CSI序列，返回参数和结束位置
func (s *VTScreen) parseCSI(data []byte, start int) (csi []byte, end int) {
	for j := start; j < len(data); j++ {
		b := data[j]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' || b == '@' {
			return data[start : j+1], j
		}
		if b < 0x20 || b > 0x7e {
			// 异常终止
			return data[start:j], j - 1
		}
	}
	return data[start:], len(data) - 1
}

// handleCSI 处理CSI序列
func (s *VTScreen) handleCSI(seq []byte) {
	if len(seq) == 0 {
		return
	}
	final := seq[len(seq)-1]
	params := s.parseParams(seq[:len(seq)-1])

	switch final {
	case 'H', 'f': // 光标定位 ESC[<row>;<col>H
		row, col := 1, 1
		if len(params) > 0 {
			row = params[0]
		}
		if len(params) > 1 {
			col = params[1]
		}
		if row < 1 {
			row = 1
		}
		if col < 1 {
			col = 1
		}
		s.cursor.row = row - 1
		s.cursor.col = col - 1
		if s.cursor.row >= s.rows {
			s.cursor.row = s.rows - 1
		}
		if s.cursor.col >= s.cols {
			s.cursor.col = s.cols - 1
		}
	case 'A': // 光标上移
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		s.cursor.row -= n
		if s.cursor.row < 0 {
			s.cursor.row = 0
		}
	case 'B': // 光标下移
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		s.cursor.row += n
		if s.cursor.row >= s.rows {
			s.cursor.row = s.rows - 1
		}
	case 'C': // 光标右移
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		s.cursor.col += n
		if s.cursor.col >= s.cols {
			s.cursor.col = s.cols - 1
		}
	case 'D': // 光标左移
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		s.cursor.col -= n
		if s.cursor.col < 0 {
			s.cursor.col = 0
		}
	case 'J': // 清屏
		mode := 0
		if len(params) > 0 {
			mode = params[0]
		}
		switch mode {
		case 0: // 从光标到末尾
			for c := s.cursor.col; c < s.cols; c++ {
				s.grid[s.cursor.row][c] = ' '
			}
			for r := s.cursor.row + 1; r < s.rows; r++ {
				for c := 0; c < s.cols; c++ {
					s.grid[r][c] = ' '
				}
			}
		case 1: // 从开头到光标
			for r := 0; r < s.cursor.row; r++ {
				for c := 0; c < s.cols; c++ {
					s.grid[r][c] = ' '
				}
			}
			for c := 0; c <= s.cursor.col; c++ {
				s.grid[s.cursor.row][c] = ' '
			}
		case 2, 3: // 全部清除
			s.resetGrid()
		}
	case 'K': // 擦除行
		mode := 0
		if len(params) > 0 {
			mode = params[0]
		}
		switch mode {
		case 0: // 从光标到行尾
			for c := s.cursor.col; c < s.cols; c++ {
				s.grid[s.cursor.row][c] = ' '
			}
		case 1: // 从行头到光标
			for c := 0; c <= s.cursor.col; c++ {
				s.grid[s.cursor.row][c] = ' '
			}
		case 2: // 整行
			for c := 0; c < s.cols; c++ {
				s.grid[s.cursor.row][c] = ' '
			}
		}
	case 'L': // 插入行（IL）
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		if s.cursor.row < s.rows {
			endRow := s.cursor.row + n
			if endRow > s.rows {
				endRow = s.rows
			}
			copy(s.grid[endRow:], s.grid[s.cursor.row:s.rows-n])
			for r := s.cursor.row; r < endRow; r++ {
				s.grid[r] = make([]rune, s.cols)
				for c := range s.grid[r] {
					s.grid[r][c] = ' '
				}
			}
		}
	case 'M': // 删除行（DL）
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		if s.cursor.row < s.rows {
			endRow := s.cursor.row + n
			if endRow > s.rows {
				endRow = s.rows
			}
			copy(s.grid[s.cursor.row:], s.grid[endRow:])
			for r := s.rows - n; r < s.rows; r++ {
				s.grid[r] = make([]rune, s.cols)
				for c := range s.grid[r] {
					s.grid[r][c] = ' '
				}
			}
		}
	case 'P': // 删除字符（DCH）
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		if s.cursor.row < s.rows && s.cursor.col < s.cols {
			copy(s.grid[s.cursor.row][s.cursor.col:], s.grid[s.cursor.row][s.cursor.col+n:])
			for c := s.cols - n; c < s.cols; c++ {
				if c >= 0 {
					s.grid[s.cursor.row][c] = ' '
				}
			}
		}
	case 'X': // 擦除字符（ECH）
		n := 1
		if len(params) > 0 && params[0] > 0 {
			n = params[0]
		}
		if s.cursor.row < s.rows && s.cursor.col < s.cols {
			end := s.cursor.col + n
			if end > s.cols {
				end = s.cols
			}
			for c := s.cursor.col; c < end; c++ {
				s.grid[s.cursor.row][c] = ' '
			}
		}
	case 'G': // 光标水平绝对定位（CHA）
		col := 1
		if len(params) > 0 && params[0] > 0 {
			col = params[0]
		}
		s.cursor.col = col - 1
		if s.cursor.col < 0 {
			s.cursor.col = 0
		}
		if s.cursor.col >= s.cols {
			s.cursor.col = s.cols - 1
		}
	case 'd': // 光标垂直绝对定位（VPA）
		row := 1
		if len(params) > 0 && params[0] > 0 {
			row = params[0]
		}
		s.cursor.row = row - 1
		if s.cursor.row < 0 {
			s.cursor.row = 0
		}
		if s.cursor.row >= s.rows {
			s.cursor.row = s.rows - 1
		}
	}
}

// parseParams 解析CSI参数，如 "2;3" → [2,3]，"" → [0]
func (s *VTScreen) parseParams(data []byte) []int {
	if len(data) == 0 {
		return []int{0}
	}
	var result []int
	var current int
	var hasCurrent bool
	for _, b := range data {
		if b >= '0' && b <= '9' {
			current = current*10 + int(b-'0')
			hasCurrent = true
		} else if b == ';' || b == ':' {
			if hasCurrent {
				result = append(result, current)
			} else {
				result = append(result, 0)
			}
			current = 0
			hasCurrent = false
		}
		// 其他字符（如 ? = > < !）属于私有序列前缀，忽略
	}
	if hasCurrent {
		result = append(result, current)
	} else if len(result) > 0 || len(data) > 0 {
		result = append(result, 0)
	}
	if len(result) == 0 {
		return []int{0}
	}
	return result
}

// GetScreen 返回当前屏幕内容，去除尾部空格
func (s *VTScreen) GetScreen() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var buf bytes.Buffer
	for r := 0; r < s.rows; r++ {
		// 找到每行最后一个非空格字符
		lastNonSpace := -1
		for c := s.cols - 1; c >= 0; c-- {
			if s.grid[r][c] != ' ' {
				lastNonSpace = c
				break
			}
		}
		if lastNonSpace >= 0 {
			for c := 0; c <= lastNonSpace; c++ {
				buf.WriteRune(s.grid[r][c])
			}
		}
		if r < s.rows-1 {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}
