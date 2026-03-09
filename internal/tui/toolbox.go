package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxToolResultLines = 20

// Diff colorization styles.
var (
	diffAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")) // green
	diffRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")) // red
	diffHunkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4")) // cyan
)

// ToolBoxRenderer renders tool calls and results in bordered boxes.
type ToolBoxRenderer struct {
	width     int
	normalBox lipgloss.Style
	errorBox  lipgloss.Style
}

// NewToolBoxRenderer creates a renderer with the given terminal width.
func NewToolBoxRenderer(width int) *ToolBoxRenderer {
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	normal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}).
		Width(boxWidth).
		Padding(0, 1)
	errStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FF0000")).
		Width(boxWidth).
		Padding(0, 1)

	return &ToolBoxRenderer{
		width:     width,
		normalBox: normal,
		errorBox:  errStyle,
	}
}

// RenderToolCall renders a tool invocation header box.
func (r *ToolBoxRenderer) RenderToolCall(name, args string) string {
	header := fmt.Sprintf("─ %s(%s) ", name, args)
	return r.normalBox.Render(header) + "\n"
}

// RenderToolResult renders a tool result in a bordered box.
// If isError is true, the border is red.
func (r *ToolBoxRenderer) RenderToolResult(name, content string, isError bool) string {
	lines := strings.Split(content, "\n")
	truncated := 0
	if len(lines) > maxToolResultLines {
		truncated = len(lines) - maxToolResultLines
		lines = lines[:maxToolResultLines]
	}

	display := ColorizeDiffLines(strings.Join(lines, "\n"))
	if truncated > 0 {
		display += fmt.Sprintf("\n[%d more lines]", truncated)
	}

	box := r.normalBox
	if isError {
		box = r.errorBox
	}
	return box.Render(display) + "\n"
}

// RenderToolProgress renders streaming tool progress output.
func (r *ToolBoxRenderer) RenderToolProgress(name, stage, content string, isError bool) string {
	if content == "" {
		return ""
	}
	prefix := fmt.Sprintf("[%s:%s]\n", name, stage)
	box := r.normalBox
	if isError {
		box = r.errorBox
	}
	return box.Render(prefix+content) + "\n"
}

// isDiffContent returns true if the content appears to be a unified diff
// (contains at least one @@ hunk header).
func isDiffContent(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			return true
		}
	}
	return false
}

// ColorizeDiffLines applies green/red/cyan coloring to unified diff lines.
// If the content does not appear to be a diff (no @@ hunk headers), it is
// returned unchanged to avoid false positives.
func ColorizeDiffLines(content string) string {
	if content == "" || !isDiffContent(content) {
		return content
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@ "):
			lines[i] = diffHunkStyle.Render(line)
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			// File headers — leave unstyled (or could style bold)
			continue
		case strings.HasPrefix(line, "+"):
			lines[i] = diffAddedStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = diffRemovedStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
