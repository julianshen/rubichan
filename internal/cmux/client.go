// Package cmux provides a JSON-RPC client for communicating with cmux
// over Unix domain sockets.
package cmux

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultSocketPath = "/tmp/cmux.sock"
	defaultTimeout    = 5 * time.Second
)

// Response is a JSON-RPC response from cmux.
type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// Identity contains the cmux context identifiers cached at Dial time.
type Identity struct {
	WindowID    string `json:"window_id"`
	WorkspaceID string `json:"workspace_id"`
	PaneID      string `json:"pane_id"`
	SurfaceID   string `json:"surface_id"`
}

// Client communicates with cmux over a Unix domain socket.
// It is thread-safe via a mutex on socket writes.
// Request IDs are auto-incremented. Default timeout is 5 s per call.
type Client struct {
	conn     net.Conn
	enc      *json.Encoder
	dec      *json.Decoder
	mu       sync.Mutex // guards enc/dec
	counter  atomic.Int64
	identity *Identity
}

// SocketPath returns the Unix socket path for cmux.
// It reads the CMUX_SOCKET_PATH environment variable and falls back to
// "/tmp/cmux.sock" when the variable is empty.
func SocketPath() string {
	if p := os.Getenv("CMUX_SOCKET_PATH"); p != "" {
		return p
	}
	return defaultSocketPath
}

// Dial connects to the cmux daemon at socketPath, verifies the connection
// with a system.ping, and caches the caller's identity via system.identify.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socketPath, defaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("cmux: dial %s: %w", socketPath, err)
	}

	c := &Client{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}

	// Verify connectivity.
	if _, err := c.Call("system.ping", map[string]any{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cmux: ping failed: %w", err)
	}

	// Cache identity.
	resp, err := c.Call("system.identify", map[string]any{})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("cmux: identify failed: %w", err)
	}
	if !resp.OK {
		conn.Close()
		return nil, fmt.Errorf("cmux: identify failed: %s", resp.Error)
	}
	var id Identity
	if err := unmarshalResult(resp, &id); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cmux: decode identity: %w", err)
	}
	c.identity = &id

	return c, nil
}

// Call sends a JSON-RPC request and returns the response.
// The connection is protected by a mutex so concurrent callers are safe.
func (c *Client) Call(method string, params any) (*Response, error) {
	id := fmt.Sprintf("req-%d", c.counter.Add(1))

	req := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{
		ID:     id,
		Method: method,
		Params: params,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.conn.SetDeadline(time.Now().Add(defaultTimeout)); err != nil {
		return nil, fmt.Errorf("cmux: set deadline: %w", err)
	}
	if err := c.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("cmux: send request: %w", err)
	}

	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("cmux: read response: %w", err)
	}

	return &resp, nil
}

// Identity returns the cmux context identifiers cached at Dial time.
func (c *Client) Identity() *Identity {
	return c.identity
}

// Close closes the underlying socket connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// unmarshalResult decodes resp.Result into dst.
func unmarshalResult(resp *Response, dst any) error {
	return json.Unmarshal(resp.Result, dst)
}
