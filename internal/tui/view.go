package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/persona"
)

// headerStyle and inputPromptStyle are aliases for the centralized pink theme styles.
var (
	headerStyle      = styleHeader
	inputPromptStyle = styleInputPrompt
)

// View implements tea.Model. It renders the TUI as a string.
func (m *Model) View() string {
	if m.quitting {
		return persona.GoodbyeMessage()
	}

	// Full-screen overlays take over the entire view.
	if m.activeOverlay != nil {
		switch m.activeOverlay.(type) {
		case *ConfigOverlay, *WikiOverlay, *UndoOverlay:
			return m.activeOverlay.View()
		}
	}

	var b strings.Builder

	if m.plainMode {
		b.WriteString(m.viewport.View())
		b.WriteString("\n")
		switch m.state {
		case StateStreaming:
			elapsed := ""
			if !m.turnStartTime.IsZero() {
				elapsed = styleTextDim.Render(fmt.Sprintf(" %s", formatElapsed(time.Since(m.turnStartTime))))
			}
			b.WriteString(fmt.Sprintf("%s %s%s", m.spinner.View(), styleSpinner.Render(m.thinkingMsg), elapsed))
		case StateAwaitingApproval:
			if m.activeOverlay != nil {
				b.WriteString(m.activeOverlay.View())
			} else if m.approvalPrompt != nil {
				b.WriteString(m.approvalPrompt.View())
			} else {
				b.WriteString(m.statusBar.View())
			}
		default:
			b.WriteString(m.statusBar.View())
		}
		b.WriteString("\n")
		b.WriteString(inputPromptStyle.Render("❯ "))
		b.WriteString(m.input.View())
		return b.String()
	}

	// Header
	header := headerStyle.Render(fmt.Sprintf("%s · %s", m.appName, m.modelName))
	b.WriteString(header)
	b.WriteString("\n")
	if line := m.activeSkillsLine(); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Divider
	dividerWidth := m.width
	if dividerWidth < 1 {
		dividerWidth = 80
	}
	b.WriteString(styleDivider.Render(strings.Repeat("━", dividerWidth)))
	b.WriteString("\n")

	// Viewport
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Status line / approval prompt
	switch m.state {
	case StateStreaming:
		elapsed := ""
		if !m.turnStartTime.IsZero() {
			elapsed = styleTextDim.Render(fmt.Sprintf(" %s", formatElapsed(time.Since(m.turnStartTime))))
		}
		b.WriteString(fmt.Sprintf("%s %s%s", m.spinner.View(), styleSpinner.Render(m.thinkingMsg), elapsed))
	case StateAwaitingApproval:
		if m.activeOverlay != nil {
			b.WriteString(m.activeOverlay.View())
		} else if m.approvalPrompt != nil {
			b.WriteString(m.approvalPrompt.View())
		} else {
			b.WriteString(m.statusBar.View())
		}
	default:
		b.WriteString(m.statusBar.View())
	}
	b.WriteString("\n")

	// Completion overlay (above input)
	if m.completion != nil {
		if cv := m.completion.View(); cv != "" {
			b.WriteString(cv)
			b.WriteString("\n")
		}
	}

	// File completion overlay (above input, mutually exclusive with command completion)
	if m.fileCompletion != nil {
		if fv := m.fileCompletion.View(); fv != "" {
			b.WriteString(fv)
			b.WriteString("\n")
		}
	}

	// Input line
	b.WriteString(inputPromptStyle.Render("❯ "))
	b.WriteString(m.input.View())

	return b.String()
}

func (m *Model) activeSkillsLine() string {
	if len(m.activeSkills) == 0 {
		return ""
	}

	line := "Skills: " + strings.Join(m.activeSkills, ", ")
	if m.width > 0 && len(line) > m.width {
		if m.width <= 3 {
			line = line[:m.width]
		} else {
			line = line[:m.width-3] + "..."
		}
	}
	return styleTextDim.Render(line)
}
