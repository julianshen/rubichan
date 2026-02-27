package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
)

func TestUIStateConstants(t *testing.T) {
	states := []UIState{
		StateInput,
		StateStreaming,
		StateAwaitingApproval,
		StateConfigOverlay,
		StateBootstrap,
	}
	seen := make(map[UIState]bool)
	for _, s := range states {
		assert.False(t, seen[s], "duplicate UIState value: %d", s)
		seen[s] = true
	}
}

func TestNewModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")

	assert.Equal(t, StateInput, m.state)
	assert.Equal(t, "rubichan", m.appName)
	assert.Equal(t, "claude-3", m.modelName)
	assert.Equal(t, 80, m.width)
	assert.Equal(t, 24, m.height)
	assert.False(t, m.quitting)
	assert.NotNil(t, m.input)
	assert.NotNil(t, m.viewport)
	assert.NotNil(t, m.spinner)
}

func TestModelHandleSlashQuit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/quit")

	require.NotNil(t, cmd, "handleCommand(/quit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)

	// Verify it produces a tea.Quit message
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelHandleSlashExit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/exit")

	require.NotNil(t, cmd, "handleCommand(/exit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)
}

func TestModelHandleSlashClear(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")

	// Write some content first
	m.content.WriteString("some previous content")

	cmd := m.handleCommand("/clear")

	assert.Nil(t, cmd, "handleCommand(/clear) should return nil (doesn't quit)")
	assert.Equal(t, "", m.content.String())
}

func TestModelHandleSlashHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/help")

	assert.Nil(t, cmd, "handleCommand(/help) should return nil")
	content := m.content.String()
	assert.Contains(t, content, "/quit")
	assert.Contains(t, content, "/clear")
	assert.Contains(t, content, "/model")
	assert.Contains(t, content, "/help")
}

func TestModelHandleSlashModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/model gpt-4")

	assert.Nil(t, cmd, "handleCommand(/model) should return nil")
	assert.Equal(t, "gpt-4", m.modelName)
	assert.True(t, strings.Contains(m.content.String(), "Model switched"))
}

func TestModelHandleSlashModelNoArg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/model")

	assert.Nil(t, cmd)
	assert.Equal(t, "claude-3", m.modelName, "model should not change without argument")
	assert.Contains(t, m.content.String(), "Usage:")
}

func TestModelHandleUnknownCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/unknown")

	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "Unknown command")
}

// --- Task 24 Tests ---

func TestModelInit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.Init()

	// Init should return textinput.Blink
	assert.NotNil(t, cmd, "Init should return a non-nil tea.Cmd (textinput.Blink)")
}

func TestModelView(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	view := m.View()

	assert.Contains(t, view, "rubichan")
	assert.Contains(t, view, "claude-3")
}

func TestModelViewQuitting(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.quitting = true
	view := m.View()

	assert.Equal(t, "Goodbye!\n", view)
}

func TestModelUpdateCtrlC(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	um := updated.(*Model)
	assert.True(t, um.quitting)
	require.NotNil(t, cmd)
	// cmd should produce QuitMsg
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelUpdateWindowSize(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	um := updated.(*Model)
	assert.Equal(t, 120, um.width)
	assert.Equal(t, 40, um.height)
}

func TestModelUpdateEnterSlashCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.input.SetValue("/help")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "/quit")
}

func TestModelUpdateEnterEmptyInput(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.input.SetValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	// Empty input should not change state
	assert.Equal(t, StateInput, um.state)
	assert.Equal(t, "", um.content.String())
}

func TestModelHandleTurnEventTextDelta(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	// Provide event channel so waitForEvent returns a non-nil cmd
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "hello world",
	})

	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "hello world")
	// Should continue waiting for events
	assert.NotNil(t, cmd)
}

func TestModelHandleTurnEventToolCall(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	// Provide a channel so waitForEvent has something to read from
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_call",
		ToolCall: &agent.ToolCallEvent{
			ID:   "tool-1",
			Name: "read_file",
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	content := um.content.String()
	// Should render in a bordered box with tool name
	assert.Contains(t, content, "read_file")
	assert.Contains(t, content, "\u256d") // rounded border top-left
	assert.Contains(t, content, "\u2570") // rounded border bottom-left
}

func TestModelHandleTurnEventToolResult(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "tool-1",
			Name:    "read_file",
			Content: "file contents here",
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	content := um.content.String()
	// Should render in a bordered box
	assert.Contains(t, content, "file contents here")
	assert.Contains(t, content, "\u256d") // rounded border top-left
}

func TestModelHandleTurnEventToolResultTruncation(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// Create a long result with many lines that should be truncated by ToolBoxRenderer
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "line content here\n"
	}
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "tool-1",
			Name:    "read_file",
			Content: longContent,
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	content := um.content.String()
	// ToolBoxRenderer truncates by line count and shows "[N more lines]"
	assert.Contains(t, content, "more lines")
}

func TestModelHandleTurnEventError(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:  "error",
		Error: assert.AnError,
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "Error:")
}

func TestModelHandleTurnEventDone(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{Type: "done"})

	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.eventCh)
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventDoneRendersMarkdown(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming

	// Simulate user prompt written before streaming starts
	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()

	// Simulate text_delta events with markdown content
	m.rawAssistant.WriteString("Hello **world**")
	m.content.WriteString("Hello **world**")

	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{Type: "done"})
	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, cmd)

	content := um.content.String()
	// The raw ** markers should have been replaced by Glamour rendering
	assert.NotContains(t, content, "**world**")
	// But the rendered text should still contain the word
	assert.Contains(t, content, "world")
	// The user prompt should still be present
	assert.Contains(t, content, "> hello")
}

func TestModelViewStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	view := m.View()

	// During streaming, should show spinner/thinking indicator
	assert.Contains(t, view, "Thinking")
}

func TestModelViewAwaitingApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateAwaitingApproval
	view := m.View()

	assert.Contains(t, view, "Approve")
}

func TestModelUpdateEnterUserMessage(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.input.SetValue("hello agent")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Contains(t, um.content.String(), "> hello agent")
	// Should return a batch command (startTurn + spinner tick)
	assert.NotNil(t, cmd)
}

func TestModelStartTurnNilAgent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.startTurn("test")

	require.NotNil(t, cmd)
	msg := cmd()
	evt, ok := msg.(TurnEventMsg)
	require.True(t, ok)
	assert.Equal(t, "error", evt.Type)
	assert.NotNil(t, evt.Error)
	assert.Contains(t, evt.Error.Error(), "no agent configured")
}

func TestModelUpdateRegularKey(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")

	// Type a regular character
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	um := updated.(*Model)
	// Should remain in input state; cmd from textinput update
	assert.Equal(t, StateInput, um.state)
	_ = cmd // textinput may return a blink cmd
}

func TestModelUpdateEnterWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	m.input.SetValue("ignored")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, cmd)
}

func TestModelUpdateSpinnerTick(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming

	// Spinner tick should update the spinner
	tickCmd := m.spinner.Tick
	msg := tickCmd()

	updated, _ := m.Update(msg)
	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
}

func TestModelUpdateSpinnerTickNotStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateInput

	// Spinner tick while not streaming should be handled gracefully
	tickCmd := m.spinner.Tick
	msg := tickCmd()

	updated, _ := m.Update(msg)
	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
}

func TestModelWaitForEventClosedChannel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	cmd := m.waitForEvent()
	require.NotNil(t, cmd)

	msg := cmd()
	evt, ok := msg.(TurnEventMsg)
	require.True(t, ok)
	assert.Equal(t, "done", evt.Type)
}

func TestModelWaitForEventNilChannel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.eventCh = nil

	cmd := m.waitForEvent()
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventToolCallNilToolCall(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)
	// Nil tool_call should render a bordered box with empty name
	content := um.content.String()
	assert.Contains(t, content, "\u256d")
}

func TestModelHandleTurnEventToolResultNilToolResult(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)
	// Nil tool_result should render a bordered box with empty content
	content := um.content.String()
	assert.Contains(t, content, "\u256d")
}

func TestModelHandleTurnEventErrorNilError(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:  "error",
		Error: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "Error: unknown error")
}

func TestModelUpdateWindowSizeTiny(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 2})

	um := updated.(*Model)
	assert.Equal(t, 10, um.width)
	assert.Equal(t, 2, um.height)
}

func TestModelHandleCommandEmptyString(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("")
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventUnknownType(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "unknown_type",
	})

	updated, cmd := m.Update(evt)
	um := updated.(*Model)
	// Unknown types should still continue reading
	assert.Equal(t, StateStreaming, um.state)
	assert.NotNil(t, cmd)
}

func TestModelUpdateUnknownMsg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")

	// Send an unrecognized message type
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, cmd)
}

func TestModelUpdateKeyWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	m.state = StateStreaming

	// Regular key press while streaming should be ignored (not forwarded to textinput)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, cmd)
}
