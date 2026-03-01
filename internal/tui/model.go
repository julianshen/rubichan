package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
)

// approvalRequest carries a tool approval request from the agent goroutine
// to the TUI. The agent blocks on the response channel until the user decides.
type approvalRequest struct {
	tool     string
	input    string
	response chan bool
}

// approvalRequestMsg is the Bubble Tea message type for approval requests.
type approvalRequestMsg approvalRequest

// UIState represents the current state of the TUI.
type UIState int

const (
	// StateInput indicates the TUI is waiting for user input.
	StateInput UIState = iota
	// StateStreaming indicates the TUI is streaming a response from the agent.
	StateStreaming
	// StateAwaitingApproval indicates the TUI is waiting for user approval of a tool call.
	StateAwaitingApproval
	// StateConfigOverlay indicates the TUI is showing the config overlay.
	StateConfigOverlay
	// StateBootstrap indicates the TUI is running the bootstrap setup wizard.
	StateBootstrap
)

// Model is the Bubble Tea model for the Rubichan TUI.
type Model struct {
	agent             *agent.Agent
	cfg               *config.Config
	configPath        string
	configForm        *ConfigForm
	input             *InputArea
	viewport          viewport.Model
	spinner           spinner.Model
	content           strings.Builder
	rawAssistant      strings.Builder
	mdRenderer        *MarkdownRenderer
	toolBox           *ToolBoxRenderer
	statusBar         *StatusBar
	approvalPrompt    *ApprovalPrompt
	pendingApproval   *approvalRequest
	alwaysApproved    sync.Map
	approvalCh        chan approvalRequest
	assistantStartIdx int
	state             UIState
	appName           string
	modelName         string
	width             int
	height            int
	turnCount         int
	maxTurns          int
	totalCost         float64
	quitting          bool
	eventCh           <-chan agent.TurnEvent
}

// Ensure Model satisfies the tea.Model interface at compile time.
var _ tea.Model = (*Model)(nil)

// NewModel creates a new TUI Model with the given agent, application name,
// model name, maximum turns, config path, and config. The agent and config
// may be nil for testing purposes.
func NewModel(a *agent.Agent, appName, modelName string, maxTurns int, configPath string, cfg *config.Config) *Model {
	ia := NewInputArea()

	vp := viewport.New(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	sb := NewStatusBar(80)
	sb.SetModel(modelName)
	sb.SetTurn(0, maxTurns)

	// Glamour renderer creation is unlikely to fail with static "dark" style,
	// but handle it gracefully — Render falls back to raw text if renderer is nil.
	mdRenderer, _ := NewMarkdownRenderer(80)

	m := &Model{
		agent:      a,
		cfg:        cfg,
		configPath: configPath,
		input:      ia,
		viewport:   vp,
		spinner:    sp,
		mdRenderer: mdRenderer,
		toolBox:    NewToolBoxRenderer(80),
		statusBar:  sb,
		approvalCh: make(chan approvalRequest),
		state:      StateInput,
		appName:    appName,
		modelName:  modelName,
		maxTurns:   maxTurns,
		width:      80,
		height:     24,
	}

	bannerText := RenderBanner()
	m.content.WriteString(bannerText)
	m.content.WriteString("\n")
	m.viewport.SetContent(m.content.String())

	return m
}

// SetAgent sets the agent on the model. This is used when the model needs to
// be created before the agent (e.g., to extract the approval function).
func (m *Model) SetAgent(a *agent.Agent) {
	m.agent = a
}

// MakeApprovalFunc returns an agent.ApprovalFunc that bridges the agent's
// synchronous approval requests to the TUI's async keypress handling.
// Tools previously marked "always" are auto-approved without prompting.
func (m *Model) MakeApprovalFunc() agent.ApprovalFunc {
	return func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		// Auto-approve if user previously chose "always" for this tool.
		if _, ok := m.alwaysApproved.Load(tool); ok {
			return true, nil
		}

		respCh := make(chan bool, 1)
		m.approvalCh <- approvalRequest{
			tool:     tool,
			input:    string(input),
			response: respCh,
		}
		return <-respCh, nil
	}
}

// IsAutoApproved checks if a tool was previously marked "always approve"
// by the user. This implements agent.AutoApproveChecker to enable parallel
// execution of auto-approved tools.
func (m *Model) IsAutoApproved(tool string) bool {
	_, ok := m.alwaysApproved.Load(tool)
	return ok
}

// waitForApproval returns a tea.Cmd that blocks until an approval request
// arrives on the approval channel, then delivers it as an approvalRequestMsg.
func (m *Model) waitForApproval() tea.Cmd {
	ch := m.approvalCh
	return func() tea.Msg {
		req := <-ch
		return approvalRequestMsg(req)
	}
}

// handleCommand processes slash commands entered by the user.
// Returns a tea.Cmd if the command produces one (e.g., tea.Quit), or nil.
func (m *Model) handleCommand(cmd string) tea.Cmd {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "/quit", "/exit":
		m.quitting = true
		return tea.Quit

	case "/clear":
		if m.agent != nil {
			m.agent.ClearConversation()
		}
		m.content.Reset()
		m.viewport.SetContent("")
		return nil

	case "/model":
		if len(parts) < 2 {
			m.content.WriteString("Usage: /model <name>\n")
			m.viewport.SetContent(m.content.String())
			return nil
		}
		newModel := parts[1]
		if m.agent != nil {
			m.agent.SetModel(newModel)
		}
		m.modelName = newModel
		m.statusBar.SetModel(newModel)
		m.content.WriteString(fmt.Sprintf("Model switched to %s\n", newModel))
		m.viewport.SetContent(m.content.String())
		return nil

	case "/config":
		if m.cfg == nil {
			m.content.WriteString("No config available\n")
			m.viewport.SetContent(m.content.String())
			return nil
		}
		m.configForm = NewConfigForm(m.cfg, m.configPath)
		m.state = StateConfigOverlay
		return m.configForm.Form().Init()

	case "/help":
		help := "Available commands:\n" +
			"  /help          Show this help message\n" +
			"  /clear         Clear conversation history\n" +
			"  /config        Edit configuration\n" +
			"  /model <name>  Switch to a different model\n" +
			"  /quit          Exit the application\n"
		m.content.WriteString(help)
		m.viewport.SetContent(m.content.String())
		return nil

	default:
		m.content.WriteString(fmt.Sprintf("Unknown command: %s\n", parts[0]))
		m.viewport.SetContent(m.content.String())
		return nil
	}
}
