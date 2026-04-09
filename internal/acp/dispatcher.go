package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// ResponseDispatcher correlates request IDs to responses.
// It owns a transport and routes incoming responses to waiting callers.
// IDs are normalized to float64 (JSON unmarshaling converts all numbers to float64).
type ResponseDispatcher struct {
	transport *StdioTransport
	server    *Server

	// pending maps request ID to a channel that will receive the response.
	// Keys are normalized to float64 for JSON compatibility.
	pending map[interface{}]chan *Response
	mu      sync.Mutex

	// stopCh signals the listener to stop
	stopCh chan struct{}

	// listenerDone signals when the listener has exited
	listenerDone chan struct{}
}

// normalizeID converts int64 IDs to float64 for JSON compatibility.
// JSON unmarshaling converts all numbers to float64, so we must use float64
// as the map key to ensure request-response correlation works correctly.
func normalizeID(id interface{}) interface{} {
	switch v := id.(type) {
	case int64:
		return float64(v)
	case int:
		return float64(v)
	default:
		return id
	}
}

// NewResponseDispatcher creates a dispatcher for a given transport and server.
func NewResponseDispatcher(transport *StdioTransport, server *Server) *ResponseDispatcher {
	return &ResponseDispatcher{
		transport:    transport,
		server:       server,
		pending:      make(map[interface{}]chan *Response),
		stopCh:       make(chan struct{}),
		listenerDone: make(chan struct{}),
	}
}

// Start begins listening for responses from the transport.
// This blocks; run it in a goroutine to avoid blocking the caller.
// When this method exits, all pending response channels are closed to prevent
// goroutines from waiting forever on dead channels.
func (d *ResponseDispatcher) Start() error {
	defer func() {
		// Clean up pending channels on exit to unblock any waiting requests
		d.mu.Lock()
		for _, respCh := range d.pending {
			close(respCh)
		}
		d.pending = make(map[interface{}]chan *Response)
		d.mu.Unlock()

		close(d.listenerDone)
	}()

	for {
		select {
		case <-d.stopCh:
			return nil
		default:
		}

		// Try to read a line from the transport
		if !d.transport.reader.Scan() {
			// No more input or scanner error
			if err := d.transport.reader.Err(); err != nil {
				return err
			}
			return nil
		}

		line := d.transport.reader.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse response
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			// Log parse error with context but continue processing
			log.Printf("dispatcher: failed to parse response: %v. Raw message (first 200 bytes): %s", err, truncate(string(line), 200))
			continue
		}

		// Route the response to waiting caller
		d.routeResponse(&resp)
	}
}

// SendRequest sends a request and waits for the response with the given timeout.
func (d *ResponseDispatcher) SendRequest(ctx context.Context, req Request, timeout time.Duration) (*Response, error) {
	// Normalize ID for JSON compatibility (JSON unmarshaling converts all numbers to float64)
	normalizedID := normalizeID(req.ID)

	// Create a response channel for this request
	respCh := make(chan *Response, 1)
	d.mu.Lock()
	d.pending[normalizedID] = respCh
	d.mu.Unlock()

	// Clean up when done (before returning from this function)
	defer func() {
		d.mu.Lock()
		delete(d.pending, normalizedID)
		d.mu.Unlock()
	}()

	// Send the request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if err := d.transport.SendMessage(json.RawMessage(reqData)); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Wait for response with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("response channel closed for request ID %v (method: %s). Dispatcher may have exited.", normalizedID, req.Method)
		}
		return resp, nil
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("request timeout after %v: ID=%v method=%s", timeout, normalizedID, req.Method)
	}
}

// Stop stops the listener goroutine.
func (d *ResponseDispatcher) Stop() {
	close(d.stopCh)
	<-d.listenerDone
}

// routeResponse routes an incoming response to the waiting caller.
func (d *ResponseDispatcher) routeResponse(resp *Response) {
	if resp == nil {
		return
	}

	// Normalize ID for comparison (JSON unmarshaling converts all numbers to float64)
	normalizedID := normalizeID(resp.ID)

	d.mu.Lock()
	respCh, ok := d.pending[normalizedID]
	d.mu.Unlock()

	if !ok {
		// Response with no matching request — log and ignore to aid debugging
		log.Printf("dispatcher: received response with unmatched ID %v (normalized: %v). This may indicate a protocol violation or ID correlation bug.", resp.ID, normalizedID)
		return
	}

	// Send response to waiting caller. Use select to avoid blocking if channel is closed.
	select {
	case respCh <- resp:
		// Success
	default:
		// Channel is closed or full — this shouldn't happen with buffered channels, log it
		log.Printf("dispatcher: unable to send response ID %v to pending channel (possibly closed)", resp.ID)
	}
}

// truncate returns a string truncated to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
