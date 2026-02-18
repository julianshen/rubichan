package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport is a test transport that records sent messages and returns canned responses.
type mockTransport struct {
	sent      []json.RawMessage
	responses []json.RawMessage
	idx       int
}

func (m *mockTransport) Send(_ context.Context, msg any) error {
	data, _ := json.Marshal(msg)
	m.sent = append(m.sent, data)
	return nil
}

func (m *mockTransport) Receive(_ context.Context, result any) error {
	if m.idx >= len(m.responses) {
		return nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return json.Unmarshal(resp, result)
}

func (m *mockTransport) Close() error { return nil }

func TestClientInitialize(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"test-server","version":"1.0"}}}`),
		},
	}

	client := NewClient("test-server", mt)
	err := client.Initialize(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-server", client.ServerName())

	// Verify notifications/initialized was sent after handshake.
	require.Len(t, mt.sent, 2)
	var notification map[string]any
	require.NoError(t, json.Unmarshal(mt.sent[1], &notification))
	assert.Equal(t, "notifications/initialized", notification["method"])
	assert.Nil(t, notification["id"], "notification should not have an id")
}

func TestClientListTools(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"read_file","description":"Read a file","inputSchema":{"type":"object","properties":{"path":{"type":"string"}}}}]}}`),
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "read_file", tools[0].Name)
	assert.Equal(t, "Read a file", tools[0].Description)
}

func TestClientCallTool(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"file contents here"}]}}`),
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	result, err := client.CallTool(context.Background(), "read_file", map[string]any{"path": "/tmp/test.txt"})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "file contents here", result.Content[0].Text)
}

func TestClientCallToolError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Invalid params"}}`),
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.CallTool(context.Background(), "bad_tool", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid params")
}
