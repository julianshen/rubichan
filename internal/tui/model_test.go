package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("/quit")

	require.NotNil(t, cmd, "handleCommand(/quit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)

	// Verify it produces a tea.Quit message
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelHandleSlashExit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("/exit")

	require.NotNil(t, cmd, "handleCommand(/exit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)
}

func TestModelHandleSlashClear(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	// Write some content first
	m.content.WriteString("some previous content")

	cmd := m.handleCommand("/clear")

	assert.Nil(t, cmd, "handleCommand(/clear) should return nil (doesn't quit)")
	assert.Equal(t, "", m.content.String())
}

func TestModelHandleSlashHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("/help")

	assert.Nil(t, cmd, "handleCommand(/help) should return nil")
	content := m.content.String()
	assert.Contains(t, content, "/quit")
	assert.Contains(t, content, "/clear")
	assert.Contains(t, content, "/model")
	assert.Contains(t, content, "/help")
}

func TestModelHandleSlashModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("/model gpt-4")

	assert.Nil(t, cmd, "handleCommand(/model) should return nil")
	assert.Equal(t, "gpt-4", m.modelName)
	assert.True(t, strings.Contains(m.content.String(), "Model switched"))
}

func TestModelHandleSlashModelNoArg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("/model")

	assert.Nil(t, cmd)
	assert.Equal(t, "claude-3", m.modelName, "model should not change without argument")
	assert.Contains(t, m.content.String(), "Usage:")
}

func TestModelHandleUnknownCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("/unknown")

	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "Unknown command")
}

// --- Task 24 Tests ---

func TestModelInit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.Init()

	// Init should return the input area's init command (focus)
	assert.NotNil(t, cmd, "Init should return a non-nil tea.Cmd")
}

func TestModelView(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	view := m.View()

	assert.Contains(t, view, "rubichan")
	assert.Contains(t, view, "claude-3")
}

func TestModelViewQuitting(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.quitting = true
	view := m.View()

	assert.Equal(t, "Goodbye!\n", view)
}

func TestModelUpdateCtrlC(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	um := updated.(*Model)
	assert.True(t, um.quitting)
	require.NotNil(t, cmd)
	// cmd should produce QuitMsg
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelUpdateWindowSize(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	um := updated.(*Model)
	assert.Equal(t, 120, um.width)
	assert.Equal(t, 40, um.height)
}

func TestModelUpdateEnterSlashCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.input.SetValue("/help")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "/quit")
}

func TestModelUpdateEnterEmptyInput(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	contentBefore := m.content.String()
	m.input.SetValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	// Empty input should not change state or add new content
	assert.Equal(t, StateInput, um.state)
	assert.Equal(t, contentBefore, um.content.String())
}

func TestModelHandleTurnEventTextDelta(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming
	view := m.View()

	// During streaming, should show spinner/thinking indicator
	assert.Contains(t, view, "Thinking")
}

func TestModelViewAwaitingApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 80)
	view := m.View()

	assert.Contains(t, view, "Allow")
	assert.Contains(t, view, "(y)es")
}

func TestModelUpdateEnterUserMessage(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.input.SetValue("hello agent")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Contains(t, um.content.String(), "> hello agent")
	// Should return a batch command (startTurn + spinner tick)
	assert.NotNil(t, cmd)
}

func TestModelStartTurnNilAgent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.startTurn(nil, "test")

	require.NotNil(t, cmd)
	msg := cmd()
	evt, ok := msg.(TurnEventMsg)
	require.True(t, ok)
	assert.Equal(t, "error", evt.Type)
	assert.NotNil(t, evt.Error)
	assert.Contains(t, evt.Error.Error(), "no agent configured")
}

func TestModelUpdateRegularKey(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	// Type a regular character
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	um := updated.(*Model)
	// Should remain in input state; cmd from input area update
	assert.Equal(t, StateInput, um.state)
	_ = cmd // input area may return a cursor cmd
}

func TestModelUpdateEnterWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming
	m.input.SetValue("ignored")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, cmd)
}

func TestModelUpdateSpinnerTick(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming

	// Spinner tick should update the spinner
	tickCmd := m.spinner.Tick
	msg := tickCmd()

	updated, _ := m.Update(msg)
	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
}

func TestModelUpdateSpinnerTickNotStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateInput

	// Spinner tick while not streaming should be handled gracefully
	tickCmd := m.spinner.Tick
	msg := tickCmd()

	updated, _ := m.Update(msg)
	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
}

func TestModelWaitForEventClosedChannel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.eventCh = nil

	cmd := m.waitForEvent()
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventToolCallNilToolCall(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 2})

	um := updated.(*Model)
	assert.Equal(t, 10, um.width)
	assert.Equal(t, 2, um.height)
}

func TestModelHandleCommandEmptyString(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.handleCommand("")
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventUnknownType(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
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
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	// Send an unrecognized message type
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, cmd)
}

func TestModelUpdateKeyWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming

	// Regular key press while streaming should be ignored (not forwarded to input area)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, cmd)
}

func TestModelStatusBarInView(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5", 50, "", nil)
	m.state = StateInput
	view := m.View()
	assert.Contains(t, view, "claude-sonnet-4-5")
	assert.Contains(t, view, "Turn 0/50")
}

func TestModelStatusBarUpdatedOnDone(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5", 50, "", nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:         "done",
		InputTokens:  1500,
		OutputTokens: 300,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	assert.Equal(t, 1, um.turnCount)
	view := um.View()
	assert.Contains(t, view, "Turn 1/50")
	assert.Contains(t, view, "1.5k")
}

// --- Config overlay tests ---

func TestModelHandleSlashConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg)

	cmd := m.handleCommand("/config")

	assert.Equal(t, StateConfigOverlay, m.state)
	assert.NotNil(t, m.configForm)
	assert.NotNil(t, cmd, "/config should return an init cmd from the form")
}

func TestModelHandleSlashConfigNilCfg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	cmd := m.handleCommand("/config")

	assert.Equal(t, StateInput, m.state, "should remain in input state without config")
	assert.Nil(t, m.configForm)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "No config available")
}

func TestModelConfigOverlayView(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg)

	m.handleCommand("/config")
	view := m.View()

	// The view should render the form, not the normal TUI
	assert.NotContains(t, view, "rubichan · claude-3", "config overlay should not show normal header")
}

func TestModelConfigOverlayCompleted(t *testing.T) {
	cfg := config.DefaultConfig()
	dir := t.TempDir()
	m := NewModel(nil, "rubichan", "claude-3", 50, dir+"/config.toml", cfg)

	m.handleCommand("/config")
	assert.Equal(t, StateConfigOverlay, m.state)

	// Simulate form completion by setting state directly
	m.configForm.Form().State = huh.StateCompleted

	// Send any message to trigger the state check in Update
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(*Model)

	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.configForm)
}

func TestModelConfigOverlayAborted(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg)

	m.handleCommand("/config")
	assert.Equal(t, StateConfigOverlay, m.state)

	// Simulate form abort
	m.configForm.Form().State = huh.StateAborted

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	um := updated.(*Model)

	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.configForm)
}

func TestModelHelpIncludesConfig(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.handleCommand("/help")

	content := m.content.String()
	assert.Contains(t, content, "/config")
}

func TestModelConfigOverlayRoutesMessages(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg)

	m.handleCommand("/config")
	assert.Equal(t, StateConfigOverlay, m.state)

	// Regular key message while in config overlay should be routed to form, not handled by model
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	um := updated.(*Model)

	// Should still be in config overlay state (not input state)
	assert.Equal(t, StateConfigOverlay, um.state)
}

// --- Approval wiring tests ---

func TestModelApprovalChannelInitialized(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	assert.NotNil(t, m.approvalCh)
	// sync.Map is always usable at zero value — verify Store/Load works.
	m.alwaysApproved.Store("test", true)
	_, ok := m.alwaysApproved.Load("test")
	assert.True(t, ok)
}

func TestModelApprovalRequestMsg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming

	// Simulate an approval request arriving
	respCh := make(chan bool, 1)
	msg := approvalRequestMsg{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	updated, cmd := m.Update(msg)
	um := updated.(*Model)

	assert.Equal(t, StateAwaitingApproval, um.state)
	assert.NotNil(t, um.approvalPrompt)
	assert.NotNil(t, cmd, "should return a cmd to wait for next approval")
}

func TestModelApprovalKeyYes(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	// Provide event channel so waitForEvent works
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um := updated.(*Model)

	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, um.approvalPrompt)
	assert.NotNil(t, cmd, "should return waitForEvent cmd")

	// Check that response was sent
	select {
	case approved := <-respCh:
		assert.True(t, approved)
	default:
		t.Fatal("expected response on channel")
	}
}

func TestModelApprovalKeyNo(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"rm -rf /"}`, 60)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"rm -rf /"}`,
		response: respCh,
	}

	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	um := updated.(*Model)

	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, um.approvalPrompt)
	assert.NotNil(t, cmd)

	select {
	case approved := <-respCh:
		assert.False(t, approved)
	default:
		t.Fatal("expected response on channel")
	}
}

func TestModelApprovalKeyAlways(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	um := updated.(*Model)

	assert.Equal(t, StateStreaming, um.state)
	_, alwaysOK := um.alwaysApproved.Load("shell")
	assert.True(t, alwaysOK)
	assert.NotNil(t, cmd)

	select {
	case approved := <-respCh:
		assert.True(t, approved)
	default:
		t.Fatal("expected response on channel")
	}
}

func TestModelApprovalUnhandledKey(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	um := updated.(*Model)

	// Should remain in awaiting approval state
	assert.Equal(t, StateAwaitingApproval, um.state)
	assert.NotNil(t, um.approvalPrompt)
	assert.Nil(t, cmd)

	// No response should have been sent
	select {
	case <-respCh:
		t.Fatal("no response expected for unhandled key")
	default:
		// expected
	}
}

func TestModelApprovalViewShowsPrompt(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval
	m.approvalPrompt = NewApprovalPrompt("file", `"/etc/hosts"`, 60)

	view := m.View()
	assert.Contains(t, view, "file")
	assert.Contains(t, view, "Allow")
	assert.Contains(t, view, "(y)es")
}

func TestModelMakeApprovalFunc(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	fn := m.MakeApprovalFunc()
	assert.NotNil(t, fn)
}

func TestModelIsAutoApproved(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	// Initially nothing is auto-approved.
	assert.False(t, m.IsAutoApproved("shell"))
	assert.False(t, m.IsAutoApproved("file"))

	// Mark shell as always-approved.
	m.alwaysApproved.Store("shell", true)
	assert.True(t, m.IsAutoApproved("shell"))
	assert.False(t, m.IsAutoApproved("file"), "unrelated tool should not be auto-approved")

	// Mark file as well.
	m.alwaysApproved.Store("file", true)
	assert.True(t, m.IsAutoApproved("file"))
}

func TestModelCheckApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	// Initially nothing is auto-approved.
	assert.Equal(t, agent.ApprovalRequired, m.CheckApproval("shell", json.RawMessage(`{}`)))

	// Mark shell as always-approved via session cache.
	m.alwaysApproved.Store("shell", true)
	assert.Equal(t, agent.AutoApproved, m.CheckApproval("shell", json.RawMessage(`{}`)))
	assert.Equal(t, agent.ApprovalRequired, m.CheckApproval("file", json.RawMessage(`{}`)))
}

func TestModelImplementsApprovalChecker(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	var _ agent.ApprovalChecker = m // compile-time check
}

func TestModelMakeApprovalFuncAlwaysApproved(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.alwaysApproved.Store("shell", true)

	fn := m.MakeApprovalFunc()

	// Should return true immediately for always-approved tools.
	// This runs in a goroutine to avoid blocking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		approved, err := fn(context.Background(), "shell", json.RawMessage(`{}`))
		assert.NoError(t, err)
		assert.True(t, approved)
	}()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("MakeApprovalFunc for always-approved tool should return immediately")
	}
}

func TestModelInitIncludesWaitForApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	cmd := m.Init()
	// Init should return a batch that includes waitForApproval
	assert.NotNil(t, cmd)
}

// --- Viewport scrolling tests ---

func TestModelViewportScrollUpPreservesPosition(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	// Set a small viewport so content overflows.
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content so it overflows the viewport.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	m.viewport.GotoBottom()

	// Scroll up via PageUp key.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	um := updated.(*Model)

	// After scrolling up, viewport should NOT be at the bottom.
	assert.False(t, um.viewport.AtBottom(), "viewport should not be at bottom after PageUp")
}

func TestModelAutoScrollWhenAtBottom(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content and stay at bottom.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	m.viewport.GotoBottom()

	// Provide event channel.
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// Receive new content — should auto-scroll since we're at bottom.
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "new streaming text\n",
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)

	assert.True(t, um.viewport.AtBottom(), "should auto-scroll when already at bottom")
}

func TestModelNoAutoScrollWhenScrolledUp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateStreaming
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content so it overflows.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	m.viewport.GotoBottom()

	// Scroll up first.
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "precondition: should be scrolled up")

	// Provide event channel.
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// Receive new content — should NOT auto-scroll since user scrolled up.
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "new streaming text\n",
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)

	assert.False(t, um.viewport.AtBottom(), "should NOT auto-scroll when user scrolled up")
}

func TestModelPageDownScrollsViewport(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	// Start at top.
	m.viewport.GotoTop()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	um := updated.(*Model)

	// Should have scrolled down from top.
	assert.Greater(t, um.viewport.YOffset, 0, "viewport should scroll down on PageDown")
}

func TestModelCtrlCDuringApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	um := updated.(*Model)

	assert.True(t, um.quitting)
	assert.Nil(t, um.pendingApproval, "pendingApproval should be cleared on Ctrl+C")
	require.NotNil(t, cmd)
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)

	// Verify the agent goroutine was unblocked with a denial.
	select {
	case approved := <-respCh:
		assert.False(t, approved, "Ctrl+C should deny the pending approval")
	default:
		t.Fatal("expected denial response on channel when Ctrl+C pressed during approval")
	}
}
