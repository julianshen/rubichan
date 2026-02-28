package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "world")
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	md := "```go\nfmt.Println(\"hello\")\n```"
	result, err := r.Render(md)
	require.NoError(t, err)
	assert.Contains(t, result, "Println")
}

func TestRenderMarkdownBoldStripsMarkers(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	// Glamour should strip the ** markdown markers and apply ANSI styling
	assert.NotContains(t, result, "**world**")
	assert.Contains(t, result, "world")
}

func TestRenderMarkdownEmpty(t *testing.T) {
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	result, err := r.Render("")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRenderMarkdownNilRenderer(t *testing.T) {
	// A nil renderer should fall back to returning raw markdown.
	r := &MarkdownRenderer{}
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	assert.Equal(t, "Hello **world**", result)
}
