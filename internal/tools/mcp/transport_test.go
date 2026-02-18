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

func TestStdioTransportClose(t *testing.T) {
	transport, err := NewStdioTransport("cat", nil)
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)
}
