// internal/output/styled_markdown.go
package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
)

// StyledMarkdownFormatter composes MarkdownFormatter with Glamour to produce
// ANSI-styled terminal output. It uses glamour.WithAutoStyle() to detect
// the terminal's light/dark background preference, falling back to dark
// style when auto-detection yields no ANSI styling (e.g., in non-TTY
// environments).
type StyledMarkdownFormatter struct {
	inner    *MarkdownFormatter
	renderer *glamour.TermRenderer
}

// NewStyledMarkdownFormatter creates a StyledMarkdownFormatter with auto-detected
// terminal style and the given word wrap width. Falls back to dark style when
// auto-detection yields no ANSI styling.
func NewStyledMarkdownFormatter(width int) *StyledMarkdownFormatter {
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)

	// Verify auto-style produces ANSI output; fall back to dark if not.
	if r != nil {
		probe, err := r.Render("**test**")
		if err != nil || !strings.Contains(probe, "\x1b[") {
			r, _ = glamour.NewTermRenderer(
				glamour.WithStylePath("dark"),
				glamour.WithWordWrap(width),
			)
		}
	} else {
		r, _ = glamour.NewTermRenderer(
			glamour.WithStylePath("dark"),
			glamour.WithWordWrap(width),
		)
	}

	return &StyledMarkdownFormatter{
		inner:    NewMarkdownFormatter(),
		renderer: r,
	}
}

// Format generates raw markdown via the inner MarkdownFormatter, then renders
// it through Glamour for ANSI styling. Falls back to raw markdown if Glamour
// rendering fails.
func (f *StyledMarkdownFormatter) Format(result *RunResult) ([]byte, error) {
	raw, err := f.inner.Format(result)
	if err != nil {
		return nil, fmt.Errorf("styled markdown: %w", err)
	}

	if f.renderer == nil {
		return raw, nil
	}

	styled, err := f.renderer.Render(string(raw))
	if err != nil {
		return raw, nil
	}

	return []byte(styled), nil
}
