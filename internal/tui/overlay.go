package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/julianshen/rubichan/internal/knowledgegraph"
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

// InitKnowledgeGraphResult carries the bootstrap profile to start the process.
type InitKnowledgeGraphResult struct {
	Profile *knowledgegraph.BootstrapProfile
}

// processOverlayResult handles the typed result from a completed overlay.
// Each case manages its own state transition and returns any follow-up command.
func (m *Model) processOverlayResult(result any) tea.Cmd {
	switch r := result.(type) {
	case ApprovalResult:
		if m.pendingApproval == nil {
			m.state = StateInput
			return nil
		}
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
	case InitKnowledgeGraphResult:
		if r.Profile == nil {
			m.state = StateInput
			return nil
		}
		// Start bootstrap in background with progress overlay.
		// Unlike other overlays, bootstrap runs as an async operation because discovery
		// can take seconds. The progress overlay provides feedback via BootstrapProgressMsg.
		// The context can be cancelled via m.bootstrapCancel (e.g., user presses Ctrl+C).
		m.activeOverlay = NewBootstrapProgressOverlay(m.width, m.height)
		m.state = StateBootstrapProgressOverlay
		ctx, cancel := context.WithCancel(context.Background())
		m.bootstrapCancel = cancel
		return func() tea.Msg {
			return m.runBootstrap(ctx, r.Profile)
		}
	case nil:
		// Overlay was cancelled (e.g., Escape pressed).
		// Defensive: unblock agent if approval was somehow cancelled.
		if m.pendingApproval != nil {
			m.pendingApproval.responseValue <- ApprovalNo
			m.pendingApproval = nil
		}
		m.approvalPrompt = nil
		m.state = StateInput
		return nil
	}
	m.state = StateInput
	return nil
}

// executeUndo applies an undo operation from the UndoResult.
func (m *Model) executeUndo(r UndoResult) {
	if m.checkpointMgr == nil {
		m.content.WriteString("[no checkpoint manager available]\n")
		m.setContentAndAutoScroll()
		return
	}
	var restored []string
	var err error
	if r.All && r.Turn > 0 {
		restored, err = m.checkpointMgr.RewindToTurn(context.Background(), r.Turn-1)
	} else {
		var path string
		path, err = m.checkpointMgr.Undo(context.Background())
		if path != "" {
			restored = []string{path}
		}
	}
	if err != nil {
		m.content.WriteString(fmt.Sprintf("[undo failed: %s]\n", err))
	} else if len(restored) > 0 {
		for _, p := range restored {
			m.content.WriteString(fmt.Sprintf("  restored %s\n", p))
		}
	} else {
		m.content.WriteString("[nothing to undo]\n")
	}
	m.setContentAndAutoScroll()
}
