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

	if m.state == StateConfigOverlay && m.configForm != nil {
		return m.configForm.Form().View()
	}

	if m.state == StateWikiOverlay && m.wikiForm != nil {
		return m.wikiForm.Form().View()
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
		b.WriteString(fmt.Sprintf("%s %s%s", m.spinner.View(), styleSpinner.Render(persona.ThinkingMessage()), elapsed))
	case StateAwaitingApproval:
		if m.approvalPrompt != nil {
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
