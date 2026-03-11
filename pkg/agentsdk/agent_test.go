package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements LLMProvider for testing.
type mockProvider struct {
	responses [][]StreamEvent // one response per call
	callIdx   int
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

	ch, err := a.Turn(ctx, "hi")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have error + done.
	assert.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "error", events[0].Type)
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

	checker := &mockApprovalChecker{results: map[string]ApprovalResult{
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

// echoTool is a simple test tool that echoes its input.
type echoTool struct{}

func (e *echoTool) Name() string                 { return "echo" }
func (e *echoTool) Description() string          { return "echoes input" }
func (e *echoTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (e *echoTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: string(input)}, nil
}
