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

// resumeSessionFlow is the shared implementation for session resumption.
// It lists available sessions, constructs a session selector overlay with a callback,
// and captures any errors from the callback (user cancellation or load failures).
func (tui *InteractiveTUI) resumeSessionFlow() error {
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

	// TODO: Integrate overlay into Bubble Tea event loop.
	// Currently, the overlay is constructed with callback but never displayed or
	// routed keyboard events. Full implementation requires wiring into the TUI
	// model's View() and Update() methods.
	_ = overlay

	if callbackErr != nil {
		return callbackErr
	}

	return nil
}

// HandleResumeCommand displays the session selector overlay for the user to choose
// a previous session to resume. Called when the user types /resume during interactive
// conversation. Returns error if no sessions exist or if loading the selected session fails.
func (tui *InteractiveTUI) HandleResumeCommand() error {
	return tui.resumeSessionFlow()
}

// PromptResumeSession is deprecated in favor of HandleResumeCommand.
// Kept for backward compatibility. Uses the same underlying logic.
func (tui *InteractiveTUI) PromptResumeSession(ctx context.Context) error {
	// ctx parameter unused; kept for API compatibility
	return tui.resumeSessionFlow()
}

// restoreTurns re-hydrates conversation history into TUI state
func (tui *InteractiveTUI) restoreTurns(turns []Turn) {
	// Store turns for later access
	tui.turns = turns
}
