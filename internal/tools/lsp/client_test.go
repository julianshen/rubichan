package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport is a bidirectional pipe for testing the JSON-RPC client.
type mockTransport struct {
	client io.ReadWriteCloser
	server io.ReadWriteCloser
}

func newMockTransport() *mockTransport {
	c, s := net.Pipe()
	return &mockTransport{client: c, server: s}
}

// writeResponse writes a JSON-RPC response with Content-Length framing to the server side.
func writeResponse(w io.Writer, id int64, result any) error {
	resp := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Result  any    `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result}
	body, _ := json.Marshal(resp)
	_, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

// writeNotification writes a JSON-RPC notification with Content-Length framing.
func writeNotification(w io.Writer, method string, params any) error {
	notif := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", Method: method, Params: params}
	body, _ := json.Marshal(notif)
	_, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

// readRequest reads one JSON-RPC request from the server side.
func readRequest(r io.Reader) (jsonrpcRequest, error) {
	// Read Content-Length header.
	var header string
	buf := make([]byte, 1)
	for {
		_, err := r.Read(buf)
		if err != nil {
			return jsonrpcRequest{}, err
		}
		header += string(buf)
		if len(header) >= 4 && header[len(header)-4:] == "\r\n\r\n" {
			break
		}
	}

	var contentLength int
	if _, err := fmt.Sscanf(header, "Content-Length: %d", &contentLength); err != nil {
		return jsonrpcRequest{}, fmt.Errorf("parse header: %w", err)
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return jsonrpcRequest{}, err
	}

	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonrpcRequest{}, err
	}
	return req, nil
}

func TestClientCallSuccess(t *testing.T) {
	mt := newMockTransport()
	defer mt.client.Close()
	defer mt.server.Close()

	client := NewClient(mt.client, nil)
	defer client.Close()

	// Server goroutine: read request, send response.
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, map[string]string{"status": "ok"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := client.Call(ctx, "test/method", map[string]string{"key": "value"})
	require.NoError(t, err)

	var got map[string]string
	require.NoError(t, json.Unmarshal(result, &got))
	assert.Equal(t, "ok", got["status"])
}

func TestClientCallError(t *testing.T) {
	mt := newMockTransport()
	defer mt.client.Close()
	defer mt.server.Close()

	client := NewClient(mt.client, nil)
	defer client.Close()

	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		// Send error response.
		resp := struct {
			JSONRPC string       `json:"jsonrpc"`
			ID      int64        `json:"id"`
			Error   jsonrpcError `json:"error"`
		}{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   jsonrpcError{Code: -32600, Message: "invalid request"},
		}
		body, _ := json.Marshal(resp)
		fmt.Fprintf(mt.server, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "test/error", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request")
}

func TestClientCallContextCancellation(t *testing.T) {
	mt := newMockTransport()
	defer mt.server.Close()

	client := NewClient(mt.client, nil)

	// Server reads the request but never responds.
	go func() {
		_, _ = readRequest(mt.server)
		// Intentionally don't respond — let the client timeout.
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Call(ctx, "test/slow", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Close client (which closes mt.client) to stop the read loop.
	client.Close()
}

func TestClientNotify(t *testing.T) {
	mt := newMockTransport()
	defer mt.client.Close()
	defer mt.server.Close()

	client := NewClient(mt.client, nil)
	defer client.Close()

	// Read the notification on the server side.
	done := make(chan jsonrpcRequest)
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		done <- req
	}()

	err := client.Notify(context.Background(), "textDocument/didOpen", map[string]string{"uri": "file:///test.go"})
	require.NoError(t, err)

	select {
	case req := <-done:
		assert.Equal(t, "textDocument/didOpen", req.Method)
		assert.Equal(t, int64(0), req.ID) // notifications have no ID
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for notification")
	}
}

func TestClientNotificationHandler(t *testing.T) {
	mt := newMockTransport()
	defer mt.client.Close()
	defer mt.server.Close()

	var receivedMethod string
	var receivedParams json.RawMessage
	var mu sync.Mutex
	notifReceived := make(chan struct{})

	handler := func(method string, params json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		receivedMethod = method
		receivedParams = params
		close(notifReceived)
	}

	client := NewClient(mt.client, handler)
	defer client.Close()

	// Server sends a notification.
	go func() {
		_ = writeNotification(mt.server, "textDocument/publishDiagnostics", PublishDiagnosticsParams{
			URI: "file:///test.go",
			Diagnostics: []Diagnostic{
				{Message: "undefined: foo", Severity: SeverityError},
			},
		})
	}()

	select {
	case <-notifReceived:
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "textDocument/publishDiagnostics", receivedMethod)
		assert.NotNil(t, receivedParams)

		var params PublishDiagnosticsParams
		require.NoError(t, json.Unmarshal(receivedParams, &params))
		assert.Equal(t, "file:///test.go", params.URI)
		require.Len(t, params.Diagnostics, 1)
		assert.Equal(t, "undefined: foo", params.Diagnostics[0].Message)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for notification")
	}
}

func TestClientConcurrentCalls(t *testing.T) {
	mt := newMockTransport()
	defer mt.client.Close()
	defer mt.server.Close()

	client := NewClient(mt.client, nil)
	defer client.Close()

	// Server goroutine: respond to multiple requests.
	go func() {
		for i := 0; i < 3; i++ {
			req, err := readRequest(mt.server)
			if err != nil {
				return
			}
			_ = writeResponse(mt.server, req.ID, map[string]int64{"id": req.ID})
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := client.Call(ctx, "test/concurrent", nil)
			require.NoError(t, err)
			assert.NotNil(t, result)
		}()
	}
	wg.Wait()
}

func TestClientClose(t *testing.T) {
	mt := newMockTransport()
	defer mt.server.Close()

	client := NewClient(mt.client, nil)

	// Close should be idempotent.
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())

	// Calls after close should fail fast.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := client.Call(ctx, "test/after-close", nil)
	require.Error(t, err)
}
