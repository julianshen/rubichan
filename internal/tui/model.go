package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/julianshen/rubichan/internal/agent"
)

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
	input             *InputArea
	viewport          viewport.Model
	spinner           spinner.Model
	content           strings.Builder
	rawAssistant      strings.Builder
	mdRenderer        *MarkdownRenderer
	toolBox           *ToolBoxRenderer
	statusBar         *StatusBar
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
// model name, and maximum turns. The agent may be nil for testing purposes.
func NewModel(a *agent.Agent, appName, modelName string, maxTurns int) *Model {
	ia := NewInputArea()

	vp := viewport.New(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	sb := NewStatusBar(80)
	sb.SetModel(modelName)
	sb.SetTurn(0, maxTurns)

	return &Model{
		agent:      a,
		input:      ia,
		viewport:   vp,
		spinner:    sp,
		mdRenderer: NewMarkdownRenderer(80),
		toolBox:    NewToolBoxRenderer(80),
		statusBar:  sb,
		state:      StateInput,
		appName:    appName,
		modelName:  modelName,
		maxTurns:   maxTurns,
		width:      80,
		height:     24,
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

	case "/help":
		help := "Available commands:\n" +
			"  /help          Show this help message\n" +
			"  /clear         Clear conversation history\n" +
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
