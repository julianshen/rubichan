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

func TestIsDiffContent(t *testing.T) {
	assert.True(t, isDiffContent("@@ -1,3 +1,4 @@\n+added\n"))
	assert.True(t, isDiffContent("some header\n@@ -0,0 +1 @@\n+new\n"))
	assert.False(t, isDiffContent("just plain text\n"))
	assert.False(t, isDiffContent("+not a diff\n-also not\n"))
	assert.False(t, isDiffContent(""))
}

func TestColorizeDiffLines_AddedLines(t *testing.T) {
	input := "@@ -1,3 +1,4 @@\n context line\n+added line\n+another added\n"
	result := ColorizeDiffLines(input)
	// Result must preserve all original text
	assert.Contains(t, result, "added line")
	assert.Contains(t, result, "another added")
	assert.Contains(t, result, "context line")
}

func TestColorizeDiffLines_RemovedLines(t *testing.T) {
	input := "@@ -1,3 +1,2 @@\n context\n-removed line\n-another removed\n"
	result := ColorizeDiffLines(input)
	assert.Contains(t, result, "removed line")
	assert.Contains(t, result, "another removed")
}

func TestColorizeDiffLines_HeaderLines(t *testing.T) {
	input := "@@ -1,3 +1,4 @@\n context\n+added\n"
	result := ColorizeDiffLines(input)
	assert.Contains(t, result, "@@ -1,3 +1,4 @@")
}

func TestColorizeDiffLines_FileHeadersUntouched(t *testing.T) {
	// +++ and --- file headers should pass through isDiffContent but
	// not be treated like added/removed lines.
	input := "@@ -1,3 +1,4 @@\n--- a/file.go\n+++ b/file.go\n+added\n"
	result := ColorizeDiffLines(input)
	lines := strings.Split(result, "\n")
	// --- and +++ lines should be in the output
	found := 0
	for _, l := range lines {
		if strings.Contains(l, "--- a/file.go") || strings.Contains(l, "+++ b/file.go") {
			found++
		}
	}
	assert.Equal(t, 2, found, "both file header lines should be present")
}

func TestColorizeDiffLines_PlainLines(t *testing.T) {
	// No @@ header means no diff detected — content returned unchanged
	input := "just some plain text\nwith multiple lines\n+not a diff line\n"
	result := ColorizeDiffLines(input)
	assert.Equal(t, input, result)
}

func TestColorizeDiffLines_EmptyInput(t *testing.T) {
	assert.Equal(t, "", ColorizeDiffLines(""))
}
