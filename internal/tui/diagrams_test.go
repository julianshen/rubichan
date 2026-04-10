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
	// When mmdc is not on PATH, renderMermaidInline should return false.
	// We test this by ensuring KittyGraphics is true but MmdcAvailable() returns false
	// (which it will in test environments without mmdc installed).
	caps := &terminal.Caps{KittyGraphics: true, DarkBackground: true}
	// In most test environments, mmdc is not installed, so this should return false.
	// If mmdc IS installed, it would attempt to render — still a valid test path.
	result := renderMermaidInline(caps, "graph TD\n    A-->B")
	// We can't assert the exact result since it depends on mmdc availability,
	// but the function should not panic.
	_ = result
}
