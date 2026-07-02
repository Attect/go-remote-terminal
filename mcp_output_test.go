package main

import (
	"strings"
	"testing"
)

func TestRenderMCPOutputUsesFinalScreenForDynamicRefresh(t *testing.T) {
	session := &Session{vtScreen: NewVTScreen(5, 40)}
	raw := []byte("progress 10%\rprogress 50%\rprogress 100%\n")
	session.vtScreen.Feed(raw)

	view := RenderMCPOutput(session, raw, MCPOutputOptions{Lines: 10, PreferScreen: true})

	if !view.Meta.DynamicDetected {
		t.Fatalf("expected dynamic output to be detected")
	}
	if !view.Meta.FromScreen {
		t.Fatalf("expected screen view for dynamic output")
	}
	if strings.Contains(view.Text, "10%") || strings.Contains(view.Text, "50%") {
		t.Fatalf("expected intermediate progress frames to be hidden, got %q", view.Text)
	}
	if !strings.Contains(view.Text, "100%") {
		t.Fatalf("expected final progress frame, got %q", view.Text)
	}
}

func TestRenderMCPOutputMetaReportsTruncation(t *testing.T) {
	raw := []byte("a\nb\nc\nd")

	view := RenderMCPOutput(nil, raw, MCPOutputOptions{Lines: 2})

	if !view.Meta.Truncated {
		t.Fatalf("expected truncation metadata")
	}
	if view.Meta.TotalLines != 4 || view.Meta.ReturnedLines != 2 {
		t.Fatalf("unexpected line metadata: %+v", view.Meta)
	}
	if view.Text != "c\nd" {
		t.Fatalf("unexpected tail text %q", view.Text)
	}
}

func TestCRLFDoesNotTriggerDynamicRefresh(t *testing.T) {
	if DetectDynamicTerminalOutput([]byte("line1\r\nline2\r\n")) {
		t.Fatalf("plain CRLF output should not be treated as dynamic refresh")
	}
}

func TestFullOutputOptionsKeepRawUntruncatedText(t *testing.T) {
	text := strings.Repeat("x", defaultMCPMaxReturnLen+20)

	view := RenderMCPOutput(nil, []byte(text), mcpOutputOptions(1, false, true))

	if view.Meta.Truncated {
		t.Fatalf("full output should not use default truncation")
	}
	if view.Text != text {
		t.Fatalf("expected full output length %d, got %d", len(text), len(view.Text))
	}
	if !view.Meta.Raw {
		t.Fatalf("full output should enable raw mode")
	}
}
