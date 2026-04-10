package interactive

// InteractiveTUI represents the interactive terminal user interface for the agent.
type InteractiveTUI struct {
	sessionMgr *SessionManager
	acpClient  *ACPClient
}

// NewInteractiveTUI creates a new interactive TUI with an optional session manager and ACP client.
// sessionMgr may be nil if session resumption is not enabled.
// acpClient may be nil if not yet initialized.
func NewInteractiveTUI(sessionMgr *SessionManager, acpClient *ACPClient) *InteractiveTUI {
	return &InteractiveTUI{
		sessionMgr: sessionMgr,
		acpClient:  acpClient,
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
