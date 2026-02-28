package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ApprovalResult represents the user's choice on a tool approval prompt.
type ApprovalResult int

const (
	ApprovalPending ApprovalResult = iota
	ApprovalYes
	ApprovalNo
	ApprovalAlways
)

// ApprovalPrompt shows an inline approval prompt for a tool call.
type ApprovalPrompt struct {
	tool   string
	args   string
	result ApprovalResult
	done   bool
	box    lipgloss.Style
}

// NewApprovalPrompt creates a new approval prompt for the given tool and args.
// The width parameter controls the box width.
func NewApprovalPrompt(tool, args string, width int) *ApprovalPrompt {
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#CC8800", Dark: "#FFAA00"}).
		Width(boxWidth).
		Padding(0, 1)

	return &ApprovalPrompt{
		tool: tool,
		args: args,
		box:  box,
	}
}

// Done returns true if the user has made a decision.
func (a *ApprovalPrompt) Done() bool { return a.done }

// Result returns the user's approval decision.
func (a *ApprovalPrompt) Result() ApprovalResult { return a.result }

// SetResult sets the approval result and marks the prompt as done.
func (a *ApprovalPrompt) SetResult(r ApprovalResult) {
	a.result = r
	a.done = true
}

// HandleKey processes a single keypress for the approval prompt.
// Returns true if the key was handled (approval decision made).
func (a *ApprovalPrompt) HandleKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "y", "Y":
		a.SetResult(ApprovalYes)
		return true
	case "n", "N":
		a.SetResult(ApprovalNo)
		return true
	case "a", "A":
		a.SetResult(ApprovalAlways)
		return true
	}
	return false
}

// View renders the approval prompt as a bordered box with tool info and options.
func (a *ApprovalPrompt) View() string {
	header := fmt.Sprintf("─ %s(%s) ", a.tool, a.args)
	prompt := "Allow?  (y)es  (n)o  (a)lways"
	return a.box.Render(header+"\n"+prompt) + "\n"
}
