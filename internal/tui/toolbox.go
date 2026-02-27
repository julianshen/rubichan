package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxToolResultLines = 20

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

	display := strings.Join(lines, "\n")
	if truncated > 0 {
		display += fmt.Sprintf("\n[%d more lines]", truncated)
	}

	box := r.normalBox
	if isError {
		box = r.errorBox
	}
	return box.Render(display) + "\n"
}
