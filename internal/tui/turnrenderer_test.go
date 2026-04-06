// internal/tui/turnrenderer_test.go
package tui

import (
	"testing"
	"time"
)

func TestTurn_StructFields(t *testing.T) {
	turn := &Turn{
		ID:            "turn-1",
		AssistantText: "Hello",
		ThinkingText:  "Thinking...",
		ToolCalls:     []RenderedToolCall{},
		Status:        "done",
		ErrorMsg:      "",
		StartTime:     time.Now(),
	}

	if turn.ID != "turn-1" {
		t.Errorf("ID not set correctly")
	}
	if turn.AssistantText != "Hello" {
		t.Errorf("AssistantText not set correctly")
	}
	if len(turn.ToolCalls) != 0 {
		t.Errorf("ToolCalls should be empty slice")
	}
}

func TestRenderedToolCall_StructFields(t *testing.T) {
	call := RenderedToolCall{
		ID:        "tool-1",
		Name:      "file",
		Args:      "path=main.go",
		Result:    "package main",
		IsError:   false,
		Collapsed: true,
		LineCount: 100,
	}

	if call.Name != "file" {
		t.Errorf("Name not set correctly")
	}
	if call.Collapsed != true {
		t.Errorf("Collapsed should be true")
	}
}

func TestRenderOptions_StructFields(t *testing.T) {
	opts := RenderOptions{
		Width:          80,
		IsStreaming:    true,
		CollapsedTools: false,
		HighlightError: false,
		MaxToolLines:   500,
	}

	if opts.Width != 80 {
		t.Errorf("Width not set correctly")
	}
}
