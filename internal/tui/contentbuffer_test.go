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

func TestContentBuffer_AppendThinking(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("before\n")
	buf.AppendThinking(CollapsibleThinking{
		Content:   "I need to analyze this code...",
		LineCount: 1,
		Collapsed: true,
	})
	buf.AppendText("after\n")

	rendered := buf.Render(100)
	assert.Contains(t, rendered, "before")
	assert.Contains(t, rendered, "Thinking")
	assert.Contains(t, rendered, "after")
	// Collapsed thinking should NOT show the content.
	assert.NotContains(t, rendered, "I need to analyze")
}

func TestContentBuffer_AppendThinking_Expanded(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendThinking(CollapsibleThinking{
		Content:   "Deep reasoning here",
		LineCount: 1,
		Collapsed: false,
	})

	rendered := buf.Render(100)
	assert.Contains(t, rendered, "Deep reasoning here")
}

func TestContentBuffer_ThinkingIDsDoNotCollideWithToolResults(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendToolResult(CollapsibleToolResult{Name: "shell", Content: "ok", LineCount: 1})
	buf.AppendThinking(CollapsibleThinking{Content: "thinking", LineCount: 1, Collapsed: true})
	buf.AppendToolResult(CollapsibleToolResult{Name: "file_read", Content: "data", LineCount: 1})

	// IDs should be 0, 1, 2 — no collisions.
	require.Len(t, buf.segments, 3)
	assert.Equal(t, 0, buf.segments[0].ToolResult.ID)
	assert.Equal(t, 1, buf.segments[1].Thinking.ID)
	assert.Equal(t, 2, buf.segments[2].ToolResult.ID)
}

func TestToggleAllCollapsible_IncludesThinking(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendToolResult(CollapsibleToolResult{Name: "shell", Content: "ok", LineCount: 1, Collapsed: true})
	buf.AppendThinking(CollapsibleThinking{Content: "thinking", LineCount: 1, Collapsed: true})

	// Both are collapsed; toggling should expand both.
	buf.ToggleAllToolResults()
	assert.False(t, buf.segments[0].ToolResult.Collapsed)
	assert.False(t, buf.segments[1].Thinking.Collapsed)

	// Now both are expanded; toggling should collapse both.
	buf.ToggleAllToolResults()
	assert.True(t, buf.segments[0].ToolResult.Collapsed)
	assert.True(t, buf.segments[1].Thinking.Collapsed)
}

func TestCollapseAllToolResults_IncludesThinking(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendToolResult(CollapsibleToolResult{Name: "shell", Content: "ok", LineCount: 1, Collapsed: false})
	buf.AppendThinking(CollapsibleThinking{Content: "thinking", LineCount: 1, Collapsed: false})

	buf.CollapseAllToolResults()
	assert.True(t, buf.segments[0].ToolResult.Collapsed)
	assert.True(t, buf.segments[1].Thinking.Collapsed)
}

func TestHasCollapsible_EmptyBuffer(t *testing.T) {
	buf := NewContentBuffer()
	assert.False(t, buf.HasCollapsible())
}

func TestHasCollapsible_WithThinkingOnly(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("text\n")
	buf.AppendThinking(CollapsibleThinking{Content: "thought", LineCount: 1, Collapsed: true})
	assert.True(t, buf.HasCollapsible())
}

func TestContentBuffer_AppendError(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("before\n")
	buf.AppendError(CollapsibleError{Message: "something went wrong", Collapsed: false})
	buf.AppendText("after\n")

	rendered := buf.Render(100)
	assert.Contains(t, rendered, "before")
	assert.Contains(t, rendered, "something went wrong")
	assert.Contains(t, rendered, "after")
}

func TestContentBuffer_ErrorCount(t *testing.T) {
	buf := NewContentBuffer()
	assert.Equal(t, 0, buf.ErrorCount())

	buf.AppendError(CollapsibleError{Message: "error 1", Collapsed: false})
	assert.Equal(t, 1, buf.ErrorCount())

	buf.AppendError(CollapsibleError{Message: "error 2", Collapsed: false})
	assert.Equal(t, 2, buf.ErrorCount())
}

func TestContentBuffer_LastErrorIndex(t *testing.T) {
	buf := NewContentBuffer()
	assert.Equal(t, -1, buf.LastErrorIndex())

	buf.AppendText("text")
	assert.Equal(t, -1, buf.LastErrorIndex())

	buf.AppendError(CollapsibleError{Message: "first error", Collapsed: false})
	assert.Equal(t, 1, buf.LastErrorIndex())

	buf.AppendText("more text")
	assert.Equal(t, 1, buf.LastErrorIndex())

	buf.AppendError(CollapsibleError{Message: "second error", Collapsed: false})
	assert.Equal(t, 3, buf.LastErrorIndex())
}

func TestContentBuffer_ErrorIDsDoNotCollideWithToolResults(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendToolResult(CollapsibleToolResult{Name: "shell", Content: "ok", LineCount: 1})
	buf.AppendError(CollapsibleError{Message: "error", Collapsed: false})
	buf.AppendToolResult(CollapsibleToolResult{Name: "file_read", Content: "data", LineCount: 1})

	// IDs should be 0, 1, 2 — no collisions.
	require.Len(t, buf.segments, 3)
	assert.Equal(t, 0, buf.segments[0].ToolResult.ID)
	assert.Equal(t, 1, buf.segments[1].Error.ID)
	assert.Equal(t, 2, buf.segments[2].ToolResult.ID)
}
