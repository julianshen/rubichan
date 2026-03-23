package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentBufferAppendAndRender(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("before\n")
	buf.AppendToolResult(CollapsibleToolResult{
		Name:      "read_file",
		Args:      `{"path":"a.go"}`,
		Content:   "hello",
		LineCount: 1,
		Collapsed: false,
		ToolType:  ToolTypeFile,
	})
	buf.AppendText("after\n")

	rendered := buf.Render(100)
	assert.Contains(t, rendered, "before")
	assert.Contains(t, rendered, "read_file")
	assert.Contains(t, rendered, "hello")
	assert.Contains(t, rendered, "after")
}

func TestContentBufferDirtySegmentRerender(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("x\n")
	buf.AppendToolResult(CollapsibleToolResult{
		Name:      "shell",
		Args:      `{"command":"echo hi"}`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: true,
		ToolType:  ToolTypeShell,
	})

	_ = buf.Render(90)
	require.Len(t, buf.segments, 2)
	assert.False(t, buf.segments[0].dirty)
	assert.False(t, buf.segments[1].dirty)

	ok := buf.ToggleToolResult(0)
	require.True(t, ok)
	assert.False(t, buf.segments[0].dirty, "text segment should remain clean")
	assert.True(t, buf.segments[1].dirty, "toggled tool-result segment should be dirty")

	_ = buf.Render(90)
	assert.False(t, buf.segments[1].dirty)
}

func TestContentBufferToggleBehavior(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendToolResult(CollapsibleToolResult{Name: "a", Content: "a", LineCount: 1, Collapsed: true})
	buf.AppendToolResult(CollapsibleToolResult{Name: "b", Content: "b", LineCount: 1, Collapsed: true})

	buf.ToggleAllToolResults()
	results := buf.ToolResults()
	assert.False(t, results[0].Collapsed)
	assert.False(t, results[1].Collapsed)

	buf.ToggleAllToolResults()
	results = buf.ToolResults()
	assert.True(t, results[0].Collapsed)
	assert.True(t, results[1].Collapsed)
}

func TestContentBufferOutputStableAcrossWidthChanges(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("prefix\n")
	buf.AppendToolResult(CollapsibleToolResult{
		Name:      "shell",
		Args:      `{"command":"printf a\\nprintf b"}`,
		Content:   "a\nb",
		LineCount: 2,
		Collapsed: false,
		ToolType:  ToolTypeShell,
	})

	w80First := buf.Render(80)
	w120 := buf.Render(120)
	w80Second := buf.Render(80)

	assert.NotEmpty(t, w120)
	assert.Equal(t, w80First, w80Second)
}

func TestContentBufferReplaceTextRangePreservesToolResultSegments(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("assistant old text")
	buf.AppendToolResult(CollapsibleToolResult{
		Name:      "shell",
		Args:      `{"command":"echo hi"}`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: false,
		ToolType:  ToolTypeShell,
	})
	buf.AppendText("\nafter\n")

	// Replace only the assistant text prefix.
	buf.ReplaceTextRange(0, len("assistant old text"), "assistant new text")

	results := buf.ToolResults()
	require.Len(t, results, 1, "tool-result segments should survive text replacement")
	assert.False(t, results[0].Collapsed)
	assert.True(t, buf.ToggleToolResult(results[0].ID), "tool-result should remain interactive")
	assert.True(t, buf.ToolResults()[0].Collapsed)
}

func TestContentBufferReplaceTextRangeInsideToolResultInsertsBeforeTool(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("prefix ")
	buf.AppendToolResult(CollapsibleToolResult{
		Name:      "shell",
		Args:      `{"command":"echo hi"}`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: false,
		ToolType:  ToolTypeShell,
	})
	buf.AppendText(" suffix")

	rendered := buf.Render(80)
	toolStart := len("prefix ")
	toolEnd := len(rendered) - len(" suffix")
	require.Greater(t, toolEnd, toolStart)

	// Start/end are inside tool-result rendered text.
	buf.ReplaceTextRange(toolStart+1, toolStart+2, "[X]")

	updated := buf.Render(80)
	assert.Contains(t, updated, "prefix [X]")
	assert.Contains(t, updated, "shell")
	assert.Contains(t, updated, " suffix")
}

func TestContentBufferReplaceTextRangeWithWidthPreservesToolResultSegments(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("assistant old text")
	buf.AppendToolResult(CollapsibleToolResult{
		Name:      "shell",
		Args:      `{"command":"echo hi"}`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: false,
		ToolType:  ToolTypeShell,
	})
	buf.AppendText("\nafter\n")

	buf.ReplaceTextRangeWithWidth(120, 0, len("assistant old text"), "assistant new text")

	results := buf.ToolResults()
	require.Len(t, results, 1, "tool-result segments should survive width-aware replacement")
	assert.True(t, buf.ToggleToolResult(results[0].ID), "tool-result should remain interactive")
}
