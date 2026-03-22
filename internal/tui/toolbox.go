package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxToolResultLines = 20

// Diff colorization styles use the centralized pink theme.
var (
	diffAddedStyle   = styleDiffAdded
	diffRemovedStyle = styleDiffRemoved
	diffHunkStyle    = styleDiffHunk
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
	normal := styleToolBoxBorder.Width(boxWidth)
	errStyle := styleToolBoxErrorBorder.Width(boxWidth)

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

// CollapsibleToolResult tracks a single tool result with collapse state.
type CollapsibleToolResult struct {
	ID        int
	Name      string
	Args      string
	Content   string
	LineCount int
	IsError   bool
	Collapsed bool
	ToolType  ToolType // tool category for visual differentiation
	ExitCode  *int     // shell exit code (nil for non-shell tools)
}

// Render returns the rendered view of a tool result, either collapsed
// (single summary line) or expanded (bordered box with content).
func (c *CollapsibleToolResult) Render(r *ToolBoxRenderer) string {
	lineLabel := c.lineLabel()
	icon := c.ToolType.Icon()
	if c.Collapsed {
		return styleToolResultHeader.Render(fmt.Sprintf("▶ %s%s(%s)", icon, c.Name, c.Args)) +
			styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	}
	header := styleToolResultHeader.Render(fmt.Sprintf("▼ %s%s(%s)", icon, c.Name, c.Args)) +
		styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	return header + r.RenderToolResult(c.Name, c.Content, c.IsError)
}

// lineLabel returns a human-friendly line count label.
// When content exceeds maxToolResultLines, it shows "N lines (20 shown)".
// For shell tools, appends "[exit N]" when ExitCode is set.
func (c *CollapsibleToolResult) lineLabel() string {
	label := ""
	if c.LineCount == 0 {
		label = "empty"
	} else if c.LineCount > maxToolResultLines {
		label = fmt.Sprintf("%d lines (%d shown)", c.LineCount, maxToolResultLines)
	} else if c.LineCount == 1 {
		label = "1 line"
	} else {
		label = fmt.Sprintf("%d lines", c.LineCount)
	}
	if c.ExitCode != nil {
		label += fmt.Sprintf(" [exit %d]", *c.ExitCode)
	}
	return label
}

// toolResultPlaceholder returns a placeholder marker for the given tool result ID.
// These are embedded in the content buffer and replaced with rendered output
// in viewportContent().
func toolResultPlaceholder(id int) string {
	return fmt.Sprintf("\x00TR:%d\x00", id)
}

// replaceToolResultPlaceholders replaces all tool result placeholder markers
// in content with their rendered (collapsed or expanded) representation.
func replaceToolResultPlaceholders(content string, results []CollapsibleToolResult, r *ToolBoxRenderer) string {
	for i := range results {
		placeholder := toolResultPlaceholder(results[i].ID)
		content = strings.Replace(content, placeholder, results[i].Render(r), 1)
	}
	return content
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
