package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a simple mock that returns a fixed sequence of stream events.
type mockProvider struct {
	events []provider.StreamEvent
}

func (m *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, len(m.events))
	for _, e := range m.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// dynamicMockProvider returns different responses per call.
type dynamicMockProvider struct {
	responses [][]provider.StreamEvent
	callIdx   int
}

func (d *dynamicMockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	events := d.responses[d.callIdx]
	d.callIdx++
	ch := make(chan provider.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// errorProvider always returns an error from Stream.
type errorProvider struct {
	err error
}

func (e *errorProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	return nil, e.err
}

// mockTool implements tools.Tool for testing.
type mockTool struct {
	name        string
	description string
	inputSchema json.RawMessage
	executeFn   func(ctx context.Context, input json.RawMessage) (tools.ToolResult, error)
}

func (m *mockTool) Name() string                 { return m.name }
func (m *mockTool) Description() string          { return m.description }
func (m *mockTool) InputSchema() json.RawMessage { return m.inputSchema }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	return m.executeFn(ctx, input)
}

func autoApprove(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return true, nil
}

func autoDeny(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return false, nil
}

func TestNewAgent(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	agent := New(mp, reg, autoApprove, cfg)

	require.NotNil(t, agent)
	assert.Equal(t, cfg.Provider.Model, agent.model)
	assert.Equal(t, cfg.Agent.MaxTurns, agent.maxTurns)
	assert.NotNil(t, agent.conversation)
	assert.NotNil(t, agent.context)
	assert.NotNil(t, agent.provider)
	assert.NotNil(t, agent.tools)
	assert.NotNil(t, agent.approve)
}

func TestNewAgentSystemPrompt(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	agent := New(mp, reg, autoApprove, cfg)

	// System prompt should be non-empty
	assert.NotEmpty(t, agent.conversation.SystemPrompt())
}

func TestTurnTextOnly(t *testing.T) {
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hello "},
			{Type: "text_delta", Text: "world!"},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "say hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have: text_delta "Hello ", text_delta "world!", done
	require.GreaterOrEqual(t, len(events), 3)

	// Verify text deltas
	var textContent string
	for _, ev := range events {
		if ev.Type == "text_delta" {
			textContent += ev.Text
		}
	}
	assert.Equal(t, "Hello world!", textContent)

	// Last event should be done
	assert.Equal(t, "done", events[len(events)-1].Type)

	// Conversation should have 2 messages: user + assistant
	msgs := agent.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Hello world!", msgs[1].Content[0].Text)
}

func TestTurnMaxTurnsExceeded(t *testing.T) {
	// Create a provider that always returns a tool call to force recursive loops.
	// But we set maxTurns to 0 so the first runLoop iteration hits the limit.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "hi"},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 0 // immediate limit
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have error event about max turns and done event
	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
			assert.Contains(t, ev.Error.Error(), "max turns")
		}
	}
	assert.True(t, hasError, "should emit error event for max turns exceeded")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestTurnWithToolCall(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a test file for the file tool to read
	testFilePath := filepath.Join(tmpDir, "hello.txt")
	err := os.WriteFile(testFilePath, []byte("hello from file"), 0644)
	require.NoError(t, err)

	// First call: LLM returns a tool_use for file read
	// Second call: LLM returns text after seeing the tool result
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				// First response: tool use
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_123",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"hello.txt"}`},
				{Type: "stop"},
			},
			{
				// Second response: text after tool result
				{Type: "text_delta", Text: "The file contains: hello from file"},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	fileTool := tools.NewFileTool(tmpDir)
	err = reg.Register(fileTool)
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "read hello.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Verify we got tool_call, tool_result, text_delta, and done events
	var hasToolCall, hasToolResult, hasDone bool
	var toolResultContent string
	for _, ev := range events {
		switch ev.Type {
		case "tool_call":
			hasToolCall = true
			assert.Equal(t, "tool_123", ev.ToolCall.ID)
			assert.Equal(t, "file", ev.ToolCall.Name)
		case "tool_result":
			hasToolResult = true
			assert.Equal(t, "tool_123", ev.ToolResult.ID)
			assert.Equal(t, "file", ev.ToolResult.Name)
			assert.False(t, ev.ToolResult.IsError)
			toolResultContent = ev.ToolResult.Content
		case "done":
			hasDone = true
		}
	}

	assert.True(t, hasToolCall, "should have tool_call event")
	assert.True(t, hasToolResult, "should have tool_result event")
	assert.True(t, hasDone, "should have done event")
	assert.Equal(t, "hello from file", toolResultContent)
}

func TestTurnWithDeniedTool(t *testing.T) {
	// LLM returns a tool_use, but approval is denied
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_456",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"secret.txt"}`},
				{Type: "stop"},
			},
			{
				// Second response after denied tool result
				{Type: "text_delta", Text: "I understand, I cannot access that file."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	tmpDir := t.TempDir()
	fileTool := tools.NewFileTool(tmpDir)
	err := reg.Register(fileTool)
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoDeny, cfg)

	ch, err := agent.Turn(context.Background(), "read secret.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Verify tool_result event has IsError=true and "denied" content
	var hasToolResult bool
	for _, ev := range events {
		if ev.Type == "tool_result" {
			hasToolResult = true
			assert.True(t, ev.ToolResult.IsError)
			assert.Contains(t, ev.ToolResult.Content, "denied")
		}
	}
	assert.True(t, hasToolResult, "should have tool_result event with denial")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestTurnWithUnknownTool(t *testing.T) {
	// LLM returns a tool_use for a tool that doesn't exist in the registry
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_789",
					Name: "nonexistent",
				}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Sorry about that."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "use nonexistent tool")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var hasToolResult bool
	for _, ev := range events {
		if ev.Type == "tool_result" {
			hasToolResult = true
			assert.True(t, ev.ToolResult.IsError)
			assert.Contains(t, ev.ToolResult.Content, "unknown tool")
		}
	}
	assert.True(t, hasToolResult, "should have tool_result event for unknown tool")
}

func TestTurnWithStreamInitError(t *testing.T) {
	// Provider returns an error from Stream() itself (not during streaming)
	errProvider := &errorProvider{err: fmt.Errorf("auth failed")}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(errProvider, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
			assert.Contains(t, ev.Error.Error(), "auth failed")
		}
	}
	assert.True(t, hasError, "should have error event from Stream() failure")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestTurnWithApprovalError(t *testing.T) {
	// The approval function returns an error
	approvalErr := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return false, fmt.Errorf("approval service unavailable")
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_err",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"x.txt"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "OK"},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	tmpDir := t.TempDir()
	fileTool := tools.NewFileTool(tmpDir)
	err := reg.Register(fileTool)
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, approvalErr, cfg)

	ch, err := agent.Turn(context.Background(), "read file")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var hasToolResult bool
	for _, ev := range events {
		if ev.Type == "tool_result" {
			hasToolResult = true
			assert.True(t, ev.ToolResult.IsError)
			assert.Contains(t, ev.ToolResult.Content, "approval error")
		}
	}
	assert.True(t, hasToolResult, "should have tool_result event with approval error")
}

func TestTurnWithToolExecutionError(t *testing.T) {
	// Use file tool with a request that causes an execution error (tool returns error in result)
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_exec",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"nonexistent.txt"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "File not found."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	tmpDir := t.TempDir()
	fileTool := tools.NewFileTool(tmpDir)
	err := reg.Register(fileTool)
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "read nonexistent")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// File tool returns error as ToolResult.IsError=true, not as Go error
	var hasToolResult bool
	for _, ev := range events {
		if ev.Type == "tool_result" {
			hasToolResult = true
			assert.True(t, ev.ToolResult.IsError)
		}
	}
	assert.True(t, hasToolResult, "should have tool_result event with error")
}

func TestTurnWithProviderError(t *testing.T) {
	// Provider returns an error event during streaming
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "partial"},
			{Type: "error", Error: fmt.Errorf("connection lost")},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
		}
	}
	assert.True(t, hasError, "should have error event")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestClearConversation(t *testing.T) {
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hello!"},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	// Run a turn to add messages to conversation
	ch, err := agent.Turn(context.Background(), "hi")
	require.NoError(t, err)
	for range ch {
		// drain
	}

	// Conversation should have messages
	require.NotEmpty(t, agent.conversation.Messages())

	// Clear and verify
	agent.ClearConversation()
	assert.Empty(t, agent.conversation.Messages())
	assert.NotEmpty(t, agent.conversation.SystemPrompt(), "system prompt should survive clear")
}

func TestSetModel(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	assert.Equal(t, "claude-sonnet-4-5", agent.model)

	agent.SetModel("claude-opus-4")
	assert.Equal(t, "claude-opus-4", agent.model)
}

func TestWithStoreOption(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Hello"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	assert.NotEmpty(t, a.sessionID, "session should be auto-created")

	// Verify session was persisted.
	sess, err := s.GetSession(a.sessionID)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "test-model", sess.Model)
}

func TestAgentWithStorePersistsMessages(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "I am well!"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	ch, err := a.Turn(context.Background(), "How are you?")
	require.NoError(t, err)

	for range ch {
	}

	msgs, err := s.GetMessages(a.sessionID)
	require.NoError(t, err)
	require.Len(t, msgs, 2, "should have user + assistant messages")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestWithResumeSession(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Create a session with history.
	require.NoError(t, s.CreateSession(store.Session{
		ID:           "resume-me",
		Model:        "gpt-4",
		SystemPrompt: "You are helpful.",
	}))
	require.NoError(t, s.AppendMessage("resume-me", "user", []provider.ContentBlock{
		{Type: "text", Text: "Hello"},
	}))
	require.NoError(t, s.AppendMessage("resume-me", "assistant", []provider.ContentBlock{
		{Type: "text", Text: "Hi there!"},
	}))

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "gpt-4"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Welcome back!"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s), WithResumeSession("resume-me"))

	assert.Equal(t, "resume-me", a.sessionID)

	// Conversation should have been hydrated.
	msgs := a.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "Hello", msgs[0].Content[0].Text)
	assert.Equal(t, "Hi there!", msgs[1].Content[0].Text)

	// System prompt should come from the stored session.
	assert.Equal(t, "You are helpful.", a.conversation.SystemPrompt())
}

func TestWithResumeSessionNotFound(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "gpt-4"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Fresh start"},
		{Type: "stop"},
	}}

	// Resume a nonexistent session — should gracefully fall back to new session.
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s), WithResumeSession("nonexistent"))

	// Should have created a new session (not "nonexistent").
	assert.NotEqual(t, "nonexistent", a.sessionID)
	assert.NotEmpty(t, a.sessionID)

	// New session should be in store.
	sess, err := s.GetSession(a.sessionID)
	require.NoError(t, err)
	require.NotNil(t, sess)
}

func TestAgentWithoutStoreStillWorks(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Hi"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg)
	assert.Empty(t, a.sessionID)
	assert.Nil(t, a.store)

	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)
	for range ch {
	}
	// Should work fine without store
}

func TestTurnContextCancelledDuringToolLoop(t *testing.T) {
	// LLM returns a tool_use, but context is cancelled before tool execution.
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_ctx",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"x.txt"}`},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	tmpDir := t.TempDir()
	require.NoError(t, reg.Register(tools.NewFileTool(tmpDir)))

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ch, err := agent.Turn(ctx, "read file")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
		}
	}
	assert.True(t, hasError, "should get error from cancelled context")
	assert.Equal(t, "done", events[len(events)-1].Type)
}

func TestTurnWithToolExecuteGoError(t *testing.T) {
	// Register a tool that returns a Go error (not a ToolResult error).
	errorTool := &mockTool{
		name:        "bad_tool",
		description: "always errors",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{}, fmt.Errorf("internal tool failure")
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_go_err",
					Name: "bad_tool",
				}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Noted the error."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(errorTool))

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "use bad tool")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var hasToolResult bool
	for _, ev := range events {
		if ev.Type == "tool_result" {
			hasToolResult = true
			assert.True(t, ev.ToolResult.IsError)
			assert.Contains(t, ev.ToolResult.Content, "tool execution error")
		}
	}
	assert.True(t, hasToolResult, "should have tool_result event with execution error")
}

func TestAgentPersistMessageErrorIsNonFatal(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Still works!"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	require.NotEmpty(t, a.sessionID)

	// Close the store to force persistence errors.
	s.Close()

	// Turn should still work — persistence errors are non-fatal.
	ch, err := a.Turn(context.Background(), "Hello after close")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should complete normally despite persistence failure.
	assert.Equal(t, "done", events[len(events)-1].Type)

	var hasText bool
	for _, ev := range events {
		if ev.Type == "text_delta" {
			hasText = true
		}
	}
	assert.True(t, hasText, "should still get text from LLM")
}
