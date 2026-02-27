package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar displays model, token usage, turn count, and estimated cost.
type StatusBar struct {
	width       int
	model       string
	inputTokens int
	maxTokens   int
	turn        int
	maxTurns    int
	cost        float64
	style       lipgloss.Style
}

// NewStatusBar creates a new StatusBar with the given terminal width.
func NewStatusBar(width int) *StatusBar {
	return &StatusBar{
		width: width,
		style: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}),
	}
}

// SetModel sets the displayed model name.
func (s *StatusBar) SetModel(name string) { s.model = name }

// SetTokens sets the input token count and max token budget.
func (s *StatusBar) SetTokens(used, max int) { s.inputTokens = used; s.maxTokens = max }

// SetTurn sets the current and maximum turn count.
func (s *StatusBar) SetTurn(current, max int) { s.turn = current; s.maxTurns = max }

// SetCost sets the cumulative estimated cost.
func (s *StatusBar) SetCost(cost float64) { s.cost = cost }

// View renders the status bar as a styled string.
func (s *StatusBar) View() string {
	return s.style.Render(fmt.Sprintf(" %s  %s/%s  Turn %d/%d  ~$%.2f",
		s.model,
		formatTokens(s.inputTokens),
		formatTokens(s.maxTokens),
		s.turn, s.maxTurns,
		s.cost,
	))
}

// formatTokens formats a token count for compact display.
func formatTokens(n int) string {
	if n >= 1000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
