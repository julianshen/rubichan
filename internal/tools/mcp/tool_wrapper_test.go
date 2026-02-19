package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrappedToolInterface(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"fs","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"hello world"}]}}`),
		},
	}

	client := NewClient("fs", mt)
	require.NoError(t, client.Initialize(context.Background()))

	mcpTool := MCPTool{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}

	wrapped := WrapTool("fs", client, mcpTool)

	// Verify it implements tools.Tool
	var _ tools.Tool = wrapped

	assert.Equal(t, "mcp_fs_read_file", wrapped.Name())
	assert.Equal(t, "Read a file", wrapped.Description())

	result, err := wrapped.Execute(context.Background(), json.RawMessage(`{"path":"/tmp/test.txt"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "hello world", result.Content)
}

func TestWrappedToolErrorResult(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fs","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"file not found"}],"isError":true}}`),
		},
	}

	client := NewClient("fs", mt)
	require.NoError(t, client.Initialize(context.Background()))

	wrapped := WrapTool("fs", client, MCPTool{Name: "read_file", Description: "Read"})

	result, err := wrapped.Execute(context.Background(), json.RawMessage(`{"path":"/missing"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, "file not found", result.Content)
}

func TestWrappedToolTransportError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fs","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Invalid params"}}`),
		},
	}

	client := NewClient("fs", mt)
	require.NoError(t, client.Initialize(context.Background()))

	wrapped := WrapTool("fs", client, MCPTool{Name: "bad_tool", Description: "Bad"})

	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mcp call")
	assert.Contains(t, err.Error(), "Invalid params")
}

func TestWrappedToolInputSchemaFallback(t *testing.T) {
	// When InputSchema is empty, a default {"type":"object"} should be returned.
	wrapped := WrapTool("fs", nil, MCPTool{
		Name:        "no_schema_tool",
		Description: "Tool without schema",
		InputSchema: nil,
	})

	schema := wrapped.InputSchema()
	assert.JSONEq(t, `{"type":"object"}`, string(schema))
}

func TestWrappedToolInputSchemaProvided(t *testing.T) {
	customSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	wrapped := WrapTool("fs", nil, MCPTool{
		Name:        "schema_tool",
		Description: "Tool with schema",
		InputSchema: customSchema,
	})

	schema := wrapped.InputSchema()
	assert.JSONEq(t, string(customSchema), string(schema))
}

func TestWrappedToolExecuteInvalidJSON(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fs","version":"1.0"}}}`),
		},
	}

	client := NewClient("fs", mt)
	require.NoError(t, client.Initialize(context.Background()))

	wrapped := WrapTool("fs", client, MCPTool{Name: "tool", Description: "A tool"})

	result, err := wrapped.Execute(context.Background(), json.RawMessage(`{invalid json`))
	require.NoError(t, err) // Invalid JSON returns ToolResult with IsError, not Go error
	assert.True(t, result.IsError)
}

func TestWrappedToolExecuteEmptyInput(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fs","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"ok"}]}}`),
		},
	}

	client := NewClient("fs", mt)
	require.NoError(t, client.Initialize(context.Background()))

	wrapped := WrapTool("fs", client, MCPTool{Name: "tool", Description: "A tool"})

	result, err := wrapped.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
}
