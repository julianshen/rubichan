package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var diffSummaryCountPattern = regexp.MustCompile(`(\d+)\s+file(?:\(s\)|s)?\s+changed`)
var diffPanelStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1)

func (m *Model) viewportContent() string {
	content := m.content.String()
	panel := m.renderDiffSummaryPanel()
	if panel == "" {
		return content
	}
	if content == "" {
		return panel
	}
	if strings.HasSuffix(content, "\n") {
		return content + panel
	}
	return content + "\n" + panel
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
	body.WriteString(fmt.Sprintf("%s Turn changes (%s) [ctrl+g]", indicator, label))
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

	return diffPanelStyle.Render(body.String()) + "\n"
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
