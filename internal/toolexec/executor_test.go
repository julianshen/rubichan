package toolexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
)

// stubTool implements tools.Tool for testing.
type stubTool struct {
	name          string
	description   string
	schema        json.RawMessage
	execResult    tools.ToolResult
	execErr       error
	execCalledCtx context.Context
	execInput     json.RawMessage
}

func (s *stubTool) Name() string                 { return s.name }
func (s *stubTool) Description() string          { return s.description }
func (s *stubTool) InputSchema() json.RawMessage { return s.schema }
func (s *stubTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	s.execCalledCtx = ctx
	s.execInput = input
	return s.execResult, s.execErr
}

type streamingStubTool struct {
	stubTool
	streamEvents []tools.ToolEvent
	streamCalled bool
}

func (s *streamingStubTool) ExecuteStream(_ context.Context, _ json.RawMessage, emit tools.ToolEventEmitter) (tools.ToolResult, error) {
	s.streamCalled = true
	for _, ev := range s.streamEvents {
		emit(ev)
	}
	return s.execResult, s.execErr
}

func TestRegistryExecutorCallsTool(t *testing.T) {
	stub := &stubTool{
		name:        "read_file",
		description: "reads a file",
		schema:      json.RawMessage(`{}`),
		execResult: tools.ToolResult{
			Content:        "file contents",
			DisplayContent: "Read /tmp/test.go",
			IsError:        false,
		},
	}

	registry := tools.NewRegistry()
	err := registry.Register(stub)
	assert.NoError(t, err)

	handler := toolexec.RegistryExecutor(registry)
	input := json.RawMessage(`{"path":"/tmp/test.go"}`)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "call-1",
		Name:  "read_file",
		Input: input,
	})

	assert.Equal(t, "file contents", result.Content)
	assert.Equal(t, "Read /tmp/test.go", result.DisplayContent)
	assert.False(t, result.IsError)
	assert.JSONEq(t, `{"path":"/tmp/test.go"}`, string(stub.execInput))
}

func TestRegistryExecutorUnknownTool(t *testing.T) {
	registry := tools.NewRegistry()

	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-2",
		Name: "nonexistent_tool",
	})

	assert.Contains(t, result.Content, "unknown tool: nonexistent_tool")
	assert.True(t, result.IsError)
}

func TestRegistryExecutorUnknownToolWithSuggestion(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(&stubTool{name: "shell", schema: json.RawMessage(`{}`)})
	_ = registry.Register(&stubTool{name: "file", schema: json.RawMessage(`{}`)})

	handler := toolexec.RegistryExecutor(registry)

	// "shell_exec" contains "shell" — should suggest "shell".
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-suggest",
		Name: "shell_exec",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, `Did you mean "shell"`)
	assert.Contains(t, result.Content, "Available tools:")
}

func TestRegistryExecutorUnknownToolNoSuggestion(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(&stubTool{name: "shell", schema: json.RawMessage(`{}`)})

	handler := toolexec.RegistryExecutor(registry)

	// "foobar" has no similarity to "shell" — no suggestion.
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-no-suggest",
		Name: "foobar",
	})
	assert.True(t, result.IsError)
	assert.Equal(t, "unknown tool: foobar", result.Content)
}

func TestRegistryExecutorToolError(t *testing.T) {
	stub := &stubTool{
		name:    "write_file",
		schema:  json.RawMessage(`{}`),
		execErr: errors.New("permission denied"),
	}

	registry := tools.NewRegistry()
	err := registry.Register(stub)
	assert.NoError(t, err)

	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "call-3",
		Name:  "write_file",
		Input: json.RawMessage(`{}`),
	})

	assert.Equal(t, "tool execution error: permission denied", result.Content)
	assert.True(t, result.IsError)
}

func TestRegistryExecutorStreamingToolEmitsEvents(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "shell",
			schema: json.RawMessage(`{}`),
			execResult: tools.ToolResult{
				Content: "done",
			},
		},
		streamEvents: []tools.ToolEvent{
			{Stage: tools.EventBegin, Content: "start"},
			{Stage: tools.EventDelta, Content: "chunk"},
			{Stage: tools.EventEnd, Content: "end"},
		},
	}
	registry := tools.NewRegistry()
	err := registry.Register(stub)
	assert.NoError(t, err)

	handler := toolexec.RegistryExecutor(registry)
	var got []tools.ToolEvent
	ctx := toolexec.WithToolEventEmitter(context.Background(), func(ev tools.ToolEvent) {
		got = append(got, ev)
	})
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-stream",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	})

	assert.Equal(t, "done", result.Content)
	assert.Len(t, got, 3)
	assert.Equal(t, tools.EventBegin, got[0].Stage)
	assert.Equal(t, "chunk", got[1].Content)
	assert.Equal(t, tools.EventEnd, got[2].Stage)
}

func TestRegistryExecutorStreamingToolFallsBackWithoutEmitter(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "shell",
			schema: json.RawMessage(`{}`),
			execResult: tools.ToolResult{
				Content: "done",
			},
		},
		streamEvents: []tools.ToolEvent{
			{Stage: tools.EventBegin, Content: "start"},
		},
	}
	registry := tools.NewRegistry()
	err := registry.Register(stub)
	assert.NoError(t, err)

	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "call-fallback",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	})

	assert.Equal(t, "done", result.Content)
	assert.False(t, stub.streamCalled)
	assert.JSONEq(t, `{"command":"echo hi"}`, string(stub.execInput))
}
