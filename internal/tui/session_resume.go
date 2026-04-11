package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/julianshen/rubichan/internal/store"
)

// maxResumeSessionCount is the maximum number of sessions shown in the resume overlay.
const maxResumeSessionCount = 20

// SessionResumeResult carries the selected session ID from the overlay.
type SessionResumeResult struct {
	SessionID string
}

// SessionResumeOverlay implements Overlay for the session resume selector.
type SessionResumeOverlay struct {
	sessions []store.Session
	index    int
	done     bool
	result   *SessionResumeResult // nil when cancelled
}

// NewSessionResumeOverlay creates a session resume overlay from a list of sessions.
func NewSessionResumeOverlay(sessions []store.Session) *SessionResumeOverlay {
	cp := make([]store.Session, len(sessions))
	copy(cp, sessions)
	return &SessionResumeOverlay{
		sessions: cp,
	}
}

func (o *SessionResumeOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}

	switch keyMsg.Type {
	case tea.KeyUp:
		if o.index > 0 {
			o.index--
		}
	case tea.KeyDown:
		if o.index < len(o.sessions)-1 {
			o.index++
		}
	case tea.KeyEnter:
		if o.index >= 0 && o.index < len(o.sessions) {
			o.result = &SessionResumeResult{SessionID: o.sessions[o.index].ID}
		}
		o.done = true
	case tea.KeyEsc:
		o.done = true
	default:
		switch keyMsg.String() {
		case "k":
			if o.index > 0 {
				o.index--
			}
		case "j":
			if o.index < len(o.sessions)-1 {
				o.index++
			}
		case "q":
			o.done = true
		}
	}
	return o, nil
}

func (o *SessionResumeOverlay) View() string {
	if len(o.sessions) == 0 {
		return "No previous sessions found.\nPress Esc to close.\n"
	}

	var b strings.Builder
	b.WriteString("Resume Session\n\n")

	for i, s := range o.sessions {
		marker := "  "
		if i == o.index {
			marker = "> "
		}

		title := s.Title
		if title == "" {
			if len(s.ID) >= 8 {
				title = s.ID[:8]
			} else {
				title = s.ID
			}
		}

		b.WriteString(fmt.Sprintf("%s%s  %s\n", marker, title, sessionTimeAgo(s.UpdatedAt)))
	}

	b.WriteString("\nUse arrows to navigate, Enter to resume, Esc to cancel\n")
	return b.String()
}

func (o *SessionResumeOverlay) Done() bool {
	return o.done
}

func (o *SessionResumeOverlay) Result() any {
	if o.result != nil {
		return *o.result
	}
	return nil
}

func sessionTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("Jan 2, 15:04")
}
