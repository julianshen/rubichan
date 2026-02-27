package tui

import (
	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps Glamour for rendering markdown to styled terminal output.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
}

// NewMarkdownRenderer creates a MarkdownRenderer with dark style
// and the given word wrap width. Dark style is used instead of auto-detect
// because the TUI runs inside Bubble Tea which manages the terminal directly.
func NewMarkdownRenderer(width int) *MarkdownRenderer {
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
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
