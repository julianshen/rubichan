package tui

import (
	"testing"

	"github.com/julianshen/rubichan/internal/terminal"
	"github.com/stretchr/testify/assert"
)

func TestRenderMermaidInline_NilCaps(t *testing.T) {
	assert.False(t, renderMermaidInline(nil, "graph TD\n    A-->B"))
}

func TestRenderMermaidInline_NoKittyGraphics(t *testing.T) {
	caps := &terminal.Caps{KittyGraphics: false}
	assert.False(t, renderMermaidInline(caps, "graph TD\n    A-->B"))
}

func TestRenderMermaidInline_KittyGraphicsButNoMmdc(t *testing.T) {
	// Force mmdc to be unavailable by setting PATH to a nonexistent directory.
	t.Setenv("PATH", "/nonexistent")
	caps := &terminal.Caps{KittyGraphics: true, DarkBackground: true}
	assert.False(t, renderMermaidInline(caps, "graph TD\n    A-->B"))
}

// --- Mermaid block detection ---

func TestDetectMermaidBlocks_SingleBlock(t *testing.T) {
	content := "Some text\n```mermaid\ngraph TD\n    A-->B\n```\nMore text"
	blocks := detectMermaidBlocks(content)

	assert.Len(t, blocks, 1)
	assert.Equal(t, "graph TD\n    A-->B", blocks[0].source)
	assert.Contains(t, content[blocks[0].start:blocks[0].end], "```mermaid")
}

func TestDetectMermaidBlocks_MultipleBlocks(t *testing.T) {
	content := "```mermaid\ngraph LR\n    X-->Y\n```\ntext\n```mermaid\nsequenceDiagram\n    A->>B: Hi\n```"
	blocks := detectMermaidBlocks(content)

	assert.Len(t, blocks, 2)
	assert.Equal(t, "graph LR\n    X-->Y", blocks[0].source)
	assert.Equal(t, "sequenceDiagram\n    A->>B: Hi", blocks[1].source)
}

func TestDetectMermaidBlocks_NoBlocks(t *testing.T) {
	content := "Some text\n```go\nfunc main() {}\n```\nMore text"
	blocks := detectMermaidBlocks(content)

	assert.Empty(t, blocks)
}

func TestDetectMermaidBlocks_EmptyContent(t *testing.T) {
	assert.Empty(t, detectMermaidBlocks(""))
}

func TestDetectMermaidBlocks_UnclosedBlock(t *testing.T) {
	content := "```mermaid\ngraph TD\n    A-->B"
	blocks := detectMermaidBlocks(content)

	assert.Empty(t, blocks, "unclosed blocks should not be detected")
}

func TestReplaceMermaidBlocks_NoCapabilities(t *testing.T) {
	content := "Text\n```mermaid\ngraph TD\n    A-->B\n```\nMore"
	caps := &terminal.Caps{KittyGraphics: false}

	result := replaceMermaidBlocks(content, caps)
	assert.Equal(t, content, result, "should pass through unchanged when Kitty unavailable")
}

func TestReplaceMermaidBlocks_NilCaps(t *testing.T) {
	content := "```mermaid\ngraph TD\n    A-->B\n```"

	result := replaceMermaidBlocks(content, nil)
	assert.Equal(t, content, result, "should pass through unchanged when caps is nil")
}

func TestReplaceMermaidBlocks_NoMermaidBlocks(t *testing.T) {
	content := "Just regular text with ```code```"
	caps := &terminal.Caps{KittyGraphics: true}

	result := replaceMermaidBlocks(content, caps)
	assert.Equal(t, content, result, "should pass through unchanged when no mermaid blocks")
}
