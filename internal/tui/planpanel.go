package tui

import (
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/session"
)

const maxPlanPanelLines = 8

// planItemIcon returns the visual icon for a plan item status.
func planItemIcon(status session.PlanStatus) string {
	switch status {
	case session.PlanStatusPending:
		return "○ " // circle — not started
	case session.PlanStatusInProgress:
		return "⟳ " // refresh — in progress
	case session.PlanStatusCompleted:
		return "✓ " // check — done
	case session.PlanStatusFailed:
		return "✗ " // cross — failed
	case session.PlanStatusReverifyRequired:
		return "⟳ " // refresh — needs re-verification
	default:
		return "? "
	}
}

// renderPlanPanel renders the plan panel from session plan items.
// Returns a formatted, truncated plan display suitable for inline display.
func renderPlanPanel(items []session.PlanItem, width int) string {
	if len(items) == 0 {
		return ""
	}

	// Render each plan item with icon and step description.
	var lines []string
	for _, item := range items {
		icon := planItemIcon(item.Status)
		line := fmt.Sprintf("%s%s", icon, item.Step)
		lines = append(lines, line)
	}

	// Truncate to maxPlanPanelLines if needed.
	truncated := 0
	if len(lines) > maxPlanPanelLines {
		truncated = len(lines) - maxPlanPanelLines
		lines = lines[:maxPlanPanelLines]
	}

	// Join lines.
	content := strings.Join(lines, "\n")
	if truncated > 0 {
		content += fmt.Sprintf("\n[%d more items]", truncated)
	}

	// Render in bordered panel.
	panel := stylePlanPanel.Width(width - 4)
	return panel.Render(content)
}

// planPanelHeight returns the height in lines that the plan panel will occupy,
// clamped at maxPlanPanelLines + 1 (for border).
func planPanelHeight(items []session.PlanItem) int {
	if len(items) == 0 {
		return 0
	}
	lines := len(items)
	if lines > maxPlanPanelLines {
		lines = maxPlanPanelLines
	}
	// +2 for rounded border top/bottom
	return lines + 2
}
