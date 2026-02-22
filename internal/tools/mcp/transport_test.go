//go:build !windows

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdioTransportSendReceive(t *testing.T) {
	// Use "cat" as a simple echo program — what we send to stdin comes back on stdout.
	transport, err := NewStdioTransport("cat", nil)
	require.NoError(t, err)
	defer transport.Close()

	msg := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
	}

	err = transport.Send(context.Background(), msg)
	require.NoError(t, err)

	var resp json.RawMessage
	err = transport.Receive(context.Background(), &resp)
	require.NoError(t, err)
	assert.Contains(t, string(resp), `"method":"test"`)
}

func TestStdioTransportReceiveRespectsContext(t *testing.T) {
	// Use "cat" without writing to stdin so stdout blocks — Receive should
	// return when context is cancelled rather than blocking forever.
	transport, err := NewStdioTransport("cat", nil)
	require.NoError(t, err)
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var resp json.RawMessage
	err = transport.Receive(ctx, &resp)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestStdioTransportClose(t *testing.T) {
	transport, err := NewStdioTransport("cat", nil)
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)
}

func TestStdioTransportBadCommand(t *testing.T) {
	_, err := NewStdioTransport("/nonexistent/binary/xyz", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start process")
}

func TestStdioTransportSendContextCancellation(t *testing.T) {
	// Use "sleep" so stdin blocks on write.
	transport, err := NewStdioTransport("sleep", []string{"60"})
	require.NoError(t, err)
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = transport.Send(ctx, jsonRPCRequest{JSONRPC: "2.0", ID: 1, Method: "test"})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestStdioTransportReceiveUnmarshalError(t *testing.T) {
	// Use "echo" to write a non-JSON line to stdout.
	transport, err := NewStdioTransport("echo", []string{"not json"})
	require.NoError(t, err)
	defer transport.Close()

	var resp json.RawMessage
	err = transport.Receive(context.Background(), &resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal response")
}

func TestStdioTransportCloseIdempotent(t *testing.T) {
	transport, err := NewStdioTransport("cat", nil)
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)

	// Second close should not panic or error.
	err = transport.Close()
	assert.NoError(t, err)
}
