package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/persona"
)

// StatusBar displays model, token usage, turn count, estimated cost,
// git branch, and turn elapsed time.
type StatusBar struct {
	width        int
	model        string
	inputTokens  int
	maxTokens    int
	turn         int
	maxTurns     int
	cost         float64
	wikiStage    string
	gitBranch    string
	elapsed      time.Duration
	skillSummary string
}

// NewStatusBar creates a new StatusBar with the given terminal width.
func NewStatusBar(width int) *StatusBar {
	return &StatusBar{
		width: width,
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

// SetWikiProgress sets the wiki generation stage for display.
func (s *StatusBar) SetWikiProgress(stage string) { s.wikiStage = stage }

// ClearWikiProgress clears the wiki progress display.
func (s *StatusBar) ClearWikiProgress() { s.wikiStage = "" }

// SetGitBranch sets the git branch name for display.
func (s *StatusBar) SetGitBranch(branch string) { s.gitBranch = branch }

// SetElapsed sets the turn elapsed duration for display.
func (s *StatusBar) SetElapsed(d time.Duration) { s.elapsed = d }

// ClearElapsed resets the elapsed time display.
func (s *StatusBar) ClearElapsed() { s.elapsed = 0 }

// SetSkillSummary sets the active skill summary for display.
func (s *StatusBar) SetSkillSummary(summary string) { s.skillSummary = summary }

// View renders the status bar as a styled string with clear segments.
func (s *StatusBar) View() string {
	sep := styleTextDim.Render(" │ ")

	segments := []string{
		styleStatusLabel.Render(persona.StatusPrefix()),
		styleStatusValue.Render(s.model),
		styleTextDim.Render(fmt.Sprintf("%s/%s", formatTokens(s.inputTokens), formatTokens(s.maxTokens))),
		styleStatusValue.Render(fmt.Sprintf("Turn %d/%d", s.turn, s.maxTurns)),
		styleTextDim.Render(fmt.Sprintf("~$%.2f", s.cost)),
	}
	if s.gitBranch != "" {
		segments = append(segments, styleStatusLabel.Render("⎇ ")+styleStatusValue.Render(s.gitBranch))
	}
	if s.elapsed > 0 {
		segments = append(segments, styleTextDim.Render("⏱ "+formatElapsed(s.elapsed)))
	}
	if s.wikiStage != "" {
		segments = append(segments, styleStatusLabel.Render("Wiki: ")+styleStatusValue.Render(s.wikiStage))
	}
	if s.skillSummary != "" {
		segments = append(segments, styleStatusLabel.Render("Skills: ")+styleStatusValue.Render(s.skillSummary))
	}

	var b strings.Builder
	b.WriteString(" ")
	for i, seg := range segments {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(seg)
	}
	return b.String()
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

// formatElapsed formats a duration for compact display.
func formatElapsed(d time.Duration) string {
	if d >= time.Minute {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
