package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Overlay represents a modal UI surface that temporarily takes over
// keyboard input from the main Model. When Done() returns true, the
// Model processes Result() and clears the overlay.
type Overlay interface {
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	View() string
	Done() bool
	Result() any
}

// ConfigResult carries saved config form values.
type ConfigResult struct{}

// WikiResult carries wiki generation parameters.
type WikiResult struct {
	Form *WikiForm
}

// UndoResult carries the user's undo selection.
type UndoResult struct {
	Turn int  // turn number of selected checkpoint
	All  bool // true = rewind all changes from that turn
}

// processOverlayResult handles the typed result from a completed overlay.
// Each case manages its own state transition and returns any follow-up command.
func (m *Model) processOverlayResult(result any) tea.Cmd {
	switch r := result.(type) {
	case ApprovalResult:
		if r == ApprovalAlways {
			m.alwaysDenied.Delete(m.pendingApproval.tool)
			m.alwaysApproved.Store(m.pendingApproval.tool, true)
		}
		if r == ApprovalDenyAlways {
			m.alwaysApproved.Delete(m.pendingApproval.tool)
			m.alwaysDenied.Store(m.pendingApproval.tool, true)
		}
		m.pendingApproval.responseValue <- r
		m.approvalPrompt = nil
		m.pendingApproval = nil
		m.state = StateStreaming
		return m.waitForEvent()
	case ConfigResult:
		m.configForm = nil
		m.state = StateInput
		return nil
	case WikiResult:
		m.wikiForm = nil
		m.state = StateInput
		return m.startWikiGeneration(r.Form)
	case UndoResult:
		m.state = StateInput
		m.executeUndo(r)
		return nil
	case nil:
		// Overlay was cancelled (e.g., Escape pressed).
		m.configForm = nil
		m.wikiForm = nil
		m.approvalPrompt = nil
		m.state = StateInput
		return nil
	}
	m.state = StateInput
	return nil
}

// executeUndo applies an undo operation from the UndoResult.
// TODO: Task 6 will implement this.
func (m *Model) executeUndo(r UndoResult) {
	_ = r // suppress unused warning until Task 6
	m.content.WriteString("[undo not yet implemented]\n")
	m.setContentAndAutoScroll()
}
