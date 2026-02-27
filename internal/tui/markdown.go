package tui

import (
	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps Glamour for rendering markdown to styled terminal output.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
}

// NewMarkdownRenderer creates a MarkdownRenderer with auto-detected style
// and the given word wrap width.
func NewMarkdownRenderer(width int) *MarkdownRenderer {
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	return &MarkdownRenderer{renderer: r}
}

// Render processes markdown text into styled terminal output.
func (m *MarkdownRenderer) Render(md string) (string, error) {
	if md == "" {
		return "", nil
	}
	return m.renderer.Render(md)
}
