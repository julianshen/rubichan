package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Style definitions for the TUI view.
var (
	headerStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#EEEEEE"})
	inputPromptStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"})
)

// View implements tea.Model. It renders the TUI as a string.
func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var b strings.Builder

	// Header
	header := headerStyle.Render(fmt.Sprintf("%s · %s", m.appName, m.modelName))
	b.WriteString(header)
	b.WriteString("\n")

	// Divider
	dividerWidth := m.width
	if dividerWidth < 1 {
		dividerWidth = 80
	}
	b.WriteString(strings.Repeat("─", dividerWidth))
	b.WriteString("\n")

	// Viewport
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Status line
	switch m.state {
	case StateStreaming:
		b.WriteString(fmt.Sprintf("%s Thinking...", m.spinner.View()))
	case StateAwaitingApproval:
		b.WriteString("Approve? [Y/N]")
	default:
		// No status line content in input state
	}
	b.WriteString("\n")

	// Input line
	b.WriteString(inputPromptStyle.Render("> "))
	b.WriteString(m.input.View())

	return b.String()
}
