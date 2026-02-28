package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/julianshen/rubichan/internal/agent"
)

// TurnEventMsg wraps an agent.TurnEvent as a Bubble Tea message so streaming
// events from the agent can be dispatched through the Update loop.
type TurnEventMsg agent.TurnEvent

// turnStartedMsg carries the event channel and first event back to Update
// so that m.eventCh is set in the Update goroutine rather than the Cmd goroutine.
type turnStartedMsg struct {
	ch    <-chan agent.TurnEvent
	first agent.TurnEvent
}

// Init implements tea.Model. It initializes the input area and starts
// listening for approval requests from the agent.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.input.Init(), m.waitForApproval())
}

// Update implements tea.Model. It processes incoming messages and returns the
// updated model and any commands to execute.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Route messages to config form overlay when active.
	if m.state == StateConfigOverlay && m.configForm != nil {
		form, cmd := m.configForm.Form().Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.configForm.SetForm(f)
		}
		switch m.configForm.Form().State {
		case huh.StateCompleted:
			_ = m.configForm.Save()
			m.state = StateInput
			m.configForm = nil
		case huh.StateAborted:
			m.state = StateInput
			m.configForm = nil
		}
		return m, cmd
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for header (1), divider (1), status (1), input (1)
		viewportHeight := m.height - 4
		if viewportHeight < 1 {
			viewportHeight = 1
		}
		m.viewport.Width = m.width
		m.viewport.Height = viewportHeight
		return m, nil

	case approvalRequestMsg:
		m.state = StateAwaitingApproval
		m.approvalPrompt = NewApprovalPrompt(msg.tool, msg.input, m.width)
		m.pendingApproval = &approvalRequest{
			tool:     msg.tool,
			input:    msg.input,
			response: msg.response,
		}
		return m, m.waitForApproval()

	case turnStartedMsg:
		m.eventCh = msg.ch
		return m.handleTurnEvent(TurnEventMsg(msg.first))

	case TurnEventMsg:
		return m.handleTurnEvent(msg)

	case spinner.TickMsg:
		if m.state == StateStreaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always quits, regardless of state.
	if msg.Type == tea.KeyCtrlC {
		m.quitting = true
		return m, tea.Quit
	}

	// Delegate to approval prompt when awaiting approval.
	if m.state == StateAwaitingApproval && m.approvalPrompt != nil {
		if m.approvalPrompt.HandleKey(msg) {
			result := m.approvalPrompt.Result()
			approved := result == ApprovalYes || result == ApprovalAlways
			if result == ApprovalAlways {
				m.alwaysApproved[m.pendingApproval.tool] = true
			}
			m.pendingApproval.response <- approved
			m.approvalPrompt = nil
			m.pendingApproval = nil
			m.state = StateStreaming
			return m, m.waitForEvent()
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		if m.state != StateInput {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.Reset()

		if strings.HasPrefix(text, "/") {
			cmd := m.handleCommand(text)
			return m, cmd
		}

		// Regular user message: write to content and start agent turn
		m.content.WriteString(fmt.Sprintf("> %s\n", text))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		m.assistantStartIdx = m.content.Len()
		m.state = StateStreaming

		return m, tea.Batch(m.startTurn(text), m.spinner.Tick)

	default:
		// Forward key to input area
		if m.state == StateInput {
			cmd := m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

// startTurn initiates an agent turn and returns a tea.Cmd that sends back a
// turnStartedMsg carrying the channel and first event. This avoids mutating
// m.eventCh from the Cmd goroutine (which would be a data race).
func (m *Model) startTurn(text string) tea.Cmd {
	return func() tea.Msg {
		if m.agent == nil {
			return TurnEventMsg(agent.TurnEvent{
				Type:  "error",
				Error: fmt.Errorf("no agent configured"),
			})
		}

		ch, err := m.agent.Turn(context.Background(), text)
		if err != nil {
			return TurnEventMsg(agent.TurnEvent{
				Type:  "error",
				Error: fmt.Errorf("turn failed: %w", err),
			})
		}

		// Read first event in the Cmd goroutine, but pass the channel
		// back via turnStartedMsg so Update sets m.eventCh safely.
		evt, ok := <-ch
		if !ok {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}
		return turnStartedMsg{ch: ch, first: evt}
	}
}

// waitForEvent returns a tea.Cmd that reads the next event from the event
// channel and returns it as a TurnEventMsg.
func (m *Model) waitForEvent() tea.Cmd {
	ch := m.eventCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}
		return TurnEventMsg(evt)
	}
}

// handleTurnEvent processes a streaming TurnEvent from the agent.
func (m *Model) handleTurnEvent(msg TurnEventMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case "text_delta":
		m.rawAssistant.WriteString(msg.Text)
		m.content.WriteString(msg.Text)
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "tool_call":
		name := ""
		args := ""
		if msg.ToolCall != nil {
			name = msg.ToolCall.Name
			args = string(msg.ToolCall.Input)
		}
		m.content.WriteString(m.toolBox.RenderToolCall(name, args))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "tool_result":
		resultContent := ""
		resultName := ""
		isError := false
		if msg.ToolResult != nil {
			resultContent = msg.ToolResult.Content
			resultName = msg.ToolResult.Name
			isError = msg.ToolResult.IsError
		}
		m.content.WriteString(m.toolBox.RenderToolResult(resultName, resultContent, isError))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "error":
		errMsg := "unknown error"
		if msg.Error != nil {
			errMsg = msg.Error.Error()
		}
		m.content.WriteString(fmt.Sprintf("Error: %s\n", errMsg))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "done":
		raw := m.rawAssistant.String()
		if raw != "" {
			rendered, err := m.mdRenderer.Render(raw)
			if err == nil && rendered != "" {
				contentStr := m.content.String()
				m.content.Reset()
				m.content.WriteString(contentStr[:m.assistantStartIdx])
				m.content.WriteString(rendered)
			}
		}
		m.rawAssistant.Reset()
		m.content.WriteString("\n")
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()

		// Update status bar with token usage and turn count.
		m.turnCount++
		m.statusBar.SetTokens(msg.InputTokens, 100000)
		m.statusBar.SetTurn(m.turnCount, m.maxTurns)
		cost := EstimateCost(m.modelName, msg.InputTokens, msg.OutputTokens)
		m.totalCost += cost
		m.statusBar.SetCost(m.totalCost)

		m.state = StateInput
		m.eventCh = nil
		return m, nil
	}

	return m, m.waitForEvent()
}
