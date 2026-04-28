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
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/knowledgegraph"
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

type testTool struct {
	name string
}

func (t *testTool) Name() string                 { return t.name }
func (t *testTool) Description() string          { return "test tool" }
func (t *testTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *testTool) Execute(_ context.Context, _ json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{Content: "ok"}, nil
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

type mockStreamingTool struct {
	mockTool
	streamFn func(ctx context.Context, input json.RawMessage, emit tools.ToolEventEmitter) (tools.ToolResult, error)
}

func (m *mockStreamingTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit tools.ToolEventEmitter) (tools.ToolResult, error) {
	return m.streamFn(ctx, input, emit)
}

func autoApprove(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return true, nil
}

func autoDeny(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return false, nil
}

type countingApprovalChecker struct {
	mu     sync.Mutex
	calls  int
	result ApprovalResult
}

func (c *countingApprovalChecker) CheckApproval(_ string, _ json.RawMessage) ApprovalResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.result
}

func (c *countingApprovalChecker) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type mockUIRequestHandler struct {
	mu    sync.Mutex
	calls int
	last  UIRequest
	resp  UIResponse
	err   error
}

func (m *mockUIRequestHandler) Request(_ context.Context, req UIRequest) (UIResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.last = req
	if m.err != nil {
		return UIResponse{}, m.err
	}
	return m.resp, nil
}

func (m *mockUIRequestHandler) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestApprovalToolErrorResult(t *testing.T) {
	a := &Agent{logger: agentsdk.DefaultLogger()}
	tc := provider.ToolUseBlock{ID: "tool-1", Name: "file"}

	t.Run("without wrapped error", func(t *testing.T) {
		result := a.approvalToolErrorResult(tc, "tool call denied by user", nil)

		assert.Equal(t, "tool-1", result.toolUseID)
		assert.Equal(t, "tool call denied by user", result.content)
		assert.True(t, result.isError)
		require.NotNil(t, result.event)
		assert.Equal(t, "tool_result", result.event.Type)
		require.NotNil(t, result.event.ToolResult)
		assert.Equal(t, "tool-1", result.event.ToolResult.ID)
		assert.Equal(t, "file", result.event.ToolResult.Name)
		assert.Equal(t, "tool call denied by user", result.event.ToolResult.Content)
		assert.True(t, result.event.ToolResult.IsError)
	})

	t.Run("with wrapped error", func(t *testing.T) {
		result := a.approvalToolErrorResult(tc, "approval error", fmt.Errorf("approval service unavailable"))

		assert.Equal(t, "tool-1", result.toolUseID)
		assert.Equal(t, "approval error", result.content)
		assert.True(t, result.isError)
		require.NotNil(t, result.event)
		assert.Equal(t, "tool_result", result.event.Type)
		require.NotNil(t, result.event.ToolResult)
		assert.Equal(t, "approval error", result.event.ToolResult.Content)
		assert.True(t, result.event.ToolResult.IsError)
	})
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

	prompt := agent.conversation.SystemPrompt()
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "## Identity")
	assert.Contains(t, prompt, "## Soul")
	assert.Contains(t, prompt, "Ruby")
	assert.Contains(t, prompt, "ガンバ")
}

func TestNewAgentSkipsPreRegisteredTools(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	// Pre-register compact_context and tool_search (simulates subagent
	// inheriting parent's filtered registry).
	preCompact := tools.NewCompactContextTool(&agentCompactor{agent: &Agent{}})
	require.NoError(t, reg.Register(preCompact))
	preSearch := tools.NewToolSearchTool(tools.NewDeferralManager(0.10))
	require.NoError(t, reg.Register(preSearch))

	// New should not panic or log warnings about duplicate registration.
	a := New(mp, reg, autoApprove, cfg)
	require.NotNil(t, a)

	// Tools should still be present (not removed or double-registered).
	_, ok := reg.Get("compact_context")
	assert.True(t, ok)
	_, ok = reg.Get("tool_search")
	assert.True(t, ok)
}

func TestWithWorkingDir(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg, WithWorkingDir("/custom/dir"))
	assert.Equal(t, "/custom/dir", a.WorkingDir())
}

func TestWithWorkingDir_Fallback(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg)
	// Should fall back to os.Getwd().
	wd := a.WorkingDir()
	assert.NotEmpty(t, wd)
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

func TestWithIdentityMD(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg, WithIdentityMD("# Identity\nName: Custom Ruby"))

	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "Workspace Identity (from IDENTITY.md)")
	assert.Contains(t, prompt, "Name: Custom Ruby")
}

func TestWithSoulMD(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg, WithSoulMD("# Soul\nPrefer direct technical answers."))

	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "Workspace Soul (from SOUL.md)")
	assert.Contains(t, prompt, "Prefer direct technical answers")
}

func TestPromptSectionOrderWithIdentityAndSoul(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg,
		WithIdentityMD("workspace identity"),
		WithSoulMD("workspace soul"),
		WithAgentMD("project rules"),
		WithExtraSystemPrompt("Extra Section", "Extra content"),
	)

	prompt := a.conversation.SystemPrompt()
	systemIdx := strings.Index(prompt, "## System")
	identityIdx := strings.Index(prompt, "## Identity")
	soulIdx := strings.Index(prompt, "## Soul")
	agentIdx := strings.Index(prompt, "## Project Guidelines (from AGENT.md)")
	extraIdx := strings.Index(prompt, "## Extra Section")

	assert.Less(t, systemIdx, identityIdx)
	assert.Less(t, identityIdx, soulIdx)
	assert.Less(t, soulIdx, agentIdx)
	assert.Less(t, agentIdx, extraIdx)
}

func TestBuildSystemPromptWithFragmentsDoesNotNestStaticSections(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(mp, reg, autoApprove, cfg, WithIdentityMD("workspace identity"))

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "")
	assert.Equal(t, 1, strings.Count(prompt, "## System"))
	assert.Equal(t, 1, strings.Count(prompt, "## Identity"))
	assert.Equal(t, 1, strings.Count(prompt, "## Soul"))
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

func TestTurnEmptyResponseEmitsError(t *testing.T) {
	// When the LLM returns an empty response (no text, no tool calls),
	// the agent should emit an error event before the done event and
	// add a placeholder assistant message to keep the conversation valid.
	mp := &mockProvider{
		events: []provider.StreamEvent{
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

	// Should have: error, done
	require.GreaterOrEqual(t, len(events), 2)

	// There should be an error event indicating empty response.
	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" && ev.Error != nil {
			hasError = true
			assert.Contains(t, ev.Error.Error(), "empty response")
		}
	}
	assert.True(t, hasError, "expected an error event for empty LLM response")

	// Last event should be done.
	assert.Equal(t, "done", events[len(events)-1].Type)

	// Conversation should still have an assistant message to avoid dangling user message.
	msgs := agent.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestTurnWhitespaceOnlyResponseEmitsError(t *testing.T) {
	// When the LLM returns only whitespace (spaces, newlines, tabs),
	// it should be treated as an empty response and emit an error event
	// with a placeholder assistant message.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "  \n  \t  "},
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

	// Should have: error, done
	require.GreaterOrEqual(t, len(events), 2)

	// There should be an error event indicating empty response.
	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" && ev.Error != nil {
			hasError = true
			assert.Contains(t, ev.Error.Error(), "empty response")
		}
	}
	assert.True(t, hasError, "expected an error event for whitespace-only LLM response")

	// Last event should be done.
	assert.Equal(t, "done", events[len(events)-1].Type)

	// Conversation should still have an assistant message to avoid dangling user message.
	msgs := agent.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestTurnStreamErrorDiscardsPartialBlocks(t *testing.T) {
	// Simulate a stream that emits partial text then an error.
	// The agent should NOT add the partial blocks to conversation.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "partial "},
			{Type: "error", Error: fmt.Errorf("connection reset")},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "test")
	require.NoError(t, err)

	var sawError bool
	for ev := range ch {
		if ev.Type == "error" {
			sawError = true
		}
	}
	assert.True(t, sawError, "should have received error event")

	// Conversation should have only the user message — no partial assistant
	msgs := agent.conversation.Messages()
	require.Len(t, msgs, 1, "stream error should prevent partial assistant message")
	assert.Equal(t, "user", msgs[0].Role)
}

func TestSetModelRaceSafety(t *testing.T) {
	// Verify SetModel and Turn don't race. Use a gated provider so
	// SetModel contends with Turn's lock window deterministically.
	gate := make(chan struct{})
	mp := &channelProvider{
		events: func() <-chan provider.StreamEvent {
			ch := make(chan provider.StreamEvent, 2)
			go func() {
				<-gate // block until SetModel is launched
				ch <- provider.StreamEvent{Type: "text_delta", Text: "hi"}
				ch <- provider.StreamEvent{Type: "stop"}
				close(ch)
			}()
			return ch
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	// Launch SetModel while Turn holds turnMu (stream is blocked on gate)
	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.SetModel("gpt-4-turbo")
	}()

	// Unblock the stream so Turn can proceed
	close(gate)

	// Drain the channel
	for range ch {
	}
	<-done

	// No race detector failure = test passes
	assert.Equal(t, "gpt-4-turbo", agent.model)
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

func TestTurnDetectsRepeatedToolOnlyNoProgress(t *testing.T) {
	mp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "tool_1", Name: "search"}},
				{Type: "text_delta", Text: `{"pattern":"todo"}`},
				{Type: "stop"},
			},
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "tool_2", Name: "search"}},
				{Type: "text_delta", Text: `{"pattern":"todo"}`},
				{Type: "stop"},
			},
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "tool_3", Name: "search"}},
				{Type: "text_delta", Text: `{"pattern":"todo"}`},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&testTool{name: "search"}))

	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 10
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "loop")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var gotNoProgress bool
	for _, ev := range events {
		if ev.Type == "error" && ev.Error != nil && strings.Contains(ev.Error.Error(), "no progress") {
			gotNoProgress = true
		}
	}
	assert.True(t, gotNoProgress, "should emit no-progress error")
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

func TestTurnWithInlineToolUseInput(t *testing.T) {
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:    "tool_inline_1",
					Name:  "echo_inline",
					Input: json.RawMessage(`{"k":"v"}`),
				}},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "done"},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	echoInlineTool := &mockTool{
		name:        "echo_inline",
		description: "echoes raw input",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, input json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: string(input)}, nil
		},
	}
	require.NoError(t, reg.Register(echoInlineTool))

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "run inline tool")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}

	require.Len(t, toolResults, 1)
	require.NotNil(t, toolResults[0].ToolResult)
	assert.Equal(t, `{"k":"v"}`, toolResults[0].ToolResult.Content)
}

func TestTurnWithDeltaInputOverridesInlineToolUseSeed(t *testing.T) {
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:    "tool_inline_seed_1",
					Name:  "echo_inline",
					Input: json.RawMessage(`{}`),
				}},
				{Type: "text_delta", Text: `{"k":"delta"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "done"},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	echoInlineTool := &mockTool{
		name:        "echo_inline",
		description: "echoes raw input",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, input json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: string(input)}, nil
		},
	}
	require.NoError(t, reg.Register(echoInlineTool))

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "run inline tool")
	require.NoError(t, err)

	var toolResults []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			toolResults = append(toolResults, ev)
		}
	}

	require.Len(t, toolResults, 1)
	require.NotNil(t, toolResults[0].ToolResult)
	assert.Equal(t, `{"k":"delta"}`, toolResults[0].ToolResult.Content)
}

func TestTurnWithStreamingToolProgress(t *testing.T) {
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_stream_1",
					Name: "stream_tool",
				}},
				{Type: "text_delta", Text: `{"msg":"go"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "stream complete"},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	streamingTool := &mockStreamingTool{
		mockTool: mockTool{
			name: "stream_tool",
		},
		streamFn: func(_ context.Context, _ json.RawMessage, emit tools.ToolEventEmitter) (tools.ToolResult, error) {
			emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "begin"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "delta-1"})
			emit(tools.ToolEvent{Stage: tools.EventEnd, Content: "end"})
			return tools.ToolResult{Content: "ok"}, nil
		},
	}
	err := reg.Register(streamingTool)
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	agent := New(dmp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "run stream_tool")
	require.NoError(t, err)

	var progressStages []string
	for ev := range ch {
		if ev.Type == "tool_progress" && ev.ToolProgress != nil {
			progressStages = append(progressStages, ev.ToolProgress.Stage.String())
		}
	}

	assert.Equal(t, []string{"begin", "delta", "end"}, progressStages)
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
	assert.Equal(t, agentsdk.ExitProviderError, events[len(events)-1].ExitReason)
}

func TestRunLoop_PromptTooLong_SmallConversation_ExitsImmediately(t *testing.T) {
	errProvider := &errorProvider{err: fmt.Errorf("prompt is too long: 300000 tokens")}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(errProvider, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var exitReason agentsdk.TurnExitReason
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitContextOverflow, exitReason)
}

type retryAfterErrorProvider struct {
	err       error
	callCount int
}

func (r *retryAfterErrorProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	r.callCount++
	if r.callCount == 1 {
		return nil, r.err
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: "recovered"}
	ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
	close(ch)
	return ch, nil
}

func TestRunLoop_PromptTooLong_RecoversViaDrain(t *testing.T) {
	prov := &retryAfterErrorProvider{err: fmt.Errorf("prompt is too long: 300000 tokens")}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg)
	for i := 0; i < 10; i++ {
		agent.conversation.AddUser(fmt.Sprintf("prior message %d", i))
		agent.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("response %d", i)}})
	}

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var exitReason agentsdk.TurnExitReason
	var sawOverflowEvent bool
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
		if evt.Type == "context_overflow" {
			sawOverflowEvent = true
		}
	}
	assert.Equal(t, agentsdk.ExitCompleted, exitReason, "should recover after drain")
	assert.True(t, sawOverflowEvent, "should emit context_overflow event on recovery")
	assert.Equal(t, 2, prov.callCount, "should retry after drain")
}

func TestRunLoop_PromptTooLong_ExhaustsRetries(t *testing.T) {
	prov := &errorProvider{err: fmt.Errorf("prompt is too long: 300000 tokens")}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg)
	for i := 0; i < 20; i++ {
		agent.conversation.AddUser(fmt.Sprintf("prior message %d", i))
		agent.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("response %d", i)}})
	}

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var exitReason agentsdk.TurnExitReason
	var overflowEvents int
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
		if evt.Type == "context_overflow" {
			overflowEvents++
		}
	}
	assert.Equal(t, agentsdk.ExitContextOverflow, exitReason, "should exhaust retries and exit")
	assert.GreaterOrEqual(t, overflowEvents, 1, "should attempt at least one recovery before giving up")
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

func TestTurnWithUIRequestApproval(t *testing.T) {
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_ui_ok",
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
	filePath := filepath.Join(tmpDir, "x.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0o600))
	require.NoError(t, reg.Register(tools.NewFileTool(tmpDir)))

	ui := &mockUIRequestHandler{
		resp: UIResponse{
			RequestID: "tool_ui_ok",
			ActionID:  "allow",
		},
	}

	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: ApprovalRequired}),
		WithUIRequestHandler(ui),
	)

	ch, err := a.Turn(context.Background(), "read file")
	require.NoError(t, err)

	var gotReq, gotResp, gotToolResult bool
	for ev := range ch {
		switch ev.Type {
		case "ui_request":
			gotReq = true
			require.NotNil(t, ev.UIRequest)
			assert.Equal(t, UIKindApproval, ev.UIRequest.Kind)
			assert.Equal(t, "file", ev.UIRequest.Metadata["tool"])
		case "ui_response":
			gotResp = true
			require.NotNil(t, ev.UIResponse)
			assert.Equal(t, "allow", ev.UIResponse.ActionID)
		case "tool_result":
			gotToolResult = true
			assert.False(t, ev.ToolResult.IsError)
		}
	}

	assert.True(t, gotReq, "expected ui_request event")
	assert.True(t, gotResp, "expected ui_response event")
	assert.True(t, gotToolResult, "expected successful tool_result event")
	assert.Equal(t, 1, ui.Calls(), "expected one UI request")
}

func TestTurnWithUIRequestApprovalError(t *testing.T) {
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_ui_err",
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

	ui := &mockUIRequestHandler{
		err: fmt.Errorf("ui unavailable"),
	}

	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: ApprovalRequired}),
		WithUIRequestHandler(ui),
	)

	ch, err := a.Turn(context.Background(), "read file")
	require.NoError(t, err)

	var gotToolResult bool
	for ev := range ch {
		if ev.Type == "tool_result" {
			gotToolResult = true
			assert.True(t, ev.ToolResult.IsError)
			assert.Contains(t, ev.ToolResult.Content, "approval error")
		}
	}
	assert.True(t, gotToolResult, "expected failed tool_result event")
}

func TestTurnWithUIRequestIDMismatch(t *testing.T) {
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_ui_mismatch",
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

	ui := &mockUIRequestHandler{
		resp: UIResponse{
			RequestID: "different-id",
			ActionID:  "allow",
		},
	}

	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: ApprovalRequired}),
		WithUIRequestHandler(ui),
	)

	ch, err := a.Turn(context.Background(), "read file")
	require.NoError(t, err)

	var gotToolResult bool
	for ev := range ch {
		if ev.Type == "tool_result" {
			gotToolResult = true
			assert.True(t, ev.ToolResult.IsError)
			assert.Contains(t, ev.ToolResult.Content, "approval error")
		}
	}
	assert.True(t, gotToolResult, "expected failed tool_result event")
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

func TestResumeSessionRuntime(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Create a session with history.
	require.NoError(t, s.CreateSession(store.Session{
		ID:           "runtime-resume",
		Model:        "gpt-4",
		SystemPrompt: "You are helpful.",
	}))
	require.NoError(t, s.AppendMessage("runtime-resume", "user", []provider.ContentBlock{
		{Type: "text", Text: "What is Go?"},
	}))
	require.NoError(t, s.AppendMessage("runtime-resume", "assistant", []provider.ContentBlock{
		{Type: "text", Text: "Go is a programming language."},
	}))

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "gpt-4"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "stop"},
	}}

	// Start with a fresh session.
	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	originalID := a.SessionID()
	assert.NotEqual(t, "runtime-resume", originalID)

	// Resume at runtime.
	err = a.ResumeSession(context.Background(), "runtime-resume")
	require.NoError(t, err)

	assert.Equal(t, "runtime-resume", a.SessionID())
	msgs := a.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "What is Go?", msgs[0].Content[0].Text)
	assert.Equal(t, "Go is a programming language.", msgs[1].Content[0].Text)
	assert.Equal(t, "You are helpful.", a.conversation.SystemPrompt())
}

func TestResumeSessionRuntimeNotFound(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "gpt-4"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	originalID := a.SessionID()

	err = a.ResumeSession(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Session should remain unchanged.
	assert.Equal(t, originalID, a.SessionID())
}

func TestResumeSessionNoStore(t *testing.T) {
	cfg := config.DefaultConfig()
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg)

	err := a.ResumeSession(context.Background(), "any-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store not configured")
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

func TestAlwaysAutoApproveReturnsTrue(t *testing.T) {
	checker := AlwaysAutoApprove{}
	assert.True(t, checker.IsAutoApproved("shell"))
	assert.True(t, checker.IsAutoApproved("file"))
	assert.True(t, checker.IsAutoApproved(""))
}

func TestAllowAllParallelReturnsTrue(t *testing.T) {
	policy := AllowAllParallel{}
	assert.True(t, policy.CanParallelize("shell"))
	assert.True(t, policy.CanParallelize("file"))
	assert.True(t, policy.CanParallelize(""))
}

func TestToolParallelPolicyRestricts(t *testing.T) {
	// A policy that only allows "file" to parallelize.
	policy := &selectiveParallelPolicy{allowed: map[string]bool{"file": true}}
	assert.True(t, policy.CanParallelize("file"))
	assert.False(t, policy.CanParallelize("shell"))
}

// selectiveParallelPolicy is a test helper for ToolParallelPolicy.
type selectiveParallelPolicy struct {
	allowed map[string]bool
}

func (p *selectiveParallelPolicy) CanParallelize(tool string) bool {
	return p.allowed[tool]
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

func TestParallelPolicyRestrictsParallelization(t *testing.T) {
	// tool_a is parallelizable, tool_b is not. Both should execute successfully,
	// but tool_b runs sequentially after the parallel batch.
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

	// Only tool_a is parallelizable.
	policy := &selectiveParallelPolicy{allowed: map[string]bool{"tool_a": true}}
	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg,
		WithApprovalChecker(AlwaysAutoApprove{}),
		WithParallelPolicy(policy),
	)

	ch, err := a.Turn(context.Background(), "run both tools")
	require.NoError(t, err)

	var resultContents []string
	for ev := range ch {
		if ev.Type == "tool_result" {
			resultContents = append(resultContents, ev.ToolResult.Content)
		}
	}

	// Both tools should have executed.
	mu.Lock()
	assert.Len(t, callOrder, 2, "both tools should execute")
	mu.Unlock()

	// Both results should be present.
	assert.Contains(t, resultContents, "result_a")
	assert.Contains(t, resultContents, "result_b")
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

func TestSingleAutoApprovedToolDoesNotRequireApprovalFunc(t *testing.T) {
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

	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg, WithApprovalChecker(AlwaysAutoApprove{}))

	ch, err := a.Turn(context.Background(), "run tool")
	require.NoError(t, err)

	var hasResult bool
	for ev := range ch {
		if ev.Type == "tool_result" {
			hasResult = true
			assert.Equal(t, "result_a", ev.ToolResult.Content)
			assert.False(t, ev.ToolResult.IsError)
		}
	}
	assert.True(t, hasResult)
}

func TestMissingApprovalFuncReturnsToolError(t *testing.T) {
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			t.Fatal("tool should not execute when approval is required without an approval function")
			return tools.ToolResult{}, nil
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

	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg)

	ch, err := a.Turn(context.Background(), "run tool")
	require.NoError(t, err)

	var results []ToolResultEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, *ev.ToolResult)
		}
	}

	require.Len(t, results, 1)
	assert.True(t, results[0].IsError)
	assert.Equal(t, "approval function not configured", results[0].Content)
}

func TestMissingApprovalFuncWithApprovalCheckerReturnsToolError(t *testing.T) {
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			t.Fatal("tool should not execute when approval is required without an approval function")
			return tools.ToolResult{}, nil
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

	cfg := config.DefaultConfig()
	a := New(dmp, reg, nil, cfg, WithAutoApproveChecker(&selectiveChecker{approved: map[string]bool{}}))

	ch, err := a.Turn(context.Background(), "run tool")
	require.NoError(t, err)

	var results []ToolResultEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, *ev.ToolResult)
		}
	}

	require.Len(t, results, 1)
	assert.True(t, results[0].IsError)
	assert.Equal(t, "approval function not configured", results[0].Content)
}

func TestExecutePlannedToolsSequentialUsesPrecomputedApprovalResults(t *testing.T) {
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))

	checker := &countingApprovalChecker{result: AutoApproved}
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, nil, cfg, WithApprovalChecker(checker))

	planned := []plannedToolCall{{
		index:          0,
		tc:             provider.ToolUseBlock{ID: "t1", Name: "tool_a", Input: json.RawMessage(`{}`)},
		approvalResult: AutoApproved,
	}}

	ch := make(chan TurnEvent, 4)
	cancelled := a.executePlannedToolsSequential(context.Background(), ch, planned, nil)
	require.False(t, cancelled)
	assert.Equal(t, 0, checker.Calls(), "sequential execution should use the precomputed approval result")
}

func TestExecuteToolsWithoutApprovalCheckerUsesSequentialPlan(t *testing.T) {
	toolA := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "result_a"}, nil
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(toolA))

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg)

	ch := make(chan TurnEvent, 4)
	cancelled := a.executeTools(context.Background(), ch, []provider.ToolUseBlock{{
		ID:    "t1",
		Name:  "tool_a",
		Input: json.RawMessage(`{}`),
	}}, nil)
	require.False(t, cancelled)

	var events []TurnEvent
	for len(ch) > 0 {
		events = append(events, <-ch)
	}
	require.Len(t, events, 2)
	require.NotNil(t, events[0].ToolCall)
	require.NotNil(t, events[1].ToolResult)
	assert.Equal(t, "t1", events[0].ToolCall.ID)
	assert.Equal(t, "t1", events[1].ToolResult.ID)
	assert.Equal(t, "result_a", events[1].ToolResult.Content)
	assert.False(t, events[1].ToolResult.IsError)
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

func TestAgentNewWithContextEnhancements(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.MaxOutputTokens = 4096
	cfg.Agent.ResultOffloadThreshold = 2048

	p := &mockProvider{}
	reg := tools.NewRegistry()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	a := New(p, reg, autoApprove, cfg, WithStore(s))
	assert.NotNil(t, a)
	assert.NotNil(t, a.resultStore, "resultStore should be initialized when store is present")
	assert.NotNil(t, a.promptBuilder, "promptBuilder should always be initialized")
}

func TestAgentNewWithoutStoreNoResultStore(t *testing.T) {
	cfg := config.DefaultConfig()
	p := &mockProvider{}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg)
	assert.NotNil(t, a)
	assert.Nil(t, a.resultStore, "resultStore should be nil when no store is present")
	assert.NotNil(t, a.promptBuilder, "promptBuilder should always be initialized")
}

func TestAgentToolResultOffloading(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ResultOffloadThreshold = 20 // very small threshold

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	p := &mockProvider{}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&mockTool{
		name:        "big_output",
		description: "produces large output",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{
				Content: "this output is definitely large enough to trigger offloading behavior in the result store",
			}, nil
		},
	}))

	a := New(p, reg, autoApprove, cfg, WithStore(s))

	tc := provider.ToolUseBlock{ID: "t1", Name: "big_output", Input: json.RawMessage(`{}`)}
	result := a.executeSingleTool(context.Background(), make(chan TurnEvent, 8), tc)

	assert.Contains(t, result.content, "Tool result stored")
	assert.Contains(t, result.content, "read_result")
}

func TestAgentToolResultOffloadingSkipsSmallResults(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ResultOffloadThreshold = 10000 // high threshold

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	p := &mockProvider{}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&mockTool{
		name:        "small_output",
		description: "produces small output",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "small"}, nil
		},
	}))

	a := New(p, reg, autoApprove, cfg, WithStore(s))

	tc := provider.ToolUseBlock{ID: "t2", Name: "small_output", Input: json.RawMessage(`{}`)}
	result := a.executeSingleTool(context.Background(), make(chan TurnEvent, 8), tc)

	assert.Equal(t, "small", result.content, "small results should not be offloaded")
}

func TestAgentToolResultOffloadingSkipsErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ResultOffloadThreshold = 20 // small threshold

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	p := &mockProvider{}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&mockTool{
		name:        "error_tool",
		description: "produces error",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{
				Content: "this is a very long error message that exceeds the threshold",
				IsError: true,
			}, nil
		},
	}))

	a := New(p, reg, autoApprove, cfg, WithStore(s))

	tc := provider.ToolUseBlock{ID: "t3", Name: "error_tool", Input: json.RawMessage(`{}`)}
	result := a.executeSingleTool(context.Background(), make(chan TurnEvent, 8), tc)

	// Error results should NOT be offloaded.
	assert.NotContains(t, result.content, "Tool result stored")
	assert.True(t, result.isError)
}

func TestAgentSavesSnapshotAfterCompaction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ContextBudget = 500 // small budget to force compaction
	cfg.Agent.MaxOutputTokens = 0

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	p := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "done"},
		{Type: "stop"},
	}}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg, WithStore(s))

	// Fill conversation to make snapshot meaningful.
	for i := 0; i < 20; i++ {
		a.conversation.AddUser("message content for testing")
		a.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response"}})
	}

	a.saveSnapshotIfNeeded()

	// Verify snapshot was saved.
	snap, err := s.GetSnapshot(a.sessionID)
	require.NoError(t, err)
	assert.NotNil(t, snap, "snapshot should exist after saveSnapshotIfNeeded")
}

func TestAgentSaveSnapshotIfNeededWithoutStore(t *testing.T) {
	cfg := config.DefaultConfig()
	p := &mockProvider{}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg)

	// Should not panic when store is nil.
	a.saveSnapshotIfNeeded()
}

func TestWithDiffTrackerOption(t *testing.T) {
	dt := tools.NewDiffTracker()
	p := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(p, reg, autoApprove, cfg, WithDiffTracker(dt))
	assert.Equal(t, dt, a.diffTracker)
	assert.Equal(t, dt, a.DiffTracker())
}

func TestDiffTrackerResetOnTurn(t *testing.T) {
	dt := tools.NewDiffTracker()
	// Pre-populate with a change that should be cleared on new turn.
	dt.Record(tools.FileChange{Path: "old.go", Operation: tools.OpModified, Tool: "file"})
	require.Len(t, dt.Changes(), 1)

	p := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "hello"},
		{Type: "stop"},
	}}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(p, reg, autoApprove, cfg, WithDiffTracker(dt))

	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)
	for range ch {
	}

	// DiffTracker should have been reset at turn start.
	assert.Empty(t, dt.Changes())
}

func TestDiffSummaryInDoneEvent(t *testing.T) {
	tmpDir := t.TempDir()

	dt := tools.NewDiffTracker()
	fileTool := tools.NewFileTool(tmpDir)
	fileTool.SetDiffTracker(dt)

	// First call: LLM uses file tool to write
	// Second call: LLM responds with text
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_w1",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"write","path":"new.txt","content":"hello"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "wrote the file"},
				{Type: "stop"},
			},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fileTool))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithDiffTracker(dt))

	ch, err := a.Turn(context.Background(), "create new.txt")
	require.NoError(t, err)

	var doneEvent *TurnEvent
	for ev := range ch {
		if ev.Type == "done" {
			doneEvent = &ev
		}
	}

	require.NotNil(t, doneEvent, "should have a done event")
	assert.Contains(t, doneEvent.DiffSummary, "new.txt")
	assert.Contains(t, doneEvent.DiffSummary, "created")
}

func TestDiffSummaryEmptyWhenNoChanges(t *testing.T) {
	p := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "just text, no tools"},
		{Type: "stop"},
	}}

	dt := tools.NewDiffTracker()
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(p, reg, autoApprove, cfg, WithDiffTracker(dt))

	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var doneEvent *TurnEvent
	for ev := range ch {
		if ev.Type == "done" {
			doneEvent = &ev
		}
	}

	require.NotNil(t, doneEvent)
	assert.Empty(t, doneEvent.DiffSummary)
}

func TestNoDiffTrackerNoPanic(t *testing.T) {
	p := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "hello"},
		{Type: "stop"},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(p, reg, autoApprove, cfg) // No WithDiffTracker

	ch, err := a.Turn(context.Background(), "test")
	require.NoError(t, err)

	var doneEvent *TurnEvent
	for ev := range ch {
		if ev.Type == "done" {
			doneEvent = &ev
		}
	}

	require.NotNil(t, doneEvent)
	assert.Empty(t, doneEvent.DiffSummary)
}

func TestAgentWithWakeManager(t *testing.T) {
	wm := NewWakeManager()
	cfg := config.DefaultConfig()
	p := &mockProvider{}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg, WithWakeManager(wm))
	assert.NotNil(t, a.wakeManager)
	assert.Equal(t, wm, a.wakeManager)
}

func TestAgentDrainWakeEventsNilManager(t *testing.T) {
	cfg := config.DefaultConfig()
	p := &mockProvider{}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg)
	// drainWakeEvents should be a no-op when wakeManager is nil.
	ch := make(chan TurnEvent, 16)
	a.drainWakeEvents(context.Background(), ch)
	assert.Empty(t, ch)
}

func TestAgentDrainWakeEventsWithPending(t *testing.T) {
	wm := NewWakeManager()
	cfg := config.DefaultConfig()
	p := &mockProvider{}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg, WithWakeManager(wm))

	// Submit and complete two background tasks to populate wake events.
	_, cancel1 := context.WithCancel(context.Background())
	id1 := wm.Submit("agent1", cancel1)
	wm.Complete(id1, &SubagentResult{Name: "agent1", Output: "result1"})

	_, cancel2 := context.WithCancel(context.Background())
	id2 := wm.Submit("agent2", cancel2)
	wm.Complete(id2, &SubagentResult{Name: "agent2", Output: "result2"})

	ch := make(chan TurnEvent, 16)
	a.drainWakeEvents(context.Background(), ch)

	// Should have exactly 2 events drained.
	assert.Len(t, ch, 2)

	ev1 := <-ch
	assert.Equal(t, "subagent_done", ev1.Type)
	assert.NotNil(t, ev1.SubagentResult)
	assert.Contains(t, ev1.Text, "completed")

	ev2 := <-ch
	assert.Equal(t, "subagent_done", ev2.Type)
	assert.NotNil(t, ev2.SubagentResult)
}

func TestAgentDrainWakeEventsNoPending(t *testing.T) {
	wm := NewWakeManager()
	cfg := config.DefaultConfig()
	p := &mockProvider{}
	reg := tools.NewRegistry()

	a := New(p, reg, autoApprove, cfg, WithWakeManager(wm))

	ch := make(chan TurnEvent, 16)
	a.drainWakeEvents(context.Background(), ch)
	// No events should be emitted.
	assert.Empty(t, ch)
}

func TestAgentRunLoopDrainsWakeAfterTools(t *testing.T) {
	// Simulate: LLM returns a tool call -> tool executes -> wake event is drained.
	wm := NewWakeManager()

	// A mock tool that, when executed, completes a background task on the
	// wake manager so there's a pending event to drain.
	var triggered sync.Once
	triggerTool := &mockTool{
		name:        "trigger",
		description: "triggers a wake event",
		inputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			triggered.Do(func() {
				_, cancel := context.WithCancel(context.Background())
				taskID := wm.Submit("bg-agent", cancel)
				wm.Complete(taskID, &SubagentResult{Name: "bg-agent", Output: "background done"})
			})
			return tools.ToolResult{Content: "triggered"}, nil
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(triggerTool))

	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				// First response: tool use
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "trigger"}},
				{Type: "text_delta", Text: `{}`},
				{Type: "stop"},
			},
			{
				// Second response: text after seeing tool result + wake message
				{Type: "text_delta", Text: "All done."},
				{Type: "stop"},
			},
		},
	}

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithWakeManager(wm))

	ch, err := a.Turn(context.Background(), "please trigger")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Find the subagent_done event.
	var foundWake bool
	for _, ev := range events {
		if ev.Type == "subagent_done" {
			foundWake = true
			assert.Contains(t, ev.Text, "bg-agent")
			assert.Contains(t, ev.Text, "background done")
			assert.NotNil(t, ev.SubagentResult)
			assert.Equal(t, "bg-agent", ev.SubagentResult.Name)
		}
	}
	assert.True(t, foundWake, "should have a subagent_done event from wake manager drain")
}

func TestSnapshotSavedAfterToolResults(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := config.DefaultConfig()
	cfg.Agent.ContextBudget = 100000

	// LLM response: tool call → text reply (two turns).
	p := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		// Turn 1: call a tool.
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "echo"}},
			{Type: "text_delta", Text: `{"msg":"hi"}`},
			{Type: "stop"},
		},
		// Turn 2: text reply after tool result.
		{
			{Type: "text_delta", Text: "All done"},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&mockTool{
		name:        "echo",
		description: "echo tool",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Content: "echoed"}, nil
		},
	}))

	a := New(p, reg, autoApprove, cfg, WithStore(s))
	ch, err := a.Turn(context.Background(), "run echo")
	require.NoError(t, err)
	for range ch {
	}

	// After the turn, the snapshot must contain the tool result message.
	snap, err := s.GetSnapshot(a.sessionID)
	require.NoError(t, err)
	require.NotNil(t, snap)

	// The snapshot should include: user, assistant(tool_use), tool_result, assistant(text).
	// At minimum, it must include the tool_result so a resume is deterministic.
	var foundToolResult bool
	for _, msg := range snap {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				foundToolResult = true
			}
		}
	}
	assert.True(t, foundToolResult, "snapshot must include tool result for deterministic resume")
}

func TestWakeEventsPersisted(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := config.DefaultConfig()
	cfg.Agent.ContextBudget = 100000

	wm := NewWakeManager()

	// LLM response: tool call that completes, then another text response.
	p := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		// Turn 1: call a tool (wake event will be drained after tool execution).
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "slow"}},
			{Type: "text_delta", Text: `{}`},
			{Type: "stop"},
		},
		// Turn 2: text reply.
		{
			{Type: "text_delta", Text: "background noticed"},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(&mockTool{
		name:        "slow",
		description: "slow tool",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			// Simulate a background task completing while this tool runs.
			wm.Complete("bg-42", &SubagentResult{
				Name:   "helper",
				Output: "background work done",
			})
			return tools.ToolResult{Content: "done"}, nil
		},
	}))

	// Submit a fake background task so Complete can find it.
	wm.Submit("helper", func() {})
	// Overwrite pending to use our known ID.
	wm.mu.Lock()
	delete(wm.pending, func() string {
		for k := range wm.pending {
			return k
		}
		return ""
	}())
	wm.pending["bg-42"] = &backgroundTask{id: "bg-42", agentName: "helper", cancel: func() {}}
	wm.mu.Unlock()

	a := New(p, reg, autoApprove, cfg, WithStore(s), WithWakeManager(wm))
	ch, err := a.Turn(context.Background(), "do something")
	require.NoError(t, err)
	for range ch {
	}

	// Wake events must be persisted in the message log.
	msgs, err := s.GetMessages(a.sessionID)
	require.NoError(t, err)

	var foundWakeMsg bool
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if strings.Contains(block.Text, "background work done") {
				foundWakeMsg = true
			}
		}
	}
	assert.True(t, foundWakeMsg, "wake events must be persisted to message log for deterministic resume")
}

func TestTurnPanicRecovery(t *testing.T) {
	// A tool that panics should be caught at the tool level (not the Turn
	// level), producing an error tool_result so the LLM can continue.
	// The turn should complete normally with a "done" event.
	mp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			// First LLM call: requests the panicking tool.
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "call_1", Name: "panicker"}},
				{Type: "stop"},
			},
			// Second LLM call: after receiving error tool_result, LLM responds normally.
			{
				{Type: "text_delta", Text: "I see the tool failed"},
				{Type: "stop"},
			},
		},
	}
	reg := tools.NewRegistry()
	panicTool := &mockTool{
		name:        "panicker",
		description: "always panics",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			panic("deliberate test panic")
		},
	}
	require.NoError(t, reg.Register(panicTool))
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "trigger panic")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Panic is caught at tool level: expect error tool_result, not Turn-level "agent panic".
	var foundToolResult, foundDone bool
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.ToolResult != nil && ev.ToolResult.IsError {
			foundToolResult = true
			assert.Contains(t, ev.ToolResult.Content, "panic", "tool_result should mention panic")
		}
		if ev.Type == "done" {
			foundDone = true
		}
	}
	assert.True(t, foundToolResult, "expected error tool_result from panicking tool, got events: %v", events)
	assert.True(t, foundDone, "expected done event after tool-level panic recovery")
}

func TestToolPanicAddsErrorToolResult(t *testing.T) {
	// When a tool panics, executeSingleTool should recover and return an error
	// tool_result so that the conversation isn't left with a dangling tool_use.
	// The turn should complete normally (with a done event) rather than via
	// the Turn-level panic recovery.
	mp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			// First LLM call: requests the panicking tool.
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "call_panic", Name: "panicker"}},
				{Type: "stop"},
			},
			// Second LLM call: after receiving error tool_result, LLM responds with text.
			{
				{Type: "text_delta", Text: "recovered"},
				{Type: "stop"},
			},
		},
	}
	reg := tools.NewRegistry()
	panicTool := &mockTool{
		name:        "panicker",
		description: "always panics",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			panic("deliberate tool panic")
		},
	}
	require.NoError(t, reg.Register(panicTool))
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "trigger panic")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have a tool_result event with isError=true for the panicking tool.
	var foundToolResult, foundDone bool
	var foundTurnLevelPanic bool
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.ToolResult != nil && ev.ToolResult.ID == "call_panic" {
			foundToolResult = true
			assert.True(t, ev.ToolResult.IsError, "tool_result should be an error")
			assert.Contains(t, ev.ToolResult.Content, "panic", "should mention panic")
		}
		if ev.Type == "done" {
			foundDone = true
		}
		// If we see a Turn-level panic error, the recovery is at the wrong level.
		if ev.Type == "error" && ev.Error != nil && strings.Contains(ev.Error.Error(), "agent panic") {
			foundTurnLevelPanic = true
		}
	}
	assert.True(t, foundToolResult, "expected error tool_result for panicking tool, got events: %v", events)
	assert.True(t, foundDone, "expected done event")
	assert.False(t, foundTurnLevelPanic, "panic should be caught at tool level, not Turn level")

	// Conversation should have a tool_result matching the tool_use.
	msgs := agent.conversation.Messages()
	// user → assistant(tool_use) → tool_result → assistant(text)
	require.GreaterOrEqual(t, len(msgs), 3, "expected at least user + assistant + tool_result")
	// The tool_result message should be present (role=user with tool_result content).
	foundToolResultInConvo := false
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID == "call_panic" {
				foundToolResultInConvo = true
				assert.True(t, block.IsError, "conversation tool_result should be error")
			}
		}
	}
	assert.True(t, foundToolResultInConvo, "conversation must have error tool_result to avoid corruption")
}

func TestTurnNilToolUseEvent(t *testing.T) {
	// A provider sending tool_use with nil ToolUse should produce an error
	// event, not crash.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "hi"},
			{Type: "tool_use", ToolUse: nil}, // malformed event
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "test nil tool")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have an error event about nil ToolUse and eventually a done.
	var foundNilError, foundDone bool
	for _, ev := range events {
		if ev.Type == "error" && ev.Error != nil && strings.Contains(ev.Error.Error(), "nil ToolUse") {
			foundNilError = true
		}
		if ev.Type == "done" {
			foundDone = true
		}
	}
	assert.True(t, foundNilError, "expected error event for nil ToolUse")
	assert.True(t, foundDone, "expected done event")
}

func TestClearConversationRaceSafety(t *testing.T) {
	// ClearConversation must be safe to call concurrently with a running Turn.
	// This test exercises the turnMu locking added to ClearConversation.
	// Run with -race to verify.
	gate := make(chan struct{})
	mp := &channelProvider{
		events: func() <-chan provider.StreamEvent {
			ch := make(chan provider.StreamEvent, 10)
			go func() {
				<-gate // wait until ClearConversation is called
				ch <- provider.StreamEvent{Type: "text_delta", Text: "hello"}
				ch <- provider.StreamEvent{Type: "stop"}
				close(ch)
			}()
			return ch
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "test race")
	require.NoError(t, err)

	// Call ClearConversation while Turn goroutine is active.
	// With turnMu, this blocks until the Turn finishes — no race.
	go func() {
		close(gate) // let the provider proceed
		agent.ClearConversation()
	}()

	// Drain the channel to let Turn complete.
	for range ch {
	}
}

// channelProvider returns events from a channel factory, allowing
// synchronization control in tests.
type channelProvider struct {
	events func() <-chan provider.StreamEvent
}

func (p *channelProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	return p.events(), nil
}

func TestAgentCheckpointMethods(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "ok"},
		{Type: "stop"},
	}}

	t.Run("no manager returns errors", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)

		_, err := a.Undo(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not configured")

		_, err = a.RewindToTurn(context.Background(), 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not configured")

		assert.Nil(t, a.Checkpoints())
	})

	t.Run("with manager", func(t *testing.T) {
		rootDir := t.TempDir()
		mgr, err := checkpoint.New(rootDir, "test-session", 0)
		require.NoError(t, err)
		defer func() { _ = mgr.Cleanup() }()

		a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithCheckpointManager(mgr))
		assert.NotNil(t, a.checkpointMgr)

		// Empty stack returns no checkpoints.
		cps := a.Checkpoints()
		assert.Empty(t, cps)

		// Undo on empty stack returns ErrNoCheckpoints.
		_, err = a.Undo(context.Background())
		assert.ErrorIs(t, err, checkpoint.ErrNoCheckpoints)
	})
}

func TestAgentForkSession(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "ok"}, {Type: "stop"},
	}}

	t.Run("no store returns error", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)
		_, err := a.ForkSession(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "store not configured")
	})

	t.Run("with store forks session and returns new id", func(t *testing.T) {
		s, err := store.NewStore(":memory:")
		require.NoError(t, err)
		defer s.Close()

		a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
		origID := a.SessionID()
		require.NotEmpty(t, origID)

		newID, err := a.ForkSession(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, newID)
		assert.NotEqual(t, origID, newID)
		assert.Equal(t, newID, a.SessionID())
	})
}

func TestAgentContextBudget(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "ok"},
		{Type: "stop"},
	}}

	t.Run("returns budget", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)
		budget := a.ContextBudget()
		assert.Equal(t, 100000, budget.Total)
	})

	t.Run("force compact returns result", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)
		result, err := a.ForceCompact(context.Background())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.BeforeTokens, 0)
	})
}

func TestLoadBootstrapContext(t *testing.T) {
	tmpDir := t.TempDir()
	bootstrapPath := filepath.Join(tmpDir, ".bootstrap.json")

	// Create bootstrap.json
	bootstrapData := `{
		"profile": {"project_name": "testapp"},
		"created_entities": ["entity1", "entity2"],
		"analysis_metadata": {"modules_found": 3}
	}`
	err := os.WriteFile(bootstrapPath, []byte(bootstrapData), 0o644)
	require.NoError(t, err)

	ctx, err := LoadBootstrapContext(bootstrapPath)
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.Equal(t, "testapp", ctx.Profile.ProjectName)
	assert.Equal(t, 2, len(ctx.CreatedEntities))
}

func TestBuildBootstrapSystemPromptPrefix(t *testing.T) {
	metadata := &knowledgegraph.BootstrapMetadata{
		Profile: knowledgegraph.BootstrapProfile{
			ProjectName: "myapp",
		},
		CreatedEntities: []string{"entity1", "entity2", "entity3"},
		AnalysisMetadata: knowledgegraph.AnalysisMetadata{
			ModulesFound:         5,
			GitCommitsAnalyzed:   30,
			IntegrationsDetected: 12,
		},
	}

	prefix := BuildBootstrapSystemPromptPrefix(metadata)
	assert.NotEmpty(t, prefix)
	assert.Contains(t, prefix, "myapp")
	assert.Contains(t, prefix, "5 modules")
	assert.Contains(t, prefix, "30")
	assert.Contains(t, prefix, "12 integrations")
}

func TestBuildBootstrapSystemPromptPrefixNil(t *testing.T) {
	prefix := BuildBootstrapSystemPromptPrefix(nil)
	assert.Empty(t, prefix)
}

func TestTurnEmitsExitReasonCompleted(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "done."},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	ag := New(mp, reg, autoApprove, cfg)

	ch, err := ag.Turn(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	var last TurnEvent
	for ev := range ch {
		last = ev
	}
	if last.Type != "done" {
		t.Fatalf("want last event type=done, got %q", last.Type)
	}
	if last.ExitReason != agentsdk.ExitCompleted {
		t.Fatalf("want ExitCompleted, got %v", last.ExitReason)
	}
}

// TestTurnUnblocksWhenConsumerCancelsCtx verifies that a consumer
// which stops reading the Turn channel and cancels its context does
// NOT deadlock the agent goroutine. Previously, every ch<-ev in
// runLoop and its callees was a synchronous send; once the 64-slot
// buffer filled, turnMu was held forever. The fix wraps sends in a
// select that also listens on ctx.Done().
//
// Test strategy: emit enough events to overflow the 64-slot turn
// channel. Consumer does NOT read. Cancel ctx. Then try a SECOND
// Turn on the same agent — turnMu.Lock() in the second call blocks
// until the first goroutine releases, so if the first hung, the
// second Turn never returns. 3s timeout detects the deadlock class.
func TestTurnUnblocksWhenConsumerCancelsCtx(t *testing.T) {
	t.Parallel()
	// 200 text_delta events + stop — guaranteed to overflow the
	// 64-slot turn channel buffer.
	events := make([]provider.StreamEvent, 0, 201)
	for i := 0; i < 200; i++ {
		events = append(events, provider.StreamEvent{Type: "text_delta", Text: "x"})
	}
	events = append(events, provider.StreamEvent{Type: "stop"})
	mp := &mockProvider{events: events}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	ag := New(mp, reg, autoApprove, cfg)

	ctx1, cancel1 := context.WithCancel(context.Background())
	ch1, err := ag.Turn(ctx1, "hello")
	require.NoError(t, err)
	_ = ch1 // intentionally never read — simulates a consumer that disconnected

	// Give the agent goroutine a moment to fill the buffer and block
	// on the next send. 50ms is comfortable — a non-blocked loop of
	// 200 trivial events finishes in microseconds.
	time.Sleep(50 * time.Millisecond)

	// Cancel the context. The fix causes every blocked send to unblock
	// via the ctx.Done() branch; runLoop then exits and the deferred
	// turnMu.Unlock() fires.
	cancel1()

	// Start a second turn. If the first goroutine is still holding
	// turnMu (deadlock), this blocks forever.
	secondTurn := make(chan error, 1)
	go func() {
		mp2 := &mockProvider{events: []provider.StreamEvent{
			{Type: "text_delta", Text: "ok"},
			{Type: "stop"},
		}}
		// Replace the provider so the second turn doesn't re-read
		// the exhausted mockProvider (its events slice is stateful).
		// We can't SetProvider mid-agent, so just drive a fresh turn
		// through a fresh agent that shares the same turnMu contract —
		// but what we actually need is to prove the FIRST agent's
		// goroutine exited. Use the same agent and the same mp
		// (which no longer has events — fine, a fresh Stream() call
		// returns an empty closed channel).
		_ = mp2
		ch2, err := ag.Turn(context.Background(), "second")
		if err != nil {
			secondTurn <- err
			return
		}
		// Drain fully so the second turn's goroutine exits cleanly.
		for range ch2 {
		}
		secondTurn <- nil
	}()
	select {
	case err := <-secondTurn:
		if err != nil {
			t.Fatalf("second Turn returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second Turn blocked on turnMu — first goroutine never released the lock (deadlock on blocked ch send)")
	}
}
