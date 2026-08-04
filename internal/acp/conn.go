package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Conn is a bidirectional ACP connection over a single byte stream.
//
// It exists because the Agent Client Protocol is symmetric: the client sends
// session/prompt, and *during* that turn the agent sends session/update
// notifications and session/request_permission requests back. The previous
// design could not express that — StdioTransport.Start and ResponseDispatcher
// were two read loops over the same stream, so a process could serve requests
// or await responses, never both.
//
// Conn runs one read loop and demultiplexes: a message carrying a method is an
// inbound request or notification; a message carrying only an id is a response
// to something this side sent.
type Conn struct {
	w io.Writer
	r *bufio.Scanner

	registry *CapabilityRegistry

	// writeMu serialises writes. Requests, responses and notifications share
	// one stream, and an interleaved write corrupts the wire.
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *Response
}

// NewConn creates a connection that reads from r, writes to w, and serves
// inbound method calls out of registry.
func NewConn(r io.Reader, w io.Writer, registry *CapabilityRegistry) *Conn {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, maxMessageSize), maxMessageSize)
	return &Conn{
		w:        w,
		r:        scanner,
		registry: registry,
		pending:  make(map[int64]chan *Response),
	}
}

// Serve runs the read loop until the stream ends, ctx is cancelled, or a write
// fails. Every pending request is failed on exit so no caller is left waiting
// on a connection that has gone away.
func (c *Conn) Serve(ctx context.Context) error {
	defer c.failPending()

	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		defer close(lines)
		for c.r.Scan() {
			// Scan reuses its buffer, so the payload must be copied before it
			// crosses the channel.
			line := append([]byte(nil), c.r.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		scanErr <- c.r.Err()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-scanErr:
			return err
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			if len(line) == 0 {
				continue
			}
			c.dispatch(line)
		}
	}
}

// dispatch routes one inbound message by shape: a method means the peer is
// asking us something, no method means it is answering us.
func (c *Conn) dispatch(line []byte) {
	var probe struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return
	}
	if probe.Method == "" {
		c.routeResponse(line)
		return
	}
	c.serveRequest(line)
}

// serveRequest answers an inbound method call. It runs in its own goroutine so
// a handler that itself calls Request — session/prompt asking the client for
// permission mid-turn — does not deadlock against the read loop that would
// carry the answer.
func (c *Conn) serveRequest(line []byte) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	// A request without an id is a notification: the peer wants no answer, and
	// replying to one would put an unmatched response on the wire.
	if req.ID == nil {
		go func() { _, _ = c.registry.Call(req.Method, req.Params) }()
		return
	}

	go func() {
		result, err := c.registry.Call(req.Method, req.Params)
		if err != nil {
			code := ErrorCodeInternalError
			if strings.Contains(err.Error(), "method not found") {
				code = ErrorCodeMethodNotFound
			}
			_ = c.write(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: code, Message: err.Error()},
			})
			return
		}
		_ = c.write(Response{JSONRPC: "2.0", ID: req.ID, Result: &result})
	}()
}

// routeResponse hands a response to whichever Request call is waiting on its
// ID. A response with no waiter is dropped: it is either a duplicate or a
// reply to a request that has already timed out.
func (c *Conn) routeResponse(line []byte) {
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return
	}
	id, ok := numericID(resp.ID)
	if !ok {
		return
	}

	c.mu.Lock()
	ch, waiting := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()

	if waiting {
		ch <- &resp
	}
}

// Request sends a method call to the peer and blocks until it answers, ctx is
// cancelled, or the connection drops.
func (c *Conn) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params for %s: %w", method, err)
	}

	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *Response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	// Always reclaim the slot, including on the ctx and drop paths, so a
	// cancelled call cannot leak an entry that nothing will ever remove.
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(Request{JSONRPC: "2.0", ID: id, Method: method, Params: raw}); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("connection closed while awaiting response to %s", method)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s failed: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
		}
		if resp.Result == nil {
			return nil, nil
		}
		return *resp.Result, nil
	}
}

// failPending closes every waiting channel so Request calls return an error
// rather than blocking forever once the connection is gone.
func (c *Conn) failPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func (c *Conn) write(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.w.Write(append(data, '\n'))
	return err
}

// marshalParams keeps an already-encoded payload as-is and encodes anything
// else, so callers may pass either.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(params)
}

// numericID normalises a JSON-RPC id to int64. Encoding a request turns the id
// into a JSON number, and decoding the peer's response yields float64, so the
// two must be reconciled before they can be matched.
func numericID(id any) (int64, bool) {
	switch v := id.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}
