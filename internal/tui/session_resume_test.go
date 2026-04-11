package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/store"
)

func testSessions() []store.Session {
	return []store.Session{
		{ID: "sess-aaa", Title: "Fix auth bug", UpdatedAt: time.Now()},
		{ID: "sess-bbb", Title: "Add tests", UpdatedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "sess-ccc", Title: "", UpdatedAt: time.Now().Add(-48 * time.Hour)},
	}
}

func TestSessionResumeOverlayImplementsOverlay(t *testing.T) {
	var _ Overlay = NewSessionResumeOverlay(nil)
}

func TestSessionResumeOverlayViewShowsSessions(t *testing.T) {
	overlay := NewSessionResumeOverlay(testSessions())
	view := overlay.View()

	assert.Contains(t, view, "Fix auth bug")
	assert.Contains(t, view, "Add tests")
	assert.Contains(t, view, "sess-ccc") // fallback to ID prefix when no title
	assert.Contains(t, view, "Resume Session")
}

func TestSessionResumeOverlaySelectSession(t *testing.T) {
	overlay := NewSessionResumeOverlay(testSessions())

	// Move down to second session
	overlay.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, overlay.index)

	// Press Enter
	overlay.Update(tea.KeyMsg{Type: tea.KeyEnter})

	require.True(t, overlay.Done())
	result := overlay.Result()
	require.NotNil(t, result)

	resume, ok := result.(SessionResumeResult)
	require.True(t, ok)
	assert.Equal(t, "sess-bbb", resume.SessionID)
}

func TestSessionResumeOverlayEscape(t *testing.T) {
	overlay := NewSessionResumeOverlay(testSessions())

	overlay.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.True(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}

func TestSessionResumeOverlayVimKeys(t *testing.T) {
	overlay := NewSessionResumeOverlay(testSessions())

	// j moves down
	overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 1, overlay.index)

	// k moves up
	overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.Equal(t, 0, overlay.index)

	// q cancels
	overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	assert.True(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}

func TestSessionResumeOverlayBoundaries(t *testing.T) {
	overlay := NewSessionResumeOverlay(testSessions())

	// Can't go above 0
	overlay.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, overlay.index)

	// Go to last
	overlay.Update(tea.KeyMsg{Type: tea.KeyDown})
	overlay.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, overlay.index)

	// Can't go past last
	overlay.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, overlay.index)
}

func TestSessionResumeOverlayEmptySessions(t *testing.T) {
	overlay := NewSessionResumeOverlay(nil)
	view := overlay.View()

	assert.Contains(t, view, "No previous sessions")

	// Enter on empty list just closes
	overlay.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}

func TestSessionResumeOverlayEnterSelectsFirst(t *testing.T) {
	overlay := NewSessionResumeOverlay(testSessions())

	overlay.Update(tea.KeyMsg{Type: tea.KeyEnter})

	require.True(t, overlay.Done())
	result, ok := overlay.Result().(SessionResumeResult)
	require.True(t, ok)
	assert.Equal(t, "sess-aaa", result.SessionID)
}

func TestSessionTimeAgo(t *testing.T) {
	assert.Equal(t, "just now", sessionTimeAgo(time.Now()))
	assert.Equal(t, "5m ago", sessionTimeAgo(time.Now().Add(-5*time.Minute)))
	assert.Equal(t, "2h ago", sessionTimeAgo(time.Now().Add(-2*time.Hour)))
	assert.Contains(t, sessionTimeAgo(time.Now().Add(-48*time.Hour)), ",")
}

// --- Model integration tests ---

func TestProcessOverlayResultSessionResume(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateResumeOverlay

	cmd := m.processOverlayResult(SessionResumeResult{SessionID: "sess-123"})

	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.Contains(t, m.content.String(), "sess-123")
}

func TestProcessOverlayResultSessionResumeCancelled(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateResumeOverlay

	cmd := m.processOverlayResult(nil)

	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
}

func TestResumeCommandSetsOverlay(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	// Create an in-memory store with a session
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(store.Session{
		ID:    "sess-abc",
		Title: "Test session",
	}))
	m.SetSessionStore(s)

	// Register the resume command
	require.NoError(t, reg.Register(commands.NewResumeCommand()))

	cmd := m.handleCommand("/resume")
	assert.Nil(t, cmd) // no init cmd needed
	assert.Equal(t, StateResumeOverlay, m.state)
	assert.NotNil(t, m.activeOverlay)
}

func TestResumeCommandNoStore(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	require.NoError(t, reg.Register(commands.NewResumeCommand()))

	cmd := m.handleCommand("/resume")
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.Contains(t, m.content.String(), "not available")
}
