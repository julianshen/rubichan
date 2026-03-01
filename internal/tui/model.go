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
	"github.com/julianshen/rubichan/internal/commands"
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
	cmdRegistry       *commands.Registry
	completion        *CompletionOverlay
}

// Ensure Model satisfies the tea.Model interface at compile time.
var _ tea.Model = (*Model)(nil)

// NewModel creates a new TUI Model with the given agent, application name,
// model name, maximum turns, config path, config, and command registry.
// The agent, config, and registry may be nil for testing purposes.
// A nil registry is replaced with an empty default.
func NewModel(a *agent.Agent, appName, modelName string, maxTurns int, configPath string, cfg *config.Config, cmdRegistry *commands.Registry) *Model {
	defaultReg := cmdRegistry == nil
	if defaultReg {
		cmdRegistry = commands.NewRegistry()
	}
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
		agent:       a,
		cfg:         cfg,
		configPath:  configPath,
		input:       ia,
		viewport:    vp,
		spinner:     sp,
		mdRenderer:  mdRenderer,
		toolBox:     NewToolBoxRenderer(80),
		statusBar:   sb,
		approvalCh:  make(chan approvalRequest),
		state:       StateInput,
		appName:     appName,
		modelName:   modelName,
		maxTurns:    maxTurns,
		width:       80,
		height:      24,
		cmdRegistry: cmdRegistry,
		completion:  NewCompletionOverlay(cmdRegistry, 80),
	}

	// When no registry was provided, populate with default built-in commands.
	// Commands that need model callbacks (clear, model) reference the model
	// instance via closures.
	if defaultReg {
		_ = cmdRegistry.Register(commands.NewQuitCommand())
		_ = cmdRegistry.Register(commands.NewExitCommand())
		_ = cmdRegistry.Register(commands.NewConfigCommand())
		_ = cmdRegistry.Register(commands.NewClearCommand(func() {
			if m.agent != nil {
				m.agent.ClearConversation()
			}
			m.content.Reset()
			m.viewport.SetContent("")
		}))
		_ = cmdRegistry.Register(commands.NewModelCommand(func(name string) {
			if m.agent != nil {
				m.agent.SetModel(name)
			}
			m.modelName = name
			m.statusBar.SetModel(name)
		}))
		_ = cmdRegistry.Register(commands.NewHelpCommand(cmdRegistry))
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

// GetAgent returns the model's agent. This is used by command callbacks that
// need to interact with the agent (e.g., clear conversation, switch model).
func (m *Model) GetAgent() *agent.Agent {
	return m.agent
}

// ClearContent resets the content buffer and viewport. This is used by the
// /clear command callback to wipe the display.
func (m *Model) ClearContent() {
	m.content.Reset()
	m.viewport.SetContent("")
}

// SwitchModel updates the model name and status bar. This is used by the
// /model command callback to reflect the switch in the TUI.
func (m *Model) SwitchModel(name string) {
	m.modelName = name
	m.statusBar.SetModel(name)
}

// syncCompletion updates the completion overlay based on the current input.
func (m *Model) syncCompletion() {
	if m.completion != nil {
		m.completion.Update(m.input.Value())
	}
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

// CheckApproval implements agent.ApprovalChecker by checking the session
// cache of "always approved" tools. Returns AutoApproved for cached tools,
// ApprovalRequired otherwise. Trust rules are composed at a higher level
// via CompositeApprovalChecker.
func (m *Model) CheckApproval(tool string, _ json.RawMessage) agent.ApprovalResult {
	if _, ok := m.alwaysApproved.Load(tool); ok {
		return agent.AutoApproved
	}
	return agent.ApprovalRequired
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
// It delegates to the command registry and interprets the result's Action.
// Returns a tea.Cmd if the command produces one (e.g., tea.Quit), or nil.
func (m *Model) handleCommand(cmd string) tea.Cmd {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	slashCmd, ok := m.cmdRegistry.Get(name)
	if !ok {
		m.content.WriteString(fmt.Sprintf("Unknown command: %s\n", parts[0]))
		m.viewport.SetContent(m.content.String())
		return nil
	}

	result, err := slashCmd.Execute(context.Background(), parts[1:])
	if err != nil {
		m.content.WriteString(fmt.Sprintf("Error: %s\n", err.Error()))
		m.viewport.SetContent(m.content.String())
		return nil
	}

	if result.Output != "" {
		m.content.WriteString(result.Output + "\n")
		m.viewport.SetContent(m.content.String())
	}

	switch result.Action {
	case commands.ActionQuit:
		m.quitting = true
		return tea.Quit
	case commands.ActionOpenConfig:
		if m.cfg == nil {
			m.content.WriteString("No config available\n")
			m.viewport.SetContent(m.content.String())
			return nil
		}
		m.configForm = NewConfigForm(m.cfg, m.configPath)
		m.state = StateConfigOverlay
		return m.configForm.Form().Init()
	}

	return nil
}
