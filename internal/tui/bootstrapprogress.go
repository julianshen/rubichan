package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/julianshen/rubichan/internal/terminal"
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
	caps     *terminal.Caps
}

// NewBootstrapProgressOverlay creates a new progress overlay.
func NewBootstrapProgressOverlay(width, height int, caps *terminal.Caps) *BootstrapProgressOverlay {
	return &BootstrapProgressOverlay{
		messages: []string{"🚀 Knowledge Graph Bootstrap Started"},
		width:    width,
		height:   height,
		caps:     caps,
	}
}

// Update handles progress messages during bootstrap.
// Progress bar escape sequences (OSC 9;4) are written directly to stderr because
// they target the terminal's titlebar/tab, which exists outside Bubble Tea's
// alternate screen and does not interfere with TUI rendering.
func (b *BootstrapProgressOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case BootstrapProgressMsg:
		b.phase = msg.Phase
		if msg.Error != "" {
			b.error = msg.Error
			b.messages = append(b.messages, fmt.Sprintf("❌ Error: %s", msg.Error))
			b.done = true
			if b.caps != nil && b.caps.ProgressBar {
				terminal.ClearProgress(os.Stderr)
			}
		} else if msg.Phase == "complete" {
			b.messages = append(b.messages, "✅ Bootstrap complete!")
			b.done = true
			if b.caps != nil && b.caps.ProgressBar {
				terminal.ClearProgress(os.Stderr)
			}
		} else {
			if msg.Count > 0 {
				b.messages = append(b.messages, fmt.Sprintf("  %s... found %d", msg.Message, msg.Count))
			} else {
				b.messages = append(b.messages, fmt.Sprintf("  %s...", msg.Message))
			}
			if b.caps != nil && b.caps.ProgressBar {
				percent := len(b.messages) * 15
				if percent > 95 {
					percent = 95
				}
				terminal.SetProgress(os.Stderr, terminal.ProgressNormal, percent)
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

	// Progress messages (constrain to terminal width)
	contentStyle := lipgloss.NewStyle().Width(b.width - 4)
	for _, msg := range b.messages {
		// Wrap long messages to avoid exceeding terminal width
		wrapped := contentStyle.Render(msg)
		output += wrapped + "\n"
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
