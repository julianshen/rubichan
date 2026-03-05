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
