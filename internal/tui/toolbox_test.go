package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderToolCall(t *testing.T) {
	r := NewToolBoxRenderer(60)
	result := r.RenderToolCall("file_read", `"src/main.go"`)
	assert.Contains(t, result, "file_read")
	assert.Contains(t, result, "src/main.go")
	assert.Contains(t, result, "\u256d")
	assert.Contains(t, result, "\u2570")
}

func TestRenderToolResult(t *testing.T) {
	r := NewToolBoxRenderer(60)
	result := r.RenderToolResult("file_read", "package main\n\nfunc main() {}", false)
	assert.Contains(t, result, "main")
	assert.Contains(t, result, "\u256d")
}

func TestRenderToolResultError(t *testing.T) {
	r := NewToolBoxRenderer(60)
	result := r.RenderToolResult("shell", "command not found", true)
	assert.Contains(t, result, "command not found")
}

func TestRenderToolResultTruncation(t *testing.T) {
	r := NewToolBoxRenderer(60)
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "line content here\n"
	}
	result := r.RenderToolResult("file_read", longContent, false)
	assert.Contains(t, result, "more lines")
}

func TestNewToolBoxRendererMinWidth(t *testing.T) {
	// Width too small should clamp to 20
	r := NewToolBoxRenderer(10)
	assert.NotNil(t, r)
	assert.Equal(t, 10, r.width)
	// Render should not panic
	result := r.RenderToolCall("test", "arg")
	assert.Contains(t, result, "test")
}

func TestRenderToolResultExactlyMaxLines(t *testing.T) {
	r := NewToolBoxRenderer(60)
	lines := make([]string, maxToolResultLines)
	for i := range lines {
		lines[i] = "line"
	}
	content := strings.Join(lines, "\n")
	result := r.RenderToolResult("test", content, false)
	assert.NotContains(t, result, "more lines")
}

func TestRenderToolResultEmptyContent(t *testing.T) {
	r := NewToolBoxRenderer(60)
	result := r.RenderToolResult("test", "", false)
	assert.Contains(t, result, "\u256d")
}
