package tui

import (
	"fmt"
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
	result := r.RenderToolResult("package main\n\nfunc main() {}", false, "file")
	assert.Contains(t, result, "main")
	assert.Contains(t, result, "\u256d")
}

func TestRenderToolResultError(t *testing.T) {
	r := NewToolBoxRenderer(60)
	result := r.RenderToolResult("command not found", true, "shell")
	assert.Contains(t, result, "command not found")
}

func TestRenderToolResultTruncation(t *testing.T) {
	r := NewToolBoxRenderer(60)
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "line content here\n"
	}
	result := r.RenderToolResult(longContent, false, "shell")
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
	result := r.RenderToolResult(content, false, "file")
	assert.NotContains(t, result, "more lines")
}

func TestRenderToolResultEmptyContent(t *testing.T) {
	r := NewToolBoxRenderer(60)
	result := r.RenderToolResult("", false, "")
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

func TestCollapsibleToolResult_CollapsedView(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `path="src/main.go"`,
		Content:   "package main\n\nfunc main() {}\n",
		LineCount: 3,
		Collapsed: true,
	}
	result := cr.Render(r)
	// Collapsed view should show summary line with ▶ indicator
	assert.Contains(t, result, "▶")
	assert.Contains(t, result, "file_read")
	assert.Contains(t, result, "3 lines")
	// Should NOT contain the full content
	assert.NotContains(t, result, "package main")
}

func TestCollapsibleToolResult_ExpandedView(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `path="src/main.go"`,
		Content:   "package main\n\nfunc main() {}\n",
		LineCount: 3,
		Collapsed: false,
	}
	result := cr.Render(r)
	// Expanded view should show ▼ indicator and full content
	assert.Contains(t, result, "▼")
	assert.Contains(t, result, "file_read")
	assert.Contains(t, result, "package main")
}

func TestCollapsibleToolResult_ErrorBorder(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="ls"`,
		Content:   "command not found",
		LineCount: 1,
		IsError:   true,
		Collapsed: false,
	}
	result := cr.Render(r)
	// Error results should still render content
	assert.Contains(t, result, "command not found")
}

func TestCollapsibleToolResult_LineCounting(t *testing.T) {
	r := NewToolBoxRenderer(60)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `path="big.go"`,
		Content:   strings.Join(lines, "\n"),
		LineCount: 50,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "50 lines (20 shown)")
}

func TestToolResultPlaceholder(t *testing.T) {
	assert.Equal(t, "\x00TR:0\x00", toolResultPlaceholder(0))
	assert.Equal(t, "\x00TR:42\x00", toolResultPlaceholder(42))
}

func TestReplaceToolResultPlaceholders(t *testing.T) {
	r := NewToolBoxRenderer(60)
	results := []CollapsibleToolResult{
		{ID: 0, Name: "file_read", Args: `path="a.go"`, Content: "pkg a", LineCount: 1, Collapsed: true},
		{ID: 1, Name: "shell", Args: `cmd="ls"`, Content: "dir output", LineCount: 1, Collapsed: false},
	}
	content := "before\n" + toolResultPlaceholder(0) + "middle\n" + toolResultPlaceholder(1) + "after"
	replaced := replaceToolResultPlaceholders(content, results, r)
	// Collapsed result should show ▶
	assert.Contains(t, replaced, "▶")
	assert.Contains(t, replaced, "file_read")
	// Expanded result should show ▼ and content
	assert.Contains(t, replaced, "▼")
	assert.Contains(t, replaced, "dir output")
	// Placeholders should be gone
	assert.NotContains(t, replaced, "\x00TR:")
}

func TestCollapsibleToolResult_EmptyContentLabel(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        0,
		Name:      "shell",
		Args:      `command="true"`,
		Content:   "",
		LineCount: 0,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "empty")
	assert.NotContains(t, result, "0 lines")
}

func TestCollapsibleToolResult_SingleLineLabel(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        0,
		Name:      "shell",
		Args:      `command="echo hi"`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "1 line")
	assert.NotContains(t, result, "1 lines")
}

func TestCollapsibleToolResult_ShellIcon(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="ls"`,
		Content:   "file1\nfile2",
		LineCount: 2,
		ToolType:  ToolTypeShell,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "❯ ")
	assert.Contains(t, result, "▶")
}

func TestCollapsibleToolResult_FileIcon(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `{"path":"main.go"}`,
		Content:   "package main",
		LineCount: 1,
		ToolType:  ToolTypeFile,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "◇ ")
}

func TestCollapsibleToolResult_DefaultIcon(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "custom",
		Args:      "",
		Content:   "output",
		LineCount: 1,
		ToolType:  ToolTypeDefault,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "• ")
}

func TestCollapsibleToolResult_StatusOk(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `{"command":"ls"}`,
		Content:   "file1\nfile2",
		LineCount: 2,
		ToolType:  ToolTypeShell,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "✓")
}

func TestCollapsibleToolResult_StatusError(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `{"command":"make"}`,
		Content:   "error: build failed",
		LineCount: 1,
		ToolType:  ToolTypeShell,
		IsError:   true,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "✗")
}

func TestCollapsibleToolResult_StatusWithTruncation(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `{"command":"find /"}`,
		Content:   strings.Repeat("line\n", 50),
		LineCount: 50,
		ToolType:  ToolTypeShell,
		IsError:   true,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "50 lines (20 shown)")
	assert.Contains(t, result, "✗")
}

func TestCollapsibleToolResult_StatusForAllToolTypes(t *testing.T) {
	r := NewToolBoxRenderer(60)
	// All tool types now show status, not just shell.
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `{"path":"a.go"}`,
		Content:   "content",
		LineCount: 1,
		ToolType:  ToolTypeFile,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "✓")
}

func TestCollapsibleToolResult_FullyExpanded(t *testing.T) {
	r := NewToolBoxRenderer(80)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	cr := &CollapsibleToolResult{
		ID:            1,
		Name:          "file_read",
		Args:          `path="big.go"`,
		Content:       strings.Join(lines, "\n"),
		LineCount:     50,
		Collapsed:     false,
		FullyExpanded: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "line 50")
	assert.NotContains(t, result, "more lines")
	// When fully expanded, label should say "50 lines" not "50 lines (20 shown)"
	assert.Contains(t, result, "50 lines")
	assert.NotContains(t, result, "20 shown")
}

func TestCollapsibleToolResult_TruncatedShowsExpandHint(t *testing.T) {
	r := NewToolBoxRenderer(80)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	cr := &CollapsibleToolResult{
		ID:            1,
		Name:          "file_read",
		Args:          `path="big.go"`,
		Content:       strings.Join(lines, "\n"),
		LineCount:     50,
		Collapsed:     false,
		FullyExpanded: false,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "30 more lines")
	assert.Contains(t, result, "Ctrl+E")
	assert.NotContains(t, result, "line 50")
}

func TestCollapsibleToolResult_ShortNoExpandHint(t *testing.T) {
	r := NewToolBoxRenderer(80)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="echo hi"`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: false,
	}
	result := cr.Render(r)
	assert.NotContains(t, result, "Ctrl+E")
	assert.NotContains(t, result, "more lines")
}

func TestToggleFullExpandMostRecent(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "shell", LineCount: 5, Collapsed: true},
		{ID: 1, Name: "file_read", LineCount: 50, Collapsed: false, FullyExpanded: false},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
	assert.True(t, results[1].FullyExpanded)
}

func TestToggleFullExpandMostRecentToggleOff(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "file_read", LineCount: 50, Collapsed: false, FullyExpanded: true},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
}

func TestToggleFullExpandSkipsCollapsed(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "file_read", LineCount: 50, Collapsed: true},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
}

func TestToggleFullExpandSkipsShortResults(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "shell", LineCount: 5, Collapsed: false},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
}

func TestCollapsibleThinking_Collapsed(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ct := &CollapsibleThinking{
		ID:        0,
		Content:   "Let me think about this...\nFirst, I need to consider X.\nThen Y.",
		LineCount: 3,
		Collapsed: true,
	}
	result := ct.Render(r)
	assert.Contains(t, result, "▶")
	assert.Contains(t, result, "Thinking")
	assert.Contains(t, result, "3 lines")
	// Collapsed should NOT show the content.
	assert.NotContains(t, result, "Let me think")
}

func TestCollapsibleThinking_Expanded(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ct := &CollapsibleThinking{
		ID:        0,
		Content:   "Let me think about this...\nFirst, I need to consider X.\nThen Y.",
		LineCount: 3,
		Collapsed: false,
	}
	result := ct.Render(r)
	assert.Contains(t, result, "▼")
	assert.Contains(t, result, "Thinking")
	assert.Contains(t, result, "Let me think")
}

func TestCollapsibleThinking_FullyExpanded(t *testing.T) {
	r := NewToolBoxRenderer(80)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("reasoning step %d", i+1)
	}
	ct := &CollapsibleThinking{
		ID:            0,
		Content:       strings.Join(lines, "\n"),
		LineCount:     50,
		Collapsed:     false,
		FullyExpanded: true,
	}
	result := ct.Render(r)
	assert.Contains(t, result, "▼")
	assert.Contains(t, result, "reasoning step 50")
	assert.Contains(t, result, "50 lines")
	assert.NotContains(t, result, "20 shown")
}

func TestCollapsibleThinking_EmptyContent(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ct := &CollapsibleThinking{
		ID:        0,
		Content:   "",
		LineCount: 0,
		Collapsed: true,
	}
	result := ct.Render(r)
	assert.Contains(t, result, "empty")
}

func TestCollapsibleThinking_SingleLine(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ct := &CollapsibleThinking{
		ID:        0,
		Content:   "short thought",
		LineCount: 1,
		Collapsed: true,
	}
	result := ct.Render(r)
	assert.Contains(t, result, "1 line")
	assert.NotContains(t, result, "1 lines")
}

func TestCollapsibleThinking_TruncatedLabel(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ct := &CollapsibleThinking{
		ID:        0,
		Content:   strings.Repeat("line\n", 50),
		LineCount: 50,
		Collapsed: true,
	}
	result := ct.Render(r)
	assert.Contains(t, result, "50 lines (20 shown)")
}

func TestCollapsibleError_Collapsed(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ce := &CollapsibleError{
		ID:        0,
		Message:   "connection refused: failed to connect to database server",
		Collapsed: true,
	}
	result := ce.Render(r)
	// Collapsed should show ✗ and a preview.
	assert.Contains(t, result, "✗")
	assert.Contains(t, result, "connection refused")
}

func TestCollapsibleError_Expanded(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ce := &CollapsibleError{
		ID:        0,
		Message:   "connection refused: failed to connect to database server at localhost:5432",
		Collapsed: false,
	}
	result := ce.Render(r)
	// Expanded should show header and full message.
	assert.Contains(t, result, "✗ Error")
	assert.Contains(t, result, "connection refused")
	assert.Contains(t, result, "localhost:5432")
}

func TestCollapsibleError_LongMessagePreview(t *testing.T) {
	r := NewToolBoxRenderer(80)
	longMsg := strings.Repeat("x", 100)
	ce := &CollapsibleError{
		ID:        0,
		Message:   longMsg,
		Collapsed: true,
	}
	result := ce.Render(r)
	// Collapsed preview should be truncated.
	assert.Contains(t, result, "...")
	assert.LessOrEqual(t, len(result), len(longMsg)+50)
}

func TestCollapsibleError_EmptyMessage(t *testing.T) {
	r := NewToolBoxRenderer(80)
	ce := &CollapsibleError{
		ID:        0,
		Message:   "",
		Collapsed: false,
	}
	result := ce.Render(r)
	assert.Contains(t, result, "✗ Error")
}
