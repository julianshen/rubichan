package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sync"
)

// maxMessageSize caps a single JSON-RPC message at 10 MB, which accommodates
// large file reads and completion outputs. bufio.Scanner has a hard internal
// token limit, so a payload beyond this fails the read rather than truncating.
const maxMessageSize = 10 * 1024 * 1024

// notificationBacklog bounds how far the peer may run ahead of the notification
// handler before the read loop pauses. Generous enough that ordinary streaming
// never blocks, small enough that a flood cannot grow without limit.
const notificationBacklog = 256

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

	// notifications are served by a single worker in arrival order. ACP's
	// session/update is a stream of turn events, so delivering them
	// concurrently does not just interleave work, it renders the wrong turn.
	// Requests keep their own goroutine each, because a request handler may
	// call Request and must not block the read loop that carries its answer.
	notifications chan Request

	// writeMu serialises writes. Requests, responses and notifications share
	// one stream, and an interleaved write corrupts the wire.
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *Response
	// closed is set once Serve has stopped. Without it, a request registered
	// after Serve returned would wait on a channel nothing will ever close,
	// because failPending has already run. Guarded by mu, so registering and
	// closing cannot interleave.
	closed bool
}

// ErrConnClosed is returned by Request when the connection has stopped serving,
// whether it was already down or dropped mid-call.
var ErrConnClosed = errors.New("acp: connection closed")

// NewConn creates a connection that reads from r, writes to w, and serves
// inbound method calls out of registry.
func NewConn(r io.Reader, w io.Writer, registry *CapabilityRegistry) *Conn {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, maxMessageSize), maxMessageSize)
	return &Conn{
		w:             w,
		r:             scanner,
		registry:      registry,
		pending:       make(map[int64]chan *Response),
		notifications: make(chan Request, notificationBacklog),
	}
}

// Serve runs the read loop until the stream ends, ctx is cancelled, or a write
// fails. Every pending request is failed on exit so no caller is left waiting
// on a connection that has gone away.
func (c *Conn) Serve(ctx context.Context) error {
	go func() {
		for n := range c.notifications {
			if _, err := c.registry.Call(n.Method, n.Params); err != nil {
				log.Printf("acp: notification %s failed: %v", n.Method, err)
			}
		}
	}()

	// Defers run last-registered-first, and the order matters. failPending is
	// registered last so it runs first: callers blocked in Request are released
	// before anything else is torn down. Closing the notification channel then
	// lets the worker drain and exit on its own.
	//
	// Serve deliberately does not wait for that worker. A handler that wedges
	// would otherwise hold shutdown open indefinitely and keep every pending
	// Request waiting on a connection that has already finished. Notifications
	// are fire-and-forget, so letting the worker outlive Serve costs nothing;
	// blocking on it costs the shutdown guarantee.
	//
	// The close is safe because only dispatch sends, and dispatch runs on this
	// goroutine — once the loop below exits there are no further senders.
	defer close(c.notifications)
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
		case line, ok := <-lines:
			if !ok {
				// The producer sends its error before closing lines, so the
				// value is already available. Reading it here rather than in a
				// sibling select case matters: both would be ready at once and
				// select picks at random, silently turning a read failure into
				// a clean end of stream.
				select {
				case err := <-scanErr:
					if err != nil {
						return fmt.Errorf("read acp stream: %w", err)
					}
				default:
				}
				return nil
			}
			if len(line) == 0 {
				continue
			}
			c.dispatch(ctx, line)
		}
	}
}

// dispatch routes one inbound message by shape: a method means the peer is
// asking us something, no method means it is answering us.
func (c *Conn) dispatch(ctx context.Context, line []byte) {
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
	c.serveRequest(ctx, line)
}

// serveRequest answers an inbound method call. It runs in its own goroutine so
// a handler that itself calls Request — session/prompt asking the client for
// permission mid-turn — does not deadlock against the read loop that would
// carry the answer.
func (c *Conn) serveRequest(ctx context.Context, line []byte) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		// The peer may still be waiting on an id we can recover. Answering is
		// what separates a malformed request from a hung one.
		var probe struct {
			ID any `json:"id"`
		}
		if json.Unmarshal(line, &probe) == nil && probe.ID != nil {
			_ = c.write(Response{
				JSONRPC: "2.0",
				ID:      probe.ID,
				Error:   &RPCError{Code: ErrorCodeInvalidRequest, Message: err.Error()},
			})
		}
		return
	}

	// A request without an id is a notification: the peer wants no answer, and
	// replying to one would put an unmatched response on the wire.
	if req.ID == nil {
		// Sending from the read loop is deliberate back-pressure: if the peer
		// outruns the handler the loop pauses, which is preferable to an
		// unbounded goroutine per notification. Handlers must therefore not
		// call Request, since that would wait on the loop they are blocking.
		// Cancellation-aware: if the peer outruns the handler *and* the
		// connection is being torn down, an unconditional send would pin the
		// read loop against a full channel that nothing will drain in time.
		select {
		case c.notifications <- req:
		case <-ctx.Done():
		}
		return
	}

	go func() {
		result, err := c.registry.Call(req.Method, req.Params)
		if err != nil {
			code := ErrorCodeInternalError
			switch {
			case errors.Is(err, ErrMethodNotFound):
				code = ErrorCodeMethodNotFound
			case errors.Is(err, ErrInvalidParams):
				code = ErrorCodeInvalidParams
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
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, ErrConnClosed)
	}
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
			return nil, fmt.Errorf("awaiting response to %s: %w", method, ErrConnClosed)
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
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func (c *Conn) write(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal acp message: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write acp message: %w", err)
	}
	return nil
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
//
// A float is accepted only when it is exactly an integer this side could have
// sent. Truncating instead would be worse than refusing: an id of 1.5 would
// become 1 and hand the peer's answer to whoever is waiting on request 1 — for
// session/request_permission, an approval the user never gave, returned as a
// valid result. Refusing merely drops a message no request is owed.
func numericID(id any) (int64, bool) {
	switch v := id.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return 0, false
		}
		if v < math.MinInt64 || v >= math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}
