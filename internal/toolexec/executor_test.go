package toolexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Equal(t, "unknown tool: nonexistent_tool", result.Content)
	assert.True(t, result.IsError)
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

// streamingStubTool implements both tools.Tool and tools.StreamingTool.
type streamingStubTool struct {
	stubTool
	streamResult tools.ToolResult
	streamErr    error
}

func (s *streamingStubTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit func(tools.ToolEvent)) (tools.ToolResult, error) {
	s.execInput = input
	emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "begin"})
	emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "output line 1\n"})
	emit(tools.ToolEvent{Stage: tools.EventEnd, Content: "end"})
	return s.streamResult, s.streamErr
}

func TestRegistryExecutorUsesStreamingToolWhenEmitterPresent(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "streaming_shell",
			schema: json.RawMessage(`{}`),
		},
		streamResult: tools.ToolResult{Content: "streamed result"},
	}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(stub))

	var received []tools.ToolEvent
	emit := func(ev tools.ToolEvent) {
		received = append(received, ev)
	}

	ctx := tools.WithEmitter(context.Background(), emit)
	handler := toolexec.RegistryExecutor(registry)
	result := handler(ctx, toolexec.ToolCall{
		ID: "call-s1", Name: "streaming_shell",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	})

	assert.Equal(t, "streamed result", result.Content)
	assert.False(t, result.IsError)
	require.Len(t, received, 3)
	assert.Equal(t, tools.EventBegin, received[0].Stage)
	assert.Equal(t, tools.EventDelta, received[1].Stage)
	assert.Equal(t, tools.EventEnd, received[2].Stage)
}

func TestRegistryExecutorStreamingToolFallsBackWithoutEmitter(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "streaming_shell",
			schema: json.RawMessage(`{}`),
		},
		streamResult: tools.ToolResult{Content: "streamed result"},
	}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(stub))

	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID: "call-s2", Name: "streaming_shell",
		Input: json.RawMessage(`{}`),
	})

	assert.Equal(t, "streamed result", result.Content)
	assert.False(t, result.IsError)
}

func TestRegistryExecutorStreamingToolError(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "streaming_shell",
			schema: json.RawMessage(`{}`),
		},
		streamErr: errors.New("stream failed"),
	}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(stub))

	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID: "call-s3", Name: "streaming_shell",
	})

	assert.Contains(t, result.Content, "stream failed")
	assert.True(t, result.IsError)
}
