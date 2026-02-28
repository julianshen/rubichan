package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if d.callIdx >= len(d.responses) {
		return nil, fmt.Errorf("dynamicMockProvider: no more responses (call #%d, have %d)", d.callIdx, len(d.responses))
	}
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

func TestWithAgentMD(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	agentMDContent := "## Project Rules\nAlways use TDD."
	a := New(mp, reg, autoApprove, cfg, WithAgentMD(agentMDContent))

	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "Project Guidelines")
	assert.Contains(t, prompt, agentMDContent)
}

func TestWithExtraSystemPrompt(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg,
		WithExtraSystemPrompt("Apple Platform Expertise", "You are an expert in iOS development."),
	)

	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "## Apple Platform Expertise")
	assert.Contains(t, prompt, "You are an expert in iOS development.")
}

func TestWithExtraSystemPromptMultiple(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg,
		WithExtraSystemPrompt("Section A", "Content A"),
		WithExtraSystemPrompt("Section B", "Content B"),
	)

	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "## Section A")
	assert.Contains(t, prompt, "Content A")
	assert.Contains(t, prompt, "## Section B")
	assert.Contains(t, prompt, "Content B")
}

func TestWithExtraSystemPromptAfterAgentMD(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg,
		WithAgentMD("Project rules here"),
		WithExtraSystemPrompt("Extra Section", "Extra content"),
	)

	prompt := a.conversation.SystemPrompt()
	// Both should be present.
	assert.Contains(t, prompt, "Project Guidelines")
	assert.Contains(t, prompt, "Project rules here")
	assert.Contains(t, prompt, "## Extra Section")
	assert.Contains(t, prompt, "Extra content")

	// Extra section should come after AGENT.md section.
	agentMDIdx := strings.Index(prompt, "Project Guidelines")
	extraIdx := strings.Index(prompt, "Extra Section")
	assert.Greater(t, extraIdx, agentMDIdx, "extra prompt should appear after AGENT.md")
}

func TestWithAgentMD_Empty(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg, WithAgentMD(""))

	prompt := a.conversation.SystemPrompt()
	assert.NotContains(t, prompt, "Project Guidelines")
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
			// Fallback — should never be reached if context cancellation is honoured.
			{
				{Type: "text_delta", Text: "unexpected second turn"},
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

func TestToolResultEventCarriesDisplayContent(t *testing.T) {
	// A tool that sets DisplayContent should propagate it to the event.
	displayTool := &mockTool{
		name:        "display_tool",
		description: "returns dual content",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{
				Content:        "compact for LLM",
				DisplayContent: "rich for user",
			}, nil
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_dc",
					Name: "display_tool",
				}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(displayTool))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "use display tool")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var found bool
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.ToolResult.Name == "display_tool" {
			found = true
			assert.Equal(t, "compact for LLM", ev.ToolResult.Content)
			assert.Equal(t, "rich for user", ev.ToolResult.DisplayContent)
		}
	}
	assert.True(t, found, "should have tool_result event with DisplayContent")
}

func TestToolResultEventEmptyDisplayContent(t *testing.T) {
	// Error paths (denied, unknown tool, hook cancel) should leave DisplayContent empty.
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_deny",
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
	require.NoError(t, reg.Register(tools.NewFileTool(tmpDir)))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoDeny, cfg)

	ch, err := a.Turn(context.Background(), "read x.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	for _, ev := range events {
		if ev.Type == "tool_result" {
			assert.Empty(t, ev.ToolResult.DisplayContent, "error paths should not set DisplayContent")
		}
	}
}

func TestTurnDoneEventCarriesTokenUsage(t *testing.T) {
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hello!"},
			{Type: "stop", InputTokens: 120, OutputTokens: 45},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(mp, reg, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "hi")
	require.NoError(t, err)

	var doneEvent TurnEvent
	for ev := range ch {
		if ev.Type == "done" {
			doneEvent = ev
		}
	}

	assert.Equal(t, "done", doneEvent.Type)
	assert.Equal(t, 120, doneEvent.InputTokens)
	assert.Equal(t, 45, doneEvent.OutputTokens)
}

func TestTurnDoneEventAccumulatesTokensAcrossEvents(t *testing.T) {
	// Some providers may send partial token counts in multiple events
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hi"},
			{Type: "text_delta", Text: " there", InputTokens: 50, OutputTokens: 10},
			{Type: "stop", InputTokens: 100, OutputTokens: 30},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(mp, reg, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var doneEvent TurnEvent
	for ev := range ch {
		if ev.Type == "done" {
			doneEvent = ev
		}
	}

	// Should accumulate: 50+100 input, 10+30 output
	assert.Equal(t, 150, doneEvent.InputTokens)
	assert.Equal(t, 40, doneEvent.OutputTokens)
}

// --- Parallel tool execution tests ---

// autoApproveChecker wraps autoApprove and implements AutoApproveChecker.
type autoApproveCheckerFunc struct{}

func (autoApproveCheckerFunc) IsAutoApproved(tool string) bool {
	return true
}

func TestParallelToolExecutionAutoApproved(t *testing.T) {
	// Two auto-approved tools should run concurrently. We verify by checking
	// that both tools execute and results are returned in order.
	var callOrder []string
	var mu sync.Mutex

	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			mu.Lock()
			callOrder = append(callOrder, "a")
			mu.Unlock()
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}
	toolB := &mockTool{
		name:        "tool_b",
		description: "tool B",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			mu.Lock()
			callOrder = append(callOrder, "b")
			mu.Unlock()
			return tools.ToolResult{Content: "result_b"}, nil
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "tool_a"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "tool_b"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Both done."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))
	require.NoError(t, reg.Register(toolB))

	checker := autoApproveCheckerFunc{}
	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithAutoApproveChecker(checker))

	ch, err := a.Turn(context.Background(), "run both tools")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Both tools should have executed.
	mu.Lock()
	assert.Len(t, callOrder, 2)
	mu.Unlock()

	// Verify results are in original tool call order (t1 before t2).
	var resultIDs []string
	for _, ev := range events {
		if ev.Type == "tool_result" {
			resultIDs = append(resultIDs, ev.ToolResult.ID)
		}
	}
	assert.Equal(t, []string{"t1", "t2"}, resultIDs)
}

func TestParallelResultsAddedInOrder(t *testing.T) {
	// Verify conversation messages maintain original tool order even with parallel execution.
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}
	toolB := &mockTool{
		name:        "tool_b",
		description: "tool B",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_b"}, nil
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "tool_a"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "tool_b"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))
	require.NoError(t, reg.Register(toolB))

	checker := autoApproveCheckerFunc{}
	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithAutoApproveChecker(checker))

	ch, err := a.Turn(context.Background(), "run both")
	require.NoError(t, err)
	for range ch {
	}

	// Check conversation: user, assistant (tool_use), tool_result t1, tool_result t2, assistant (text)
	msgs := a.conversation.Messages()
	// Find tool_result messages — they should be in order.
	var toolResultIDs []string
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				toolResultIDs = append(toolResultIDs, block.ToolUseID)
			}
		}
	}
	assert.Equal(t, []string{"t1", "t2"}, toolResultIDs)
}

func TestSequentialFallbackForNonAutoApproved(t *testing.T) {
	// When the approval function doesn't implement AutoApproveChecker,
	// tools should still execute sequentially (existing behavior).
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "tool_a"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))

	// Use plain autoApprove — no AutoApproveChecker interface.
	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "run tool")
	require.NoError(t, err)

	var hasResult bool
	for ev := range ch {
		if ev.Type == "tool_result" {
			hasResult = true
			assert.Equal(t, "result_a", ev.ToolResult.Content)
		}
	}
	assert.True(t, hasResult)
}

func TestMixedParallelAndSequential(t *testing.T) {
	// tool_a is auto-approved, tool_b is not. They should be partitioned:
	// tool_a runs in the parallel batch, tool_b runs sequentially after.
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}
	toolB := &mockTool{
		name:        "tool_b",
		description: "tool B",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_b"}, nil
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "tool_a"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "tool_b"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))
	require.NoError(t, reg.Register(toolB))

	// Checker that only auto-approves tool_a.
	partialChecker := &selectiveChecker{approved: map[string]bool{"tool_a": true}}
	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithAutoApproveChecker(partialChecker))

	ch, err := a.Turn(context.Background(), "run mixed tools")
	require.NoError(t, err)

	var resultNames []string
	for ev := range ch {
		if ev.Type == "tool_result" {
			resultNames = append(resultNames, ev.ToolResult.Name)
		}
	}
	// Both tools should execute and results should be in original order.
	assert.Equal(t, []string{"tool_a", "tool_b"}, resultNames)

	// Verify conversation messages are also in original order.
	var toolResultIDs []string
	for _, msg := range a.conversation.Messages() {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				toolResultIDs = append(toolResultIDs, block.ToolUseID)
			}
		}
	}
	assert.Equal(t, []string{"t1", "t2"}, toolResultIDs)
}

func TestMixedParallelReversedOrder(t *testing.T) {
	// Regression: needs-approval tool first (index 0), auto-approved second (index 1).
	// Results must still be added to conversation in original order (t1 before t2).
	toolA := &mockTool{
		name:        "tool_a",
		description: "needs approval",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}
	toolB := &mockTool{
		name:        "tool_b",
		description: "auto-approved",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_b"}, nil
		},
	}

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				// tool_a (needs approval) at index 0, tool_b (auto-approved) at index 1
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "tool_a"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "tool_b"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))
	require.NoError(t, reg.Register(toolB))

	// Only tool_b is auto-approved.
	partialChecker := &selectiveChecker{approved: map[string]bool{"tool_b": true}}
	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithAutoApproveChecker(partialChecker))

	ch, err := a.Turn(context.Background(), "run reversed mixed tools")
	require.NoError(t, err)

	var resultIDs []string
	for ev := range ch {
		if ev.Type == "tool_result" {
			resultIDs = append(resultIDs, ev.ToolResult.ID)
		}
	}
	// Results must be in original order: t1 (needs-approval) before t2 (auto-approved).
	assert.Equal(t, []string{"t1", "t2"}, resultIDs)

	// Conversation must also have them in order.
	var convIDs []string
	for _, msg := range a.conversation.Messages() {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				convIDs = append(convIDs, block.ToolUseID)
			}
		}
	}
	assert.Equal(t, []string{"t1", "t2"}, convIDs)
}

// selectiveChecker only auto-approves tools in its approved map.
type selectiveChecker struct {
	approved map[string]bool
}

func (s *selectiveChecker) IsAutoApproved(tool string) bool {
	return s.approved[tool]
}
