package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// BootstrapProgressMsg carries progress updates during knowledge graph bootstrap.
type BootstrapProgressMsg struct {
	Phase   string // e.g., "analysis", "entities", "complete", "error"
	Message string
	Count   int
	Error   string
}

// BootstrapProgressOverlay displays progress during knowledge graph bootstrap.
type BootstrapProgressOverlay struct {
	messages []string
	phase    string
	done     bool
	error    string
	width    int
	height   int
}

// NewBootstrapProgressOverlay creates a new progress overlay.
func NewBootstrapProgressOverlay(width, height int) *BootstrapProgressOverlay {
	return &BootstrapProgressOverlay{
		messages: []string{"🚀 Knowledge Graph Bootstrap Started"},
		width:    width,
		height:   height,
	}
}

// Update handles progress messages during bootstrap.
func (b *BootstrapProgressOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case BootstrapProgressMsg:
		b.phase = msg.Phase
		if msg.Error != "" {
			b.error = msg.Error
			b.messages = append(b.messages, fmt.Sprintf("❌ Error: %s", msg.Error))
			b.done = true
		} else if msg.Phase == "complete" {
			b.messages = append(b.messages, "✅ Bootstrap complete!")
			b.done = true
		} else {
			if msg.Count > 0 {
				b.messages = append(b.messages, fmt.Sprintf("  %s... found %d", msg.Message, msg.Count))
			} else {
				b.messages = append(b.messages, fmt.Sprintf("  %s...", msg.Message))
			}
		}
	case tea.WindowSizeMsg:
		b.width = msg.Width
		b.height = msg.Height
	}
	return b, nil
}

// View renders the progress overlay.
func (b *BootstrapProgressOverlay) View() string {
	var output string

	// Title
	title := styleApprovalBorder.Width(b.width - 4).Render("Knowledge Graph Bootstrap")
	output += title + "\n\n"

	// Progress messages
	for _, msg := range b.messages {
		output += msg + "\n"
	}

	// Spinner or completion indicator
	if !b.done {
		output += "\n⏳ Processing...\n"
	}

	return output
}

// Done returns true when bootstrap is complete or errored.
func (b *BootstrapProgressOverlay) Done() bool {
	return b.done
}

// Result returns nil (progress overlay handles state directly via messages).
func (b *BootstrapProgressOverlay) Result() any {
	return nil
}
