package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// helpBinding describes a single key binding.
type helpBinding struct {
	key         string
	description string
}

// helpCategory groups related key bindings.
type helpCategory struct {
	name     string
	bindings []helpBinding
}

// HelpOverlay displays available key bindings in a scrollable overlay.
type HelpOverlay struct {
	viewport  viewport.Model
	cancelled bool
	width     int
	height    int
}

// NewHelpOverlay creates a new help overlay with the given dimensions.
func NewHelpOverlay(width, height int) *HelpOverlay {
	vp := viewport.New(width-4, height-6)
	vp.SetContent(helpContent())

	return &HelpOverlay{
		viewport:  vp,
		cancelled: false,
		width:     width,
		height:    height,
	}
}

// helpContent generates the static help text with all key bindings.
func helpContent() string {
	categories := []helpCategory{
		{
			name: "Navigation",
			bindings: []helpBinding{
				{"↑/↓ or k/j", "scroll messages"},
				{"Page Up/Down", "jump full page"},
				{"Home/End", "jump to start/end"},
				{"Ctrl+L", "jump to last error"},
			},
		},
		{
			name: "Input",
			bindings: []helpBinding{
				{"Enter", "send prompt"},
				{"Ctrl+A", "jump to start of line"},
				{"Ctrl+E", "jump to end of line"},
				{"Ctrl+U", "clear line"},
				{"Ctrl+W", "delete word"},
			},
		},
		{
			name: "Tool Results",
			bindings: []helpBinding{
				{"o", "toggle tool result"},
				{"Ctrl+O", "toggle all tool results"},
				{"Ctrl+E", "expand most recent"},
			},
		},
		{
			name: "Overlays",
			bindings: []helpBinding{
				{"Ctrl+F", "toggle plan panel"},
				{"Ctrl+K", "open config"},
				{"Ctrl+Z", "open undo"},
				{"Ctrl+W", "open wiki generator"},
				{"?", "show this help"},
				{"Esc or q", "close overlay"},
			},
		},
	}

	var content strings.Builder
	content.WriteString("KEY BINDINGS\n")
	content.WriteString(strings.Repeat("─", 40) + "\n\n")

	for i, cat := range categories {
		content.WriteString(styleHeader.Render(cat.name) + "\n")
		for _, binding := range cat.bindings {
			content.WriteString(fmt.Sprintf("  %-20s %s\n", binding.key, binding.description))
		}
		if i < len(categories)-1 {
			content.WriteString("\n")
		}
	}

	return content.String()
}

// Update handles input for the help overlay.
func (h *HelpOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEscape:
			h.cancelled = true
			return h, nil
		case tea.KeyUp:
			h.viewport.ScrollUp(1)
			return h, nil
		case tea.KeyDown:
			h.viewport.ScrollDown(1)
			return h, nil
		case tea.KeyPgUp:
			h.viewport.HalfPageUp()
			return h, nil
		case tea.KeyPgDown:
			h.viewport.HalfPageDown()
			return h, nil
		case tea.KeyHome:
			h.viewport.GotoTop()
			return h, nil
		case tea.KeyEnd:
			h.viewport.GotoBottom()
			return h, nil
		}

		// Check for character keys q/Q.
		switch msg.String() {
		case "q", "Q":
			h.cancelled = true
			return h, nil
		}

	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
		h.viewport.Width = msg.Width - 4
		h.viewport.Height = msg.Height - 6
	}
	return h, nil
}

// View renders the help overlay.
func (h *HelpOverlay) View() string {
	// Border and title
	border := styleApprovalBorder.Width(h.width - 4)
	content := border.Render(h.viewport.View())

	// Hints at bottom
	hints := styleTextDim.Render("↑↓/jk = scroll · q/Esc = close")
	help := fmt.Sprintf("%s\n%s\n", content, styleKeyHint.Render(hints))

	return help
}

// Done returns true when the overlay should close.
func (h *HelpOverlay) Done() bool {
	return h.cancelled
}

// Result returns nil (help overlay has no result value).
func (h *HelpOverlay) Result() any {
	return nil
}
