package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown(t *testing.T) {
	r := NewMarkdownRenderer(80)
	result, err := r.Render("Hello **world**")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "world")
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	r := NewMarkdownRenderer(80)
	md := "```go\nfmt.Println(\"hello\")\n```"
	result, err := r.Render(md)
	require.NoError(t, err)
	assert.Contains(t, result, "Println")
}

func TestRenderMarkdownEmpty(t *testing.T) {
	r := NewMarkdownRenderer(80)
	result, err := r.Render("")
	require.NoError(t, err)
	assert.Empty(t, result)
}
