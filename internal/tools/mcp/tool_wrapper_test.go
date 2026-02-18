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
