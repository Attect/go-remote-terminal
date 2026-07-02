package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

const defaultMCPMaxReturnLen = 8000

type MCPOutputOptions struct {
	Lines        int
	MaxChars     int
	Raw          bool
	PreferScreen bool
}

type MCPOutputMeta struct {
	Truncated         bool `json:"truncated"`
	TotalLines        int  `json:"totalLines"`
	ReturnedLines     int  `json:"returnedLines"`
	ReturnedChars     int  `json:"returnedChars"`
	DynamicDetected   bool `json:"dynamicDetected"`
	FromScreen        bool `json:"fromScreen"`
	FromMark          bool `json:"fromMark"`
	Raw               bool `json:"raw"`
	SuggestFullOutput bool `json:"suggestFullOutput"`
}

type MCPOutputView struct {
	Text string        `json:"text"`
	Meta MCPOutputMeta `json:"meta"`
}

type MCPOutputSuggestion struct {
	PreferredTool string `json:"preferredTool"`
	Reason        string `json:"reason"`
	ShortHint     string `json:"shortHint"`
}

type MCPOutputEnvelope struct {
	Schema      string              `json:"schema"`
	Kind        string              `json:"kind"`
	Tool        string              `json:"tool"`
	SessionID   string              `json:"sessionId"`
	Command     string              `json:"command,omitempty"`
	Completed   bool                `json:"completed"`
	WaitTimeout int                 `json:"waitTimeoutMs,omitempty"`
	InputType   string              `json:"inputType,omitempty"`
	Submitted   bool                `json:"submitted,omitempty"`
	Wait        bool                `json:"wait,omitempty"`
	Output      MCPOutputView       `json:"output"`
	Suggestion  MCPOutputSuggestion `json:"suggestion"`
}

var progressLikePattern = regexp.MustCompile(`(?i)(\d{1,3}%|\[[=>#\-\.\s]{3,}\]|\b(done|eta|progress|download|upload|installing|extracting)\b)`)

func RenderMCPOutput(session *Session, raw []byte, opts MCPOutputOptions) MCPOutputView {
	if opts.Lines <= 0 {
		opts.Lines = 50
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = defaultMCPMaxReturnLen
	}

	dynamic := DetectDynamicTerminalOutput(raw)
	text := ""
	fromScreen := false

	if opts.Raw {
		text = CleanTerminalOutput(raw)
	} else if opts.PreferScreen && session != nil && session.vtScreen != nil && dynamic {
		text = session.vtScreen.GetScreen()
		fromScreen = true
	} else {
		text = StableTerminalText(raw)
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")

	totalLines := countOutputLines(text)
	text, returnedLines, lineTruncated := tailLines(text, opts.Lines)
	text, charTruncated := tailChars(text, opts.MaxChars)
	if charTruncated {
		returnedLines = countOutputLines(text)
	}

	meta := MCPOutputMeta{
		Truncated:         lineTruncated || charTruncated,
		TotalLines:        totalLines,
		ReturnedLines:     returnedLines,
		ReturnedChars:     len([]rune(text)),
		DynamicDetected:   dynamic,
		FromScreen:        fromScreen,
		FromMark:          true,
		Raw:               opts.Raw,
		SuggestFullOutput: lineTruncated || charTruncated || dynamic,
	}

	return MCPOutputView{Text: text, Meta: meta}
}

func FormatMCPOutput(title string, view MCPOutputView) string {
	var sb strings.Builder
	if title != "" {
		sb.WriteString(title)
		sb.WriteString("\n\n")
	}
	if view.Text == "" {
		sb.WriteString("（无可显示输出）")
	} else {
		sb.WriteString(view.Text)
	}
	sb.WriteString("\n\n---\n")
	sb.WriteString(formatMCPOutputMeta(view.Meta))
	return sb.String()
}

func formatMCPOutputMeta(meta MCPOutputMeta) string {
	mode := "尾部稳定文本"
	if meta.FromScreen {
		mode = "最终屏幕"
	} else if meta.Raw {
		mode = "原始输出清理"
	}
	return fmt.Sprintf("输出元信息: 模式=%s; 总行数=%d; 返回行数=%d; 返回字符=%d; 已截断=%t; 检测到动态刷新=%t; 建议完整输出=%t",
		mode, meta.TotalLines, meta.ReturnedLines, meta.ReturnedChars, meta.Truncated, meta.DynamicDetected, meta.SuggestFullOutput)
}

func DetectDynamicTerminalOutput(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if hasStandaloneCR(data) {
		return true
	}
	if bytes.Contains(data, []byte("\x1b[")) {
		for i := 0; i < len(data)-2; i++ {
			if data[i] == 0x1b && data[i+1] == '[' {
				for j := i + 2; j < len(data); j++ {
					b := data[j]
					if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' || b == '@' {
						switch b {
						case 'A', 'B', 'C', 'D', 'G', 'H', 'f', 'J', 'K', 's', 'u':
							return true
						}
						break
					}
				}
			}
		}
	}
	return progressLikePattern.MatchString(CleanTerminalOutput(data))
}

func hasStandaloneCR(data []byte) bool {
	for i, b := range data {
		if b == '\r' && (i+1 >= len(data) || data[i+1] != '\n') {
			return true
		}
	}
	return false
}

func StableTerminalText(data []byte) string {
	clean := CleanTerminalOutput(data)
	lines := strings.Split(clean, "\n")
	stable := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			if len(stable) == 0 || stable[len(stable)-1] != "" {
				stable = append(stable, line)
			}
			continue
		}
		if len(stable) > 0 && stable[len(stable)-1] == line && progressLikePattern.MatchString(line) {
			continue
		}
		stable = append(stable, line)
	}
	return strings.Join(stable, "\n")
}

func tailLines(text string, limit int) (string, int, bool) {
	if text == "" {
		return "", 0, false
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= limit {
		return text, len(lines), false
	}
	return strings.Join(lines[len(lines)-limit:], "\n"), limit, true
}

func tailChars(text string, limit int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= limit {
		return text, false
	}
	return "...（输出过长，前面部分已截断）...\n" + string(runes[len(runes)-limit:]), true
}

func countOutputLines(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}
