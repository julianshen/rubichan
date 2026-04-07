package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements LLMProvider for testing.
type mockProvider struct {
	responses [][]StreamEvent // one response per call
	callIdx   int
}

type mockUIHandler struct {
	mu    sync.Mutex
	resp  UIResponse
	err   error
	calls int
	last  UIRequest
}

func (m *mockUIHandler) Request(_ context.Context, req UIRequest) (UIResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.last = req
	if m.err != nil {
		return UIResponse{}, m.err
	}
	return m.resp, nil
}

func (m *mockUIHandler) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockProvider) Stream(_ context.Context, _ CompletionRequest) (<-chan StreamEvent, error) {
	if m.callIdx >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses")
	}
	events := m.responses[m.callIdx]
	m.callIdx++

	ch := make(chan StreamEvent, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func textResponse(text string) []StreamEvent {
	return []StreamEvent{
		{Type: "text_delta", Text: text},
		{Type: "stop", InputTokens: 100, OutputTokens: 50},
	}
}

func toolCallResponse(id, name, inputJSON string) []StreamEvent {
	return []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: id, Name: name}},
		{Type: "text_delta", Text: inputJSON},
		{Type: "stop", InputTokens: 100, OutputTokens: 50},
	}
}

func TestNewAgentDefaults(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("hi")}}
	a := NewAgent(p)

	assert.NotNil(t, a.tools)
	assert.NotNil(t, a.logger)
	assert.NotNil(t, a.conversation)
	assert.Equal(t, "claude-sonnet-4-5", a.config.Model)
}

func TestNewAgentWithOptions(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("hi")}}
	r := NewRegistry()
	a := NewAgent(p,
		WithTools(r),
		WithModel("gpt-4o"),
		WithSystemPrompt("Be concise."),
	)

	assert.Equal(t, "gpt-4o", a.config.Model)
	assert.Equal(t, "Be concise.", a.conversation.SystemPrompt())
}

func TestAgentTurnTextOnly(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("Hello!")}}
	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "Hi")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have: text_delta + done.
	assert.Len(t, events, 2)
	assert.Equal(t, "text_delta", events[0].Type)
	assert.Equal(t, "Hello!", events[0].Text)
	assert.Equal(t, "done", events[1].Type)
}

func TestAgentTurnWithToolCall(t *testing.T) {
	// First response: tool call. Second response: text reply.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{"text":"hi"}`),
		textResponse("Done."),
	}}

	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))

	ch, err := a.Turn(context.Background(), "use echo tool")
	require.NoError(t, err)

	var types []string
	for ev := range ch {
		types = append(types, ev.Type)
	}

	assert.Contains(t, types, "tool_call")
	assert.Contains(t, types, "tool_result")
	assert.Contains(t, types, "text_delta")
	assert.Contains(t, types, "done")
}

func TestAgentTurnUnknownTool(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "nonexistent", `{}`),
		textResponse("ok"),
	}}

	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}

	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "unknown tool")
}

func TestAgentTurnContextCancelled(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("hi")}}
	a := NewAgent(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := a.Turn(ctx, "hi")
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestAgentTurnEmptyMessage(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("hi")}}
	a := NewAgent(p)

	_, err := a.Turn(context.Background(), "")
	require.Error(t, err)
	assert.Equal(t, ErrEmptyMessage, err)
}

func TestNewAgentNilProviderPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewAgent(nil)
	})
}

func TestAgentTurnProviderError(t *testing.T) {
	p := &mockProvider{responses: nil} // no responses → error
	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "hi")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have error + done.
	assert.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "error", events[0].Type)
	assert.Contains(t, events[0].Error.Error(), "no more mock responses")
}

func TestAgentTurnMaxTurns(t *testing.T) {
	// Provider always returns tool calls, so the loop will exceed max turns.
	responses := make([][]StreamEvent, 5)
	for i := range responses {
		responses[i] = toolCallResponse(fmt.Sprintf("tc_%d", i), "echo", `{"text":"x"}`)
	}

	p := &mockProvider{responses: responses}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	cfg := DefaultAgentConfig()
	cfg.MaxTurns = 3

	a := NewAgent(p, WithTools(r), WithConfig(cfg))

	ch, err := a.Turn(context.Background(), "loop")
	require.NoError(t, err)

	var lastError string
	for ev := range ch {
		if ev.Type == "error" && ev.Error != nil {
			lastError = ev.Error.Error()
		}
	}
	assert.Contains(t, lastError, "max turns (3) exceeded")
}

func TestAgentApprovalDenied(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}

	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	denyAll := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, nil
	}

	a := NewAgent(p, WithTools(r), WithApproval(denyAll))

	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}

	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "denied")
}

func TestAgentApprovalCheckerAutoDenied(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}

	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": AutoDenied,
	}}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker))

	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}

	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "deny-always")
}

func TestAgentConversationGrows(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		textResponse("response 1"),
		textResponse("response 2"),
	}}
	a := NewAgent(p)

	ch1, _ := a.Turn(context.Background(), "msg1")
	for range ch1 {
	}
	assert.Len(t, a.Conversation().Messages(), 2) // user + assistant

	ch2, _ := a.Turn(context.Background(), "msg2")
	for range ch2 {
	}
	assert.Len(t, a.Conversation().Messages(), 4) // 2 + user + assistant
}

func TestAgentWithLogger(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("hi")}}
	l := &captureLogger{}
	a := NewAgent(p, WithLogger(l))
	assert.Equal(t, l, a.logger)
}

func TestAgentApprovalCheckerRequiresApproval(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}
	approveAll := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return true, nil
	}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker), WithApproval(approveAll))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.False(t, toolResults[0].ToolResult.IsError)
}

func TestAgentApprovalCheckerRequiresApprovalNoFunc(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "approval function not configured")
}

func TestAgentApprovalCheckerUsesUIRequestHandler(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}
	ui := &mockUIHandler{
		resp: UIResponse{RequestID: "tc_1", ActionID: "allow"},
	}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker), WithUIRequestHandler(ui))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Equal(t, 1, ui.Calls())
	assert.Equal(t, UIKindApproval, ui.last.Kind)
	assert.Equal(t, "echo", ui.last.Metadata["tool"])
	assert.Contains(t, eventTypes(events), "ui_request")
	assert.Contains(t, eventTypes(events), "ui_response")
}

func TestAgentNoCheckerUsesUIRequestHandler(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	ui := &mockUIHandler{
		resp: UIResponse{RequestID: "tc_1", ActionID: "deny"},
	}

	a := NewAgent(p, WithTools(r), WithUIRequestHandler(ui))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "denied")
}

func TestAgentUIRequestHandlerDenyAlwaysMessage(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}
	ui := &mockUIHandler{
		resp: UIResponse{RequestID: "tc_1", ActionID: "deny_always"},
	}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker), WithUIRequestHandler(ui))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Equal(t, "Tool call denied by user (deny-always).", toolResults[0].ToolResult.Content)
}

func TestAgentUIRequestMetadataInputTruncated(t *testing.T) {
	largeInput := `{"data":"` + strings.Repeat("x", maxUIRequestInputBytes+200) + `"}`
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", largeInput),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))
	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}
	ui := &mockUIHandler{
		resp: UIResponse{RequestID: "tc_1", ActionID: "allow"},
	}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker), WithUIRequestHandler(ui))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)
	for range ch {
	}

	require.NotEmpty(t, ui.last.Metadata["input"])
	assert.LessOrEqual(t, len(ui.last.Metadata["input"]), maxUIRequestInputBytes+len("...(truncated)"))
	assert.True(t, strings.HasSuffix(ui.last.Metadata["input"], "...(truncated)"))
}

func TestAgentUIRequestHandlerIDMismatch(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}
	ui := &mockUIHandler{
		resp: UIResponse{RequestID: "wrong-id", ActionID: "allow"},
	}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker), WithUIRequestHandler(ui))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "approval error")
}

func eventTypes(events []TurnEvent) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

func TestAgentApprovalFuncError(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	errApprove := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, fmt.Errorf("approval service unavailable")
	}

	a := NewAgent(p, WithTools(r), WithApproval(errApprove))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "approval error")
}

func TestAgentStreamingTool(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "stream_echo", `{"text":"hello"}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&streamEchoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var progressEvents []TurnEvent
	var resultEvents []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_progress" {
			progressEvents = append(progressEvents, ev)
		}
		if ev.Type == "tool_result" {
			resultEvents = append(resultEvents, ev)
		}
	}
	assert.Len(t, progressEvents, 1)
	assert.Equal(t, "streaming...", progressEvents[0].ToolProgress.Content)
	require.Len(t, resultEvents, 1)
	assert.False(t, resultEvents[0].ToolResult.IsError)
	assert.Equal(t, "hello", resultEvents[0].ToolResult.Content)
}

func TestAgentStreamingToolError(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "fail_stream", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&failStreamTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "tool error")
}

func TestAgentToolExecuteError(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "fail_tool", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&failTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "tool error")
}

func TestAgentApprovalCheckerApprovalFuncError(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	checker := &agentMockChecker{results: map[string]ApprovalResult{
		"echo": ApprovalRequired,
	}}
	errApprove := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, fmt.Errorf("checker approval error")
	}

	a := NewAgent(p, WithTools(r), WithApprovalChecker(checker), WithApproval(errApprove))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolResults, 1)
	assert.True(t, toolResults[0].ToolResult.IsError)
	assert.Contains(t, toolResults[0].ToolResult.Content, "approval error")
}

func TestAgentContextCancelledDuringLoop(t *testing.T) {
	// Provider returns a tool call; context is cancelled by the tool.
	// On the next loop iteration, runLoop should detect cancellation.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "cancel_tool", `{}`),
		// Second call won't happen — context is cancelled.
	}}
	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, r.Register(&contextCancelTool{cancel: cancel}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(ctx, "test")
	require.NoError(t, err)

	var hasError, hasDone bool
	for ev := range ch {
		if ev.Type == "error" {
			hasError = true
		}
		if ev.Type == "done" {
			hasDone = true
		}
	}
	assert.True(t, hasError)
	assert.True(t, hasDone)
}

func TestAgentStreamErrorDiscardsToolCalls(t *testing.T) {
	// Stream produces a tool_use then an error before stop.
	// The error should prevent tool execution.
	errorStream := []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "echo"}},
		{Type: "text_delta", Text: `{"text":"hi"}`},
		{Type: "error", Error: fmt.Errorf("stream interrupted")},
		{Type: "stop", InputTokens: 50, OutputTokens: 25},
	}
	p := &mockProvider{responses: [][]StreamEvent{errorStream, textResponse("ok")}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var types []string
	for ev := range ch {
		types = append(types, ev.Type)
	}

	assert.Contains(t, types, "error")
	assert.Contains(t, types, "done")
	// Tool should NOT have been executed since the stream had an error.
	assert.NotContains(t, types, "tool_call")
	assert.NotContains(t, types, "tool_result")
	// Conversation should only have the user message — no partial assistant blocks.
	assert.Len(t, a.Conversation().Messages(), 1)
	assert.Equal(t, "user", a.Conversation().Messages()[0].Role)
}

func TestAgentStreamErrorWithPartialText(t *testing.T) {
	// Stream produces text, then a tool_use (which finalizes text), then an error.
	// The partial text block should NOT be added to conversation.
	errorStream := []StreamEvent{
		{Type: "text_delta", Text: "partial response"},
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "echo"}},
		{Type: "error", Error: fmt.Errorf("connection lost")},
		{Type: "stop", InputTokens: 50, OutputTokens: 25},
	}
	p := &mockProvider{responses: [][]StreamEvent{errorStream}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	logger := &captureLogger{}
	a := NewAgent(p, WithTools(r), WithLogger(logger))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)
	for range ch {
	}

	// Conversation should only have the user message.
	assert.Len(t, a.Conversation().Messages(), 1)
	// Stream error should have been logged.
	require.Len(t, logger.errors, 1)
	assert.Contains(t, logger.errors[0], "connection lost")
}

func TestAgentContextCancelledMidToolBatch(t *testing.T) {
	// Two tool calls in one response; first tool cancels context.
	twoToolStream := []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "cancel_tool"}},
		{Type: "text_delta", Text: `{}`},
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_2", Name: "echo"}},
		{Type: "text_delta", Text: `{}`},
		{Type: "stop", InputTokens: 100, OutputTokens: 50},
	}
	p := &mockProvider{responses: [][]StreamEvent{twoToolStream}}
	r := NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	cancelTool := &contextCancelTool{cancel: cancel}
	require.NoError(t, r.Register(cancelTool))
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(ctx, "test")
	require.NoError(t, err)

	var toolCalls []string
	for ev := range ch {
		if ev.Type == "tool_call" {
			toolCalls = append(toolCalls, ev.ToolCall.Name)
		}
	}

	// First tool should execute, second should be skipped due to cancellation.
	assert.Contains(t, toolCalls, "cancel_tool")
	assert.NotContains(t, toolCalls, "echo")
}

func TestAgentApprovalFuncErrorLogged(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("ok"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	logger := &captureLogger{}
	errApprove := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, fmt.Errorf("approval service down")
	}

	a := NewAgent(p, WithTools(r), WithApproval(errApprove), WithLogger(logger))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)
	for range ch {
	}

	// The approval error should be logged.
	require.Len(t, logger.errors, 1)
	assert.Contains(t, logger.errors[0], "approval failure")
	assert.Contains(t, logger.errors[0], "approval service down")
}

func TestAgentMultipleToolCallsInSingleResponse(t *testing.T) {
	// Two tool calls in one LLM response.
	multiToolStream := []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "echo"}},
		{Type: "text_delta", Text: `{"text":"a"}`},
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_2", Name: "echo"}},
		{Type: "text_delta", Text: `{"text":"b"}`},
		{Type: "stop", InputTokens: 100, OutputTokens: 50},
	}
	p := &mockProvider{responses: [][]StreamEvent{multiToolStream, textResponse("done")}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var toolCallNames []string
	var toolResultContents []string
	for ev := range ch {
		if ev.Type == "tool_call" {
			toolCallNames = append(toolCallNames, ev.ToolCall.Name)
		}
		if ev.Type == "tool_result" {
			toolResultContents = append(toolResultContents, ev.ToolResult.Content)
		}
	}

	assert.Len(t, toolCallNames, 2)
	assert.Len(t, toolResultContents, 2)
}

func TestAgentToolUseWithInlineInput(t *testing.T) {
	inlineInputStream := []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "echo", Input: json.RawMessage(`{"text":"inline"}`)}},
		{Type: "stop", InputTokens: 100, OutputTokens: 50},
	}
	p := &mockProvider{responses: [][]StreamEvent{inlineInputStream, textResponse("done")}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var results []string
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, ev.ToolResult.Content)
		}
	}

	require.Len(t, results, 1)
	assert.Equal(t, `{"text":"inline"}`, results[0])
}

func TestAgentToolUsePrefersDeltaInputOverInlineSeed(t *testing.T) {
	stream := []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "echo", Input: json.RawMessage(`{}`)}},
		{Type: "text_delta", Text: `{"text":"delta"}`},
		{Type: "stop", InputTokens: 100, OutputTokens: 50},
	}
	p := &mockProvider{responses: [][]StreamEvent{stream, textResponse("done")}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var results []string
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, ev.ToolResult.Content)
		}
	}

	require.Len(t, results, 1)
	assert.Equal(t, `{"text":"delta"}`, results[0])
}

func TestAgentNilToolUseEventEmitsError(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		{
			{Type: "tool_use", ToolUse: nil},
			{Type: "stop", InputTokens: 10, OutputTokens: 5},
		},
	}}
	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var sawError bool
	for ev := range ch {
		if ev.Type == "error" && ev.Error != nil {
			sawError = true
			assert.Contains(t, ev.Error.Error(), "nil ToolUse")
		}
	}
	assert.True(t, sawError, "expected malformed tool_use error event")
}

// contextCancelTool cancels the provided context on execution.
type contextCancelTool struct {
	cancel context.CancelFunc
}

func (c *contextCancelTool) Name() string                 { return "cancel_tool" }
func (c *contextCancelTool) Description() string          { return "cancels context" }
func (c *contextCancelTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (c *contextCancelTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	c.cancel()
	return ToolResult{Content: "cancelled"}, nil
}

// echoTool is a simple test tool that echoes its input.
type echoTool struct{}

func (e *echoTool) Name() string                 { return "echo" }
func (e *echoTool) Description() string          { return "echoes input" }
func (e *echoTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (e *echoTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: string(input)}, nil
}

// captureLogger captures log output for testing.
type captureLogger struct {
	warns  []string
	errors []string
}

func (l *captureLogger) Warn(msg string, args ...any) {
	l.warns = append(l.warns, fmt.Sprintf(msg, args...))
}
func (l *captureLogger) Error(msg string, args ...any) {
	l.errors = append(l.errors, fmt.Sprintf(msg, args...))
}

// streamEchoTool implements StreamingTool for testing.
type streamEchoTool struct{}

func (s *streamEchoTool) Name() string                 { return "stream_echo" }
func (s *streamEchoTool) Description() string          { return "streaming echo" }
func (s *streamEchoTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (s *streamEchoTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: string(input)}, nil
}
func (s *streamEchoTool) ExecuteStream(_ context.Context, input json.RawMessage, emit ToolEventEmitter) (ToolResult, error) {
	emit(ToolEvent{Stage: EventBegin, Content: "streaming..."})
	var args struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(input, &args)
	return ToolResult{Content: args.Text}, nil
}

// failStreamTool is a streaming tool that returns an error.
type failStreamTool struct{}

func (f *failStreamTool) Name() string                 { return "fail_stream" }
func (f *failStreamTool) Description() string          { return "fails" }
func (f *failStreamTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f *failStreamTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{}, fmt.Errorf("fail")
}
func (f *failStreamTool) ExecuteStream(_ context.Context, _ json.RawMessage, _ ToolEventEmitter) (ToolResult, error) {
	return ToolResult{}, fmt.Errorf("stream execution failed")
}

// failTool is a regular tool that returns an error.
type failTool struct{}

func (f *failTool) Name() string                 { return "fail_tool" }
func (f *failTool) Description() string          { return "fails" }
func (f *failTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f *failTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{}, fmt.Errorf("execution failed")
}

// agentMockChecker returns pre-configured results per tool name (for agent tests).
type agentMockChecker struct {
	results map[string]ApprovalResult
}

func (m *agentMockChecker) CheckApproval(toolName string, _ json.RawMessage) ApprovalResult {
	if r, ok := m.results[toolName]; ok {
		return r
	}
	return AutoApproved
}

// errorProvider returns a fixed error from Stream for testing.
type errorProvider struct {
	err error
}

func (p *errorProvider) Stream(_ context.Context, _ CompletionRequest) (<-chan StreamEvent, error) {
	return nil, p.err
}

// testProviderError is a mock ProviderError-like type implementing
// ContextOverflowError for testing the agent loop's error detection.
type testProviderError struct {
	kind    string
	message string
}

func (e *testProviderError) Error() string             { return e.message }
func (e *testProviderError) ProviderErrorKind() string { return e.kind }
func (e *testProviderError) IsRetryable() bool         { return false }

func TestAgent_Turn_ContextOverflowEvent(t *testing.T) {
	pe := &testProviderError{kind: ProviderErrContextOverflow, message: "prompt exceeds context window"}
	p := &errorProvider{err: pe}
	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have a context_overflow event followed by done.
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "context_overflow", events[0].Type)
	assert.Contains(t, events[0].Error.Error(), "context window")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestAgent_Turn_ProviderErrorPreservesKind(t *testing.T) {
	pe := &testProviderError{kind: "auth_failed", message: "invalid api key"}
	p := &errorProvider{err: pe}
	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Auth errors should still come through as "error" type events.
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "error", events[0].Type)
	assert.Contains(t, events[0].Error.Error(), "invalid api key")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestAgent_ConsumeStream_InputJsonDelta(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "t1", Name: "read"}},
		{Type: "input_json_delta", Text: `{"path":`},
		{Type: "input_json_delta", Text: `"/tmp"}`},
		{Type: "stop", InputTokens: 10, OutputTokens: 5},
	}}}

	reg := NewRegistry()
	reg.Register(&dummyTool{name: "read"})
	a := NewAgent(p, WithTools(reg))

	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var jsonDeltas []string
	var toolCalls []string
	for ev := range ch {
		switch ev.Type {
		case "input_json_delta":
			jsonDeltas = append(jsonDeltas, ev.Text)
		case "tool_call":
			toolCalls = append(toolCalls, ev.ToolCall.Name)
		}
	}

	// input_json_delta should be forwarded to TUI.
	assert.Equal(t, []string{`{"path":`, `"/tmp"}`}, jsonDeltas)
	// Tool call should have accumulated the JSON.
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "read", toolCalls[0])
}

func TestAgent_ConsumeStream_MessageStart(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{{
		{Type: "message_start", Model: "gpt-4o", MessageID: "msg_123"},
		{Type: "text_delta", Text: "hello"},
		{Type: "stop", InputTokens: 10, OutputTokens: 5},
	}}}
	a := NewAgent(p)

	ch, err := a.Turn(context.Background(), "hi")
	require.NoError(t, err)

	var messageStarts []TurnEvent
	for ev := range ch {
		if ev.Type == "message_start" {
			messageStarts = append(messageStarts, ev)
		}
	}

	require.Len(t, messageStarts, 1)
	assert.Equal(t, "gpt-4o", messageStarts[0].Model)
	assert.Equal(t, "msg_123", messageStarts[0].MessageID)
}
