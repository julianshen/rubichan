package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
)

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func shellTool() provider.ToolDef {
	return provider.ToolDef{
		Name:        "shell",
		Description: "Execute shell commands.",
		InputSchema: mustMarshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The command to run",
				},
			},
			"required": []string{"command"},
		}),
	}
}

func fileTool() provider.ToolDef {
	return provider.ToolDef{
		Name:        "file",
		Description: "Read or write files.",
		InputSchema: mustMarshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "File content to write",
				},
			},
			"required": []string{"path"},
		}),
	}
}

// --- RenderToolsAsText tests ---

func TestRenderToolsAsText_Header(t *testing.T) {
	out := RenderToolsAsText([]provider.ToolDef{shellTool()})

	if !strings.Contains(out, "## Tools") {
		t.Error("expected '## Tools' header")
	}
	if !strings.Contains(out, "<tool_use>") {
		t.Error("expected XML example with <tool_use>")
	}
	if !strings.Contains(out, "<name>TOOL_NAME</name>") {
		t.Error("expected <name>TOOL_NAME</name> placeholder in example")
	}
	if !strings.Contains(out, "<input>") {
		t.Error("expected <input> placeholder in example")
	}
	if !strings.Contains(out, "multiple") {
		t.Error("expected note about multiple tool calls")
	}
}

func TestRenderToolsAsText_ToolSection(t *testing.T) {
	out := RenderToolsAsText([]provider.ToolDef{shellTool()})

	if !strings.Contains(out, "### shell") {
		t.Error("expected '### shell' section")
	}
	if !strings.Contains(out, "Execute shell commands.") {
		t.Error("expected tool description")
	}
	if !strings.Contains(out, "**Parameters:**") {
		t.Error("expected parameters header")
	}
	if !strings.Contains(out, "`command`") {
		t.Error("expected command parameter name")
	}
	if !strings.Contains(out, "string") {
		t.Error("expected parameter type")
	}
	if !strings.Contains(out, "**(required)**") {
		t.Error("expected required marker")
	}
	if !strings.Contains(out, "The command to run") {
		t.Error("expected parameter description")
	}
}

func TestRenderToolsAsText_MultipleTools(t *testing.T) {
	out := RenderToolsAsText([]provider.ToolDef{shellTool(), fileTool()})

	if !strings.Contains(out, "### shell") {
		t.Error("expected shell section")
	}
	if !strings.Contains(out, "### file") {
		t.Error("expected file section")
	}
}

func TestRenderToolsAsText_OptionalParam(t *testing.T) {
	out := RenderToolsAsText([]provider.ToolDef{fileTool()})

	// path is required, content is optional
	if !strings.Contains(out, "`path`") {
		t.Error("expected path param")
	}
	if !strings.Contains(out, "`content`") {
		t.Error("expected content param")
	}
	// content should NOT have (required)
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.Contains(line, "`content`") && strings.Contains(line, "**(required)**") {
			t.Error("content should not be marked required")
		}
	}
}

func TestRenderToolsAsText_Empty(t *testing.T) {
	out := RenderToolsAsText([]provider.ToolDef{})
	// Should still produce header even with no tools
	if !strings.Contains(out, "## Tools") {
		t.Error("expected Tools header even with empty list")
	}
}

func TestRenderToolsAsText_NoSchema(t *testing.T) {
	tool := provider.ToolDef{
		Name:        "noop",
		Description: "Does nothing.",
		InputSchema: nil,
	}
	out := RenderToolsAsText([]provider.ToolDef{tool})
	if !strings.Contains(out, "### noop") {
		t.Error("expected noop section")
	}
}

// --- ParseTextToolCalls tests ---

func TestParseTextToolCalls_Single(t *testing.T) {
	text := `Let me run a command.
<tool_use>
<name>shell</name>
<input>{"command": "ls -la"}</input>
</tool_use>
Done.`

	calls := ParseTextToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("expected name 'shell', got %q", calls[0].Name)
	}
	var input map[string]string
	if err := json.Unmarshal(calls[0].Input, &input); err != nil {
		t.Fatalf("input not valid JSON: %v", err)
	}
	if input["command"] != "ls -la" {
		t.Errorf("expected command 'ls -la', got %q", input["command"])
	}
}

func TestParseTextToolCalls_Multiple(t *testing.T) {
	text := `
<tool_use>
<name>shell</name>
<input>{"command": "pwd"}</input>
</tool_use>
Some text in between.
<tool_use>
<name>file</name>
<input>{"path": "/tmp/foo.txt"}</input>
</tool_use>
`
	calls := ParseTextToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("first call: expected 'shell', got %q", calls[0].Name)
	}
	if calls[1].Name != "file" {
		t.Errorf("second call: expected 'file', got %q", calls[1].Name)
	}
}

func TestParseTextToolCalls_NoToolCalls(t *testing.T) {
	text := "This is a plain response with no tool calls."
	calls := ParseTextToolCalls(text)
	if calls == nil {
		t.Error("expected non-nil slice")
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestParseTextToolCalls_MalformedJSON(t *testing.T) {
	// Malformed JSON should still be captured, not dropped
	text := `<tool_use>
<name>shell</name>
<input>{bad json}</input>
</tool_use>`

	calls := ParseTextToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call even with malformed JSON, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("expected name 'shell', got %q", calls[0].Name)
	}
	// Input should be captured as-is
	if string(calls[0].Input) == "" {
		t.Error("expected input to be non-empty even if invalid JSON")
	}
}

func TestParseTextToolCalls_WhitespaceVariations(t *testing.T) {
	text := "<tool_use><name>  shell  </name><input>  {\"command\":\"echo hi\"}  </input></tool_use>"

	calls := ParseTextToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("expected 'shell', got %q", calls[0].Name)
	}
}

func TestParseTextToolCalls_ExtraWhitespaceBetweenTags(t *testing.T) {
	text := `<tool_use>
  <name>shell</name>
  <input>{"command": "date"}</input>
</tool_use>`

	calls := ParseTextToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("expected 'shell', got %q", calls[0].Name)
	}
}
