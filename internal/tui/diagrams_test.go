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
