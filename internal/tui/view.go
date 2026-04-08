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
		case *ConfigOverlay, *WikiOverlay, *UndoOverlay, *HelpOverlay, *AboutOverlay, *InitKnowledgeGraphOverlay:
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
			label := "Generating..."
			if m.thinkingActive {
				label = "Thinking..."
			}
			b.WriteString(fmt.Sprintf("%s %s%s", m.spinner.View(), styleSpinner.Render(label), elapsed))
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

	// Plan panel
	if m.planPanelVisible && m.sessionState != nil {
		if items := m.sessionState.Plan(); len(items) > 0 {
			b.WriteString(renderPlanPanel(items, m.width))
			b.WriteString("\n")
		}
	}

	// Viewport with selection highlighting if active
	viewContent := m.viewport.View()
	if m.selection.Active && !m.selection.IsEmpty() {
		viewContent = m.renderWithSelection(viewContent)
	}
	b.WriteString(viewContent)
	b.WriteString("\n")

	// Status line / approval prompt
	switch m.state {
	case StateStreaming:
		elapsed := ""
		if !m.turnStartTime.IsZero() {
			elapsed = styleTextDim.Render(fmt.Sprintf(" %s", formatElapsed(time.Since(m.turnStartTime))))
		}
		label := "Generating..."
		if m.thinkingActive {
			label = "Thinking..."
		}
		b.WriteString(fmt.Sprintf("%s %s%s", m.spinner.View(), styleSpinner.Render(label), elapsed))
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

// renderWithSelection applies selection highlighting to the viewport view.
// It overlays selectionStyle (reversed colors with blue background) on selected text ranges,
// while preserving the original ANSI styling of the content by reconstructing the line
// character-by-character and applying selection style only to selected portions.
func (m *Model) renderWithSelection(viewStr string) string {
	start, end := m.selection.Normalized()
	sel := MouseSelection{Start: start, End: end}
	lines := strings.Split(viewStr, "\n")

	for i, line := range lines {
		contentLine := m.viewport.YOffset + i
		if !sel.ContainsLine(contentLine) {
			continue
		}

		// Strip ANSI codes to calculate selection positions in plain text
		stripped := stripANSI(line)
		runes := []rune(stripped)
		lineLen := len(runes)

		startCol, endCol := sel.ColRangeForLine(contentLine, lineLen)
		if startCol >= endCol || startCol >= lineLen {
			continue
		}

		// Clamp endCol to line length
		if endCol > lineLen {
			endCol = lineLen
		}

		// Reconstruct the line while preserving ANSI codes.
		// We iterate through the original line, tracking plain-text position,
		// and wrap selected runes in selection style while preserving ANSI escape codes.
		var output strings.Builder
		plainPos := 0 // position in plain text (stripped version)
		inEscape := false

		for _, r := range line {
			// Handle ANSI escape sequences — copy them as-is to output
			if r == '\x1b' {
				inEscape = true
				output.WriteRune(r)
				continue
			}
			if inEscape {
				output.WriteRune(r)
				if r == 'm' {
					inEscape = false
				}
				continue
			}

			// Regular character (not part of escape sequence)
			if plainPos >= startCol && plainPos < endCol {
				// This rune is selected — wrap in selection style
				output.WriteString(selectionStyle.Render(string(r)))
			} else {
				// Unselected rune — copy as-is
				output.WriteRune(r)
			}
			plainPos++
		}

		lines[i] = output.String()
	}

	return strings.Join(lines, "\n")
}
