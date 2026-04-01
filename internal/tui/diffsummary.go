package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var diffSummaryCountPattern = regexp.MustCompile(`(\d+)\s+file(?:\(s\)|s)?\s+changed`)

func (m *Model) viewportContent() string {
	content := m.content.Render(m.width)

	// Append diff summary panel if available.
	panel := m.renderDiffSummaryPanel()
	if panel != "" {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += panel
	}

	// Append agent detail panel if toggled.
	agentPanel := m.renderAgentPanel()
	if agentPanel != "" {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += agentPanel
	}

	return content
}

// renderAgentPanel renders the expandable agent detail panel when visible.
func (m *Model) renderAgentPanel() string {
	if !m.agentPanelVisible || len(m.runningAgents) == 0 {
		return ""
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("▼ %s %s",
		styleToolResultHeader.Render("Running Agents"),
		styleKeyHint.Render("[ctrl+a]"),
	))
	for _, a := range m.runningAgents {
		body.WriteString(fmt.Sprintf("\n  ⊕ %s [%s]", a.Name, a.ID))
	}

	return styleDiffPanel.Render(body.String()) + "\n"
}

func (m *Model) renderDiffSummaryPanel() string {
	if strings.TrimSpace(m.diffSummary) == "" {
		return ""
	}

	label := diffSummaryLabel(m.diffSummary)
	indicator := "▶"
	if m.diffExpanded {
		indicator = "▼"
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("%s %s (%s) %s",
		indicator,
		styleToolResultHeader.Render("Turn changes"),
		styleSectionLabel.Render(label),
		styleKeyHint.Render("[ctrl+g]"),
	))
	if m.diffExpanded {
		rendered := m.diffSummary
		if m.mdRenderer != nil {
			if formatted, err := m.mdRenderer.Render(m.diffSummary); err == nil && formatted != "" {
				rendered = formatted
			}
		}
		body.WriteString("\n\n")
		body.WriteString(strings.TrimRight(rendered, "\n"))
	}

	return styleDiffPanel.Render(body.String()) + "\n"
}

func diffSummaryLabel(summary string) string {
	matches := diffSummaryCountPattern.FindStringSubmatch(summary)
	if len(matches) == 2 {
		count, err := strconv.Atoi(matches[1])
		if err == nil {
			if count == 1 {
				return "1 file changed"
			}
			return fmt.Sprintf("%d files changed", count)
		}
	}

	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line != "" {
			return line
		}
	}

	return "changes available"
}
