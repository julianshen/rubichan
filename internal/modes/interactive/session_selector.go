package interactive

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// SessionSelector manages session selection state for overlay UI
type SessionSelector struct {
	sessions []SessionMetadata
	index    int
}

// NewSessionSelector creates a selector from a list of sessions
func NewSessionSelector(sessions []SessionMetadata) *SessionSelector {
	return &SessionSelector{
		sessions: sessions,
		index:    0,
	}
}

// SelectedIndex returns the currently selected index
func (ss *SessionSelector) SelectedIndex() int {
	return ss.index
}

// Selected returns the currently selected session
func (ss *SessionSelector) Selected() SessionMetadata {
	if ss.index < 0 || ss.index >= len(ss.sessions) {
		return SessionMetadata{}
	}
	return ss.sessions[ss.index]
}

// Sessions returns all sessions
func (ss *SessionSelector) Sessions() []SessionMetadata {
	return ss.sessions
}

// MoveUp moves selection up (previous session)
func (ss *SessionSelector) MoveUp() {
	if ss.index > 0 {
		ss.index--
	}
}

// MoveDown moves selection down (next session)
func (ss *SessionSelector) MoveDown() {
	if ss.index < len(ss.sessions)-1 {
		ss.index++
	}
}

// Reset selects the first session
func (ss *SessionSelector) Reset() {
	ss.index = 0
}

// SessionSelectorOverlay implements tea.Model for session selection UI
type SessionSelectorOverlay struct {
	selector *SessionSelector
	callback func(SessionMetadata, error)
	width    int
	height   int
}

// NewSessionSelectorOverlay creates a session selector overlay
func NewSessionSelectorOverlay(sessions []SessionMetadata, callback func(SessionMetadata, error)) *SessionSelectorOverlay {
	return &SessionSelectorOverlay{
		selector: NewSessionSelector(sessions),
		callback: callback,
		width:    80,
		height:   24,
	}
}

// Init implements tea.Model
func (o *SessionSelectorOverlay) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (o *SessionSelectorOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			o.selector.MoveUp()
		case tea.KeyDown:
			o.selector.MoveDown()
		case tea.KeyEnter:
			if o.callback != nil {
				o.callback(o.selector.Selected(), nil)
			}
			return o, tea.Quit
		case tea.KeyEsc:
			if o.callback != nil {
				o.callback(SessionMetadata{}, fmt.Errorf("cancelled"))
			}
			return o, tea.Quit
		default:
			// Check for letter keys (k/j for vim, q for quit)
			switch msg.String() {
			case "k":
				o.selector.MoveUp()
			case "j":
				o.selector.MoveDown()
			case "q":
				if o.callback != nil {
					o.callback(SessionMetadata{}, fmt.Errorf("cancelled"))
				}
				return o, tea.Quit
			}
		}
	}
	return o, nil
}

// View implements tea.Model
func (o *SessionSelectorOverlay) View() string {
	var b strings.Builder
	b.WriteString("\n📋 Resume Session\n\n")

	sessions := o.selector.Sessions()
	if len(sessions) == 0 {
		b.WriteString("No previous sessions found.\n")
		return b.String()
	}

	selected := o.selector.SelectedIndex()
	for i, s := range sessions {
		marker := "  "
		if i == selected {
			marker = "→ "
		}

		relTime := timeAgo(s.CreatedAt)
		b.WriteString(fmt.Sprintf("%s[%s] %s (%d turns)\n", marker, s.ID, relTime, s.TurnCount))
	}

	b.WriteString("\nUse ↑↓ to navigate, Enter to resume, Esc to cancel\n")
	return b.String()
}

// timeAgo formats duration since a time
func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return t.Format("Jan 2, 15:04")
}
