package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxToolResultLines = 20

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

// RenderToolCall renders a tool invocation header box with formatted arguments.
func (r *ToolBoxRenderer) RenderToolCall(name, args string) string {
	formatted := formatToolArgs(name, args)
	header := fmt.Sprintf("─ %s %s ", ClassifyTool(name).Icon()+name, formatted)
	return r.normalBox.Render(header) + "\n"
}

// renderInBox renders content in a bordered box, choosing error style when isError is true.
func (r *ToolBoxRenderer) renderInBox(content string, isError bool) string {
	box := r.normalBox
	if isError {
		box = r.errorBox
	}
	return box.Render(content) + "\n"
}

// RenderToolResult renders a tool result in a bordered box, truncating to maxToolResultLines.
// The toolName is used to aid content-type detection for syntax highlighting.
func (r *ToolBoxRenderer) RenderToolResult(content string, isError bool, toolName string) string {
	lines := strings.Split(content, "\n")
	truncated := 0
	if len(lines) > maxToolResultLines {
		truncated = len(lines) - maxToolResultLines
		lines = lines[:maxToolResultLines]
	}

	display := ColorizeContent(strings.Join(lines, "\n"), toolName)
	if truncated > 0 {
		display += fmt.Sprintf("\n[%d more lines — Ctrl+E to expand]", truncated)
	}
	return r.renderInBox(display, isError)
}

// RenderToolResultFull renders a tool result without truncation.
func (r *ToolBoxRenderer) RenderToolResultFull(content string, isError bool, toolName string) string {
	return r.renderInBox(ColorizeContent(content, toolName), isError)
}

// RenderToolProgress renders streaming tool progress output.
func (r *ToolBoxRenderer) RenderToolProgress(name, stage, content string, isError bool) string {
	if content == "" {
		return ""
	}
	prefix := fmt.Sprintf("[%s:%s]\n", name, stage)
	return r.renderInBox(prefix+content, isError)
}

// CollapsibleToolResult tracks a single tool result with collapse state.
type CollapsibleToolResult struct {
	ID            int
	Name          string
	Args          string
	Content       string
	LineCount     int
	IsError       bool
	Collapsed     bool
	FullyExpanded bool     // show all content (no truncation); only meaningful when Collapsed == false
	ToolType      ToolType // tool category for visual differentiation
}

// Render returns the rendered view of a tool result in one of three states:
//   - collapsed: single summary line with ▶ indicator
//   - expanded-truncated: ▼ header + first maxToolResultLines lines + expand hint
//   - expanded-full: ▼ header + all lines (when FullyExpanded == true)
func (c *CollapsibleToolResult) Render(r *ToolBoxRenderer) string {
	lineLabel := c.lineLabel()
	icon := c.ToolType.Icon()
	formatted := formatToolArgs(c.Name, c.Args)
	if c.Collapsed {
		return styleToolResultHeader.Render(fmt.Sprintf("▶ %s%s %s", icon, c.Name, formatted)) +
			styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	}
	header := styleToolResultHeader.Render(fmt.Sprintf("▼ %s%s %s", icon, c.Name, formatted)) +
		styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	if c.FullyExpanded {
		return header + r.RenderToolResultFull(c.Content, c.IsError, c.Name)
	}
	return header + r.RenderToolResult(c.Content, c.IsError, c.Name)
}

// formatLineLabel returns a human-friendly line count string.
func formatLineLabel(lineCount int, fullyExpanded bool) string {
	switch {
	case lineCount == 0:
		return "empty"
	case lineCount > maxToolResultLines && !fullyExpanded:
		return fmt.Sprintf("%d lines (%d shown)", lineCount, maxToolResultLines)
	case lineCount == 1:
		return "1 line"
	default:
		return fmt.Sprintf("%d lines", lineCount)
	}
}

func (c *CollapsibleToolResult) lineLabel() string {
	label := formatLineLabel(c.LineCount, c.FullyExpanded)
	if c.IsError {
		label += " ✗"
	} else {
		label += " ✓"
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

// toggleFullExpandMostRecent toggles FullyExpanded on the most recent
// non-collapsed tool result that has truncatable content (LineCount > maxToolResultLines).
// Iterates from end to find the right target.
func toggleFullExpandMostRecent(results []CollapsibleToolResult) {
	for i := len(results) - 1; i >= 0; i-- {
		if !results[i].Collapsed && results[i].LineCount > maxToolResultLines {
			results[i].FullyExpanded = !results[i].FullyExpanded
			return
		}
	}
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

// CollapsibleThinking tracks a thinking/reasoning block with collapse state.
type CollapsibleThinking struct {
	ID            int
	Content       string
	LineCount     int
	Collapsed     bool
	FullyExpanded bool
}

// Render returns the rendered view of a thinking block in one of three states:
//   - collapsed: single summary line with ▶ indicator
//   - expanded-truncated: ▼ header + first maxToolResultLines lines
//   - expanded-full: ▼ header + all lines (when FullyExpanded == true)
func (ct *CollapsibleThinking) Render(r *ToolBoxRenderer) string {
	lineLabel := ct.lineLabel()
	if ct.Collapsed {
		return styleToolResultHeader.Render("▶ 💭 Thinking") +
			styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	}
	header := styleToolResultHeader.Render("▼ 💭 Thinking") +
		styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	if ct.FullyExpanded {
		return header + r.renderInBox(ct.Content, false)
	}
	return header + r.RenderToolResult(ct.Content, false, "")
}

func (ct *CollapsibleThinking) lineLabel() string {
	return formatLineLabel(ct.LineCount, ct.FullyExpanded)
}

// CollapsibleError tracks an error message with collapse state.
type CollapsibleError struct {
	ID        int
	Message   string
	Collapsed bool
}

// Render returns the rendered view of an error in one of two states:
//   - collapsed: single summary line with ✗ indicator
//   - expanded: ✗ header + full message in error-styled box
func (ce *CollapsibleError) Render(r *ToolBoxRenderer) string {
	if ce.Collapsed {
		preview := ce.Message
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		return styleErrorIcon.Render("✗ ") + preview + "\n"
	}
	header := styleErrorIcon.Render("✗ Error") + "\n"
	return header + r.renderInBox(ce.Message, true)
}

// ColorizeDiffLines applies green/red/cyan coloring to unified diff lines.
// Delegates to ColorizeContent for the actual colorization.
func ColorizeDiffLines(content string) string {
	if content == "" || !isDiffContent(content) {
		return content
	}
	return colorizeDiffContent(content)
}
