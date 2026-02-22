package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		return io.EOF
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

func TestClientSkipsNotifications(t *testing.T) {
	// A notification (no ID) arrives between the request and the response.
	mt := &mockTransport{
		responses: []json.RawMessage{
			// Initialize response
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			// Server-sent notification (no ID) — should be skipped
			json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":50}}`),
			// Actual tools/list response
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"my_tool","description":"A tool","inputSchema":{"type":"object"}}]}}`),
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "my_tool", tools[0].Name)
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

func TestClientClose(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient("test", mt)
	err := client.Close()
	assert.NoError(t, err)
}

func TestClientInitializeSendError(t *testing.T) {
	mt := &errorTransport{sendErr: fmt.Errorf("connection refused")}
	client := NewClient("test", mt)
	err := client.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send initialize")
}

func TestClientInitializeProtocolError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad protocol"}}`),
		},
	}
	client := NewClient("test", mt)
	err := client.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad protocol")
}

func TestClientInitializeParseError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":"not an object"}`),
		},
	}
	client := NewClient("test", mt)
	err := client.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse initialize result")
}

func TestClientInitializeNotificationSendError(t *testing.T) {
	// Send succeeds for initialize, fails for notifications/initialized.
	mt := &countingErrorTransport{
		failAfter: 1,
		sendErr:   fmt.Errorf("pipe broken"),
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
		},
	}
	client := NewClient("test", mt)
	err := client.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send notifications/initialized")
}

func TestClientListToolsSendError(t *testing.T) {
	mt := &countingErrorTransport{
		failAfter: 2, // Initialize sends 2 messages (init + notification)
		sendErr:   fmt.Errorf("broken pipe"),
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
		},
	}
	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.ListTools(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send tools/list")
}

func TestClientListToolsParseError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":"not an object"}`),
		},
	}
	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.ListTools(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse tools/list result")
}

func TestClientCallToolSendError(t *testing.T) {
	mt := &countingErrorTransport{
		failAfter: 2,
		sendErr:   fmt.Errorf("broken pipe"),
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
		},
	}
	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.CallTool(context.Background(), "tool", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send tools/call")
}

func TestClientCallToolParseError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":"not an object"}`),
		},
	}
	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.CallTool(context.Background(), "tool", map[string]any{"key": "val"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse tools/call result")
}

func TestClientReceiveResponseEOF(t *testing.T) {
	// When the transport returns io.EOF (no more responses), receiveResponse
	// should propagate the error rather than looping forever.
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			// No response for the ListTools call — Receive returns io.EOF.
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.ListTools(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF)
}

func TestClientReceiveResponseSkipsNonMatchingID(t *testing.T) {
	// Send a response with a non-matching ID before the real one.
	mt := &mockTransport{
		responses: []json.RawMessage{
			// Initialize
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			// Stale response with non-matching ID (should be skipped)
			json.RawMessage(`{"jsonrpc":"2.0","id":999,"result":{"tools":[]}}`),
			// Real response
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"my_tool","description":"A tool","inputSchema":{"type":"object"}}]}}`),
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "my_tool", tools[0].Name)
}

func TestClientListToolsProtocolError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`),
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.ListTools(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method not found")
}

func TestClientInitializeReceiveError(t *testing.T) {
	// Transport that succeeds Send but fails Receive.
	mt := &mockTransport{
		responses: []json.RawMessage{}, // Empty — Receive returns EOF immediately
	}

	client := NewClient("test", mt)
	err := client.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receive initialize response")
}

func TestClientCallToolReceiveError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			// No response for CallTool — Receive returns EOF
		},
	}

	client := NewClient("test", mt)
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.CallTool(context.Background(), "tool", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receive tools/call response")
}

// errorTransport always returns an error on Send.
type errorTransport struct {
	sendErr error
}

func (e *errorTransport) Send(_ context.Context, _ any) error       { return e.sendErr }
func (e *errorTransport) Receive(_ context.Context, _ any) error    { return io.EOF }
func (e *errorTransport) Close() error                              { return nil }

// countingErrorTransport succeeds for the first N sends, then fails.
type countingErrorTransport struct {
	failAfter int
	sendErr   error
	count     int
	responses []json.RawMessage
	recvIdx   int
}

func (c *countingErrorTransport) Send(_ context.Context, _ any) error {
	c.count++
	if c.count > c.failAfter {
		return c.sendErr
	}
	return nil
}

func (c *countingErrorTransport) Receive(_ context.Context, result any) error {
	if c.recvIdx >= len(c.responses) {
		return io.EOF
	}
	resp := c.responses[c.recvIdx]
	c.recvIdx++
	return json.Unmarshal(resp, result)
}

func (c *countingErrorTransport) Close() error { return nil }
