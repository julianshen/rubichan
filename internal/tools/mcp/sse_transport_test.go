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
