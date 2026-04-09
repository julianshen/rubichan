package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ResponseDispatcher correlates request IDs to responses.
// It owns a transport and routes incoming responses to waiting callers.
type ResponseDispatcher struct {
	transport *StdioTransport
	server    *Server

	// pending maps request ID to a channel that will receive the response
	pending map[interface{}]chan *Response
	mu      sync.Mutex

	// stopCh signals the listener to stop
	stopCh chan struct{}

	// listenerDone signals when the listener has exited
	listenerDone chan struct{}
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
// This blocks; run it in a goroutine.
func (d *ResponseDispatcher) Start() error {
	defer close(d.listenerDone)

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
			// Log parse error but continue
			continue
		}

		// Route the response to waiting caller
		d.routeResponse(&resp)
	}
}

// SendRequest sends a request and waits for the response with the given timeout.
func (d *ResponseDispatcher) SendRequest(ctx context.Context, req Request, timeout time.Duration) (*Response, error) {
	// Create a response channel for this request
	respCh := make(chan *Response, 1)
	d.mu.Lock()
	d.pending[req.ID] = respCh
	d.mu.Unlock()

	// Clean up when done (before returning from this function)
	defer func() {
		d.mu.Lock()
		delete(d.pending, req.ID)
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
			return nil, fmt.Errorf("response channel closed")
		}
		return resp, nil
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("request timeout after %v", timeout)
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

	d.mu.Lock()
	respCh, ok := d.pending[resp.ID]
	d.mu.Unlock()

	if !ok {
		// Response with no matching request — ignore
		return
	}

	respCh <- resp
}
