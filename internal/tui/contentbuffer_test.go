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
