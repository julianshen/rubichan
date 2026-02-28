package tui

import (
	"fmt"

	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps Glamour for rendering markdown to styled terminal output.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
}

// NewMarkdownRenderer creates a MarkdownRenderer with dark style
// and the given word wrap width. Dark style is used instead of auto-detect
// because the TUI runs inside Bubble Tea which manages the terminal directly.
// Returns an error if the Glamour renderer cannot be created.
func NewMarkdownRenderer(width int) (*MarkdownRenderer, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, fmt.Errorf("creating glamour renderer: %w", err)
	}
	return &MarkdownRenderer{renderer: r}, nil
}

// Render processes markdown text into styled terminal output.
func (m *MarkdownRenderer) Render(md string) (string, error) {
	if md == "" {
		return "", nil
	}
	if m.renderer == nil {
		return md, nil
	}
	return m.renderer.Render(md)
}
