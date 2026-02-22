package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSETransportSendReceive(t *testing.T) {
	responseCh := make(chan string, 1)

	mux := http.NewServeMux()

	// POST endpoint receives JSON-RPC requests
	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		// Queue a response to be sent via SSE
		responseCh <- `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
		w.WriteHeader(http.StatusAccepted)
	})

	// SSE endpoint streams responses
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)

		// Send the endpoint event first
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		flusher.Flush()

		// Wait for a response to send
		select {
		case resp := <-responseCh:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", resp)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}

		<-r.Context().Done()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, server.URL+"/sse")
	require.NoError(t, err)
	defer transport.Close()

	// Compile-time interface check
	var _ Transport = transport

	msg := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
	}

	err = transport.Send(ctx, msg)
	require.NoError(t, err)

	var resp jsonRPCResponse
	err = transport.Receive(ctx, &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Result)
}

func TestSSETransportClose(t *testing.T) {
	var mu sync.Mutex
	sseConnected := false

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		flusher.Flush()

		mu.Lock()
		sseConnected = true
		mu.Unlock()

		<-r.Context().Done()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, server.URL+"/sse")
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)

	mu.Lock()
	assert.True(t, sseConnected)
	mu.Unlock()
}

func TestSSETransportReceiveEOFOnStreamDrop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		flusher.Flush()

		// Close immediately — simulates stream drop.
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, server.URL+"/sse")
	require.NoError(t, err)
	defer transport.Close()

	// Receive should return io.EOF when the SSE stream drops, not block forever.
	var resp jsonRPCResponse
	err = transport.Receive(ctx, &resp)
	assert.ErrorIs(t, err, io.EOF)
}

func TestSSETransportStreamClosedBeforeEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Close immediately without sending endpoint event.
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := NewSSETransport(ctx, server.URL+"/sse")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSE stream closed before endpoint event")
}

func TestSSETransportContextCancelledDuringConnect(t *testing.T) {
	// Use a server that hangs before sending the endpoint event.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Hang — never send endpoint event.
		<-r.Context().Done()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := NewSSETransport(ctx, server.URL+"/sse")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSSETransportSendPostError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, server.URL+"/sse")
	require.NoError(t, err)
	defer transport.Close()

	err = transport.Send(ctx, jsonRPCRequest{JSONRPC: "2.0", ID: 1, Method: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POST returned 400")
}

func TestSSETransportReceiveContextDone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, server.URL+"/sse")
	require.NoError(t, err)
	defer transport.Close()

	// Cancel and then try to receive.
	recvCtx, recvCancel := context.WithCancel(context.Background())
	recvCancel()

	var resp jsonRPCResponse
	err = transport.Receive(recvCtx, &resp)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSSETransportSendConnectionError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Point the POST URL at a port that's definitely closed.
		fmt.Fprintf(w, "event: endpoint\ndata: http://127.0.0.1:1/message\n\n")
		flusher.Flush()
		<-r.Context().Done()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSSETransport(ctx, server.URL+"/sse")
	require.NoError(t, err)
	defer transport.Close()

	// Send should fail because POST target is unreachable.
	err = transport.Send(ctx, jsonRPCRequest{JSONRPC: "2.0", ID: 1, Method: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send POST")
}

func TestSSETransportServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := NewSSETransport(ctx, server.URL+"/sse")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestSSETransportConnectError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := NewSSETransport(ctx, "http://127.0.0.1:1/sse")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect to SSE endpoint")
}
