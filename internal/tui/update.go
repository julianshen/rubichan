package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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

// maxToolResultDisplay is the maximum number of characters to display for a
// tool result before truncation.
const maxToolResultDisplay = 500

// Init implements tea.Model. It starts the text input cursor blinking.
func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model. It processes incoming messages and returns the
// updated model and any commands to execute.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	switch msg.Type {
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		if m.state != StateInput {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.SetValue("")

		if strings.HasPrefix(text, "/") {
			cmd := m.handleCommand(text)
			return m, cmd
		}

		// Regular user message: write to content and start agent turn
		m.content.WriteString(fmt.Sprintf("> %s\n", text))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		m.state = StateStreaming

		return m, tea.Batch(m.startTurn(text), m.spinner.Tick)

	default:
		// Forward key to textinput
		if m.state == StateInput {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
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
		m.content.WriteString(msg.Text)
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "tool_call":
		name := ""
		if msg.ToolCall != nil {
			name = msg.ToolCall.Name
		}
		m.content.WriteString(fmt.Sprintf("\n[Tool: %s]\n", name))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "tool_result":
		resultContent := ""
		if msg.ToolResult != nil {
			resultContent = msg.ToolResult.Content
		}
		if len(resultContent) > maxToolResultDisplay {
			resultContent = resultContent[:maxToolResultDisplay] + "..."
		}
		m.content.WriteString(fmt.Sprintf("Result: %s\n", resultContent))
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
		m.content.WriteString("\n")
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		m.state = StateInput
		m.eventCh = nil
		return m, nil
	}

	return m, m.waitForEvent()
}
