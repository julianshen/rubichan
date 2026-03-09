package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer wraps Glamour for rendering markdown to styled terminal output.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
}

var assistantProtocolMarkers = []struct {
	token string
	keep  bool
}{
	{token: "assistantanalysis", keep: false},
	{token: "assistantcommentary", keep: false},
	{token: "assistantfinal", keep: true},
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

// IsMarkdownBreakpoint returns true when text ends at a natural markdown
// boundary suitable for incremental rendering: double newline (paragraph),
// closing code fence, or heading line.
func IsMarkdownBreakpoint(text string) bool {
	if len(text) == 0 {
		return false
	}

	// Double newline — paragraph boundary.
	if strings.HasSuffix(text, "\n\n") {
		return true
	}

	// Find the last complete line (text ending with \n).
	if text[len(text)-1] != '\n' {
		return false
	}
	// Trim trailing newline, find previous newline to get last line.
	trimmed := text[:len(text)-1]
	lastNL := strings.LastIndex(trimmed, "\n")
	var lastLine string
	if lastNL == -1 {
		lastLine = trimmed
	} else {
		lastLine = trimmed[lastNL+1:]
	}

	// Closing code fence: line is exactly ``` (possibly with whitespace).
	stripped := strings.TrimSpace(lastLine)
	if stripped == "```" {
		return true
	}

	// Heading: line starts with # (one or more).
	if len(stripped) > 0 && stripped[0] == '#' {
		// Ensure it's a valid heading (# followed by space or end of line).
		hashes := strings.TrimLeft(stripped, "#")
		if len(hashes) == 0 || hashes[0] == ' ' {
			return true
		}
	}

	return false
}

// SanitizeAssistantOutput strips leaked protocol markers from assistant text
// so the TUI only shows the user-facing portion of the response.
func SanitizeAssistantOutput(text string) string {
	if text == "" {
		return ""
	}

	keep := true
	for _, marker := range []struct {
		token string
		keep  bool
	}{
		{token: "assistantanalysis", keep: false},
		{token: "assistantcommentary", keep: false},
		{token: "assistantfinal", keep: true},
		{token: "analysis", keep: false},
		{token: "commentary", keep: false},
		{token: "final", keep: true},
	} {
		if strings.HasPrefix(text, marker.token) {
			text = text[len(marker.token):]
			keep = marker.keep
			break
		}
	}

	var cleaned strings.Builder
	pos := 0

	for pos < len(text) {
		idx, keepNext, found := nextAssistantProtocolMarker(text, pos)
		if !found {
			if keep {
				cleaned.WriteString(text[pos:])
			}
			break
		}

		if keep {
			chunk := text[pos:idx]
			chunk = strings.TrimSuffix(chunk, "command.")
			cleaned.WriteString(chunk)
		}

		keep = keepNext
		pos = idx
		for _, marker := range assistantProtocolMarkers {
			if strings.HasPrefix(text[pos:], marker.token) {
				pos += len(marker.token)
				break
			}
		}
	}

	result := cleaned.String()
	result = strings.TrimSpace(result)
	result = strings.ReplaceAll(result, "\r\n", "\n")
	return result
}

func nextAssistantProtocolMarker(text string, start int) (idx int, keep bool, found bool) {
	bestIdx := -1
	bestKeep := false
	for _, marker := range assistantProtocolMarkers {
		i := strings.Index(text[start:], marker.token)
		if i == -1 {
			continue
		}
		i += start
		if bestIdx == -1 || i < bestIdx {
			bestIdx = i
			bestKeep = marker.keep
		}
	}
	if bestIdx == -1 {
		return 0, false, false
	}
	return bestIdx, bestKeep, true
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
