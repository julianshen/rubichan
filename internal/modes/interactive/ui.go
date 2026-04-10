package interactive

import (
	"context"
	"fmt"
)

// InteractiveTUI represents the interactive terminal user interface for the agent.
type InteractiveTUI struct {
	sessionMgr *SessionManager
	acpClient  *ACPClient
	turns      []Turn // restored conversation history
}

// NewInteractiveTUI creates a new interactive TUI with an optional session manager and ACP client.
// sessionMgr may be nil if session resumption is not enabled.
// acpClient may be nil if not yet initialized.
func NewInteractiveTUI(sessionMgr *SessionManager, acpClient *ACPClient) *InteractiveTUI {
	return &InteractiveTUI{
		sessionMgr: sessionMgr,
		acpClient:  acpClient,
		turns:      []Turn{},
	}
}

// SessionManager returns the underlying session manager (may be nil).
func (t *InteractiveTUI) SessionManager() *SessionManager {
	return t.sessionMgr
}

// SetSessionManager updates the session manager (for testing or late initialization).
func (t *InteractiveTUI) SetSessionManager(mgr *SessionManager) {
	t.sessionMgr = mgr
}

// ACPClient returns the underlying ACP client (may be nil).
func (t *InteractiveTUI) ACPClient() *ACPClient {
	return t.acpClient
}

// SetACPClient updates the ACP client (for testing or late initialization).
func (t *InteractiveTUI) SetACPClient(client *ACPClient) {
	t.acpClient = client
}

// ShouldPromptResume returns true if there are existing sessions to resume
func (tui *InteractiveTUI) ShouldPromptResume() (bool, error) {
	if tui.sessionMgr == nil {
		return false, nil
	}

	sessions, err := tui.sessionMgr.List()
	if err != nil {
		return false, err
	}

	return len(sessions) > 0, nil
}

// PromptResumeSession shows session selector overlay and loads selected session
func (tui *InteractiveTUI) PromptResumeSession(ctx context.Context) error {
	if tui.sessionMgr == nil {
		return fmt.Errorf("session manager not available")
	}

	sessions, err := tui.sessionMgr.List()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	if len(sessions) == 0 {
		return fmt.Errorf("no sessions to resume")
	}

	// Callback handles selection or cancellation
	var callbackErr error

	callback := func(selected SessionMetadata, err error) {
		if err != nil {
			// User cancelled, nothing to do
			callbackErr = err
			return
		}

		// Load selected session
		turns, err := tui.sessionMgr.Load(selected.ID)
		if err != nil {
			callbackErr = fmt.Errorf("load session: %w", err)
			return
		}

		// Restore turns into TUI state
		tui.restoreTurns(turns)
	}

	// Create and show overlay
	overlay := NewSessionSelectorOverlay(sessions, callback)

	// In a real implementation, this would integrate with the Bubble Tea model
	// For now, we just set up the infrastructure
	_ = overlay // Use overlay to avoid lint error

	if callbackErr != nil {
		return callbackErr
	}

	return nil
}

// restoreTurns re-hydrates conversation history into TUI state
func (tui *InteractiveTUI) restoreTurns(turns []Turn) {
	// Store turns for later access
	tui.turns = turns
}
