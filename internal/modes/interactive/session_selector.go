package interactive

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
