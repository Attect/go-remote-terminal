package main

import "testing"

func TestStructuredTextResultAddsStructuredContent(t *testing.T) {
	result := structuredTextResult("ok", map[string]interface{}{"a": 1})
	if result.StructuredContent == nil {
		t.Fatalf("expected structured content")
	}
}

func TestBuildMCPOutputPayloadHasStableSchema(t *testing.T) {
	view := MCPOutputView{Text: "ok", Meta: MCPOutputMeta{TotalLines: 1}}
	payload := buildMCPOutputPayload("get_output", "sid-1", "", view)
	if payload.Schema != "grt.mcp.output.v1" {
		t.Fatalf("unexpected schema: %#v", payload.Schema)
	}
	if payload.Kind != "terminal_output" {
		t.Fatalf("unexpected kind: %#v", payload.Kind)
	}
	if payload.Suggestion.PreferredTool == "" {
		t.Fatalf("expected suggestion to be populated")
	}
}

func TestAllMCPToolsIncludesWaitOutput(t *testing.T) {
	tools := AllMCPTools()
	found := false
	for _, tool := range tools {
		if tool.Name == "wait_output" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("wait_output tool not found")
	}
}

func TestAllMCPToolsIncludesSendCommand(t *testing.T) {
	tools := AllMCPTools()
	found := false
	for _, tool := range tools {
		if tool.Name == "send_command" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("send_command tool not found")
	}
}

func TestAllMCPToolsDoesNotIncludeExecute(t *testing.T) {
	for _, tool := range AllMCPTools() {
		if tool.Name == "execute" {
			t.Fatalf("execute should not be exposed in the new MCP tool system")
		}
	}
}
