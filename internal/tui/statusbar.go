package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/julianshen/rubichan/internal/persona"
)

// AgentStatus represents a running agent's identity for status bar display.
type AgentStatus struct {
	ID   string
	Name string
}

// segment priority tiers (lower = higher priority = shown first)
const (
	priorityAlways = iota // model name, turn count
	priorityHigh          // tokens, cost
	priorityMedium        // git branch, elapsed
	priorityLow           // wiki, subagent, skills
)

type statusSegment struct {
	content  string
	priority int
}

// StatusBar displays model, token usage, turn count, estimated cost,
// git branch, and turn elapsed time.
type StatusBar struct {
	mu            sync.Mutex
	width         int
	model         string
	inputTokens   int
	maxTokens     int
	turn          int
	maxTurns      int
	cost          float64
	errorCount    int
	wikiStage     string
	gitBranch     string
	elapsed       time.Duration
	skillSummary  string
	subagentName  string
	runningAgents []AgentStatus
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

// IncrementErrorCount increments the error count.
func (s *StatusBar) IncrementErrorCount() { s.errorCount++ }

// ClearErrorCount resets the error count to zero.
func (s *StatusBar) ClearErrorCount() { s.errorCount = 0 }

// ErrorCount returns the current error count.
func (s *StatusBar) ErrorCount() int { return s.errorCount }

// SetSubagent sets the currently running subagent name for display.
// Pass empty string to clear.
func (s *StatusBar) SetSubagent(name string) { s.subagentName = name }

// SetRunningAgents updates the list of currently running agents for display.
func (s *StatusBar) SetRunningAgents(agents []AgentStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runningAgents = agents
}

// RunningAgents returns the current list of running agents.
func (s *StatusBar) RunningAgents() []AgentStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AgentStatus(nil), s.runningAgents...)
}

// View renders the status bar as a styled string with clear segments.
// Lower-priority segments are elided first when the bar does not fit the terminal width.
func (s *StatusBar) View() string {
	sep := styleTextDim.Render(" │ ")

	segments := []statusSegment{
		{styleStatusLabel.Render(persona.StatusPrefix()), priorityAlways},
		{styleStatusValue.Render(s.model), priorityAlways},
		{styleTextDim.Render(fmt.Sprintf("%s/%s", formatTokens(s.inputTokens), formatTokens(s.maxTokens))), priorityHigh},
		{styleStatusValue.Render(fmt.Sprintf("Turn %d/%d", s.turn, s.maxTurns)), priorityAlways},
		{styleTextDim.Render(fmt.Sprintf("~$%.2f", s.cost)), priorityHigh},
	}

	// Add error badge if there are errors.
	if s.errorCount > 0 {
		badge := styleErrorBadge.Render(fmt.Sprintf("⚠ %d", s.errorCount))
		segments = append(segments, statusSegment{badge, priorityAlways})
	}
	if s.gitBranch != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("⎇ ") + styleStatusValue.Render(s.gitBranch), priorityMedium,
		})
	}
	if s.elapsed > 0 {
		segments = append(segments, statusSegment{
			styleTextDim.Render("⏱ " + formatElapsed(s.elapsed)), priorityMedium,
		})
	}
	if s.wikiStage != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("Wiki: ") + styleStatusValue.Render(s.wikiStage), priorityLow,
		})
	}
	if s.subagentName != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("🔄 ") + styleStatusValue.Render(s.subagentName), priorityLow,
		})
	}
	s.mu.Lock()
	agents := s.runningAgents
	s.mu.Unlock()
	if len(agents) > 0 {
		var label string
		if len(agents) == 1 {
			label = fmt.Sprintf("⊕ 1 agent: %s", agents[0].Name)
		} else {
			label = fmt.Sprintf("⊕ %d agents", len(agents))
		}
		segments = append(segments, statusSegment{
			styleStatusLabel.Render(label), priorityMedium,
		})
	}
	if s.skillSummary != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("Skills: ") + styleStatusValue.Render(s.skillSummary), priorityLow,
		})
	}

	visible := s.fitSegments(segments, sep)

	var b strings.Builder
	b.WriteString(" ")
	for i, seg := range visible {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(seg.content)
	}
	return b.String()
}

// fitSegments returns segments that fit within the status bar width,
// dropping lowest-priority segments first.
func (s *StatusBar) fitSegments(segments []statusSegment, sep string) []statusSegment {
	if s.width <= 0 {
		return segments
	}

	visible := make([]statusSegment, len(segments))
	copy(visible, segments)

	for {
		if s.segmentsWidth(visible, sep) <= s.width {
			return visible
		}
		worstIdx := -1
		worstPri := -1
		for i := len(visible) - 1; i >= 0; i-- {
			if visible[i].priority > worstPri {
				worstPri = visible[i].priority
				worstIdx = i
			}
		}
		if worstIdx < 0 || worstPri == priorityAlways {
			break
		}
		visible = append(visible[:worstIdx], visible[worstIdx+1:]...)
	}
	return visible
}

// segmentsWidth calculates the total rendered width of segments with separators.
func (s *StatusBar) segmentsWidth(segments []statusSegment, sep string) int {
	total := 1 // leading space
	sepW := lipgloss.Width(sep)
	for i, seg := range segments {
		if i > 0 {
			total += sepW
		}
		total += lipgloss.Width(seg.content)
	}
	return total
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
