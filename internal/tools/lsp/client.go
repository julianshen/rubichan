package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// jsonrpcRequest is a JSON-RPC 2.0 request message.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response message.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcNotification is a JSON-RPC 2.0 notification (no ID).
type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonrpcError is a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonrpcError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// NotificationHandler handles server-initiated notifications.
type NotificationHandler func(method string, params json.RawMessage)

// Client is a JSON-RPC 2.0 client that communicates over an io.ReadWriteCloser
// using LSP Content-Length framing.
type Client struct {
	rwc     io.ReadWriteCloser
	reader  *bufio.Reader
	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan jsonrpcResponse

	notifyHandler NotificationHandler

	done chan struct{}
}

// NewClient creates a new JSON-RPC client over the given transport.
// The onNotify handler is called for server-initiated notifications (e.g.,
// textDocument/publishDiagnostics). It may be nil if notifications are not needed.
func NewClient(rwc io.ReadWriteCloser, onNotify NotificationHandler) *Client {
	c := &Client{
		rwc:           rwc,
		reader:        bufio.NewReaderSize(rwc, 64*1024),
		pending:       make(map[int64]chan jsonrpcResponse),
		notifyHandler: onNotify,
		done:          make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	select {
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	default:
	}

	id := c.nextID.Add(1)

	ch := make(chan jsonrpcResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	// Notifications have no ID field.
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(msg)
}

// Close shuts down the client.
func (c *Client) Close() error {
	select {
	case <-c.done:
		return nil // already closed
	default:
		close(c.done)
	}
	return c.rwc.Close()
}

// writeMessage encodes a message with Content-Length framing and writes it.
func (c *Client) writeMessage(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := io.WriteString(c.rwc, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.rwc.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// readLoop reads messages from the transport and dispatches them.
func (c *Client) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		data, err := c.readMessage()
		if err != nil {
			// Transport closed or error — stop reading.
			return
		}

		c.dispatch(data)
	}
}

// readMessage reads one LSP message using Content-Length framing.
func (c *Client) readMessage() ([]byte, error) {
	contentLength := -1

	// Read headers until empty line.
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			val := strings.TrimPrefix(line, "Content-Length: ")
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("parse content-length %q: %w", val, err)
			}
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// dispatch routes a received message to the appropriate handler.
func (c *Client) dispatch(data []byte) {
	// Try to determine if this is a response (has "id" and "result"/"error")
	// or a notification (has "method" but no "id").
	var probe struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *jsonrpcError   `json:"error"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return // malformed message
	}

	if probe.ID != nil && probe.Method == "" {
		// Response to a pending request.
		resp := jsonrpcResponse{
			ID:     *probe.ID,
			Result: probe.Result,
			Error:  probe.Error,
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		c.pendingMu.Unlock()
		if ok {
			ch <- resp
		}
		return
	}

	if probe.Method != "" && probe.ID == nil {
		// Server notification.
		if c.notifyHandler != nil {
			var notif jsonrpcNotification
			if err := json.Unmarshal(data, &notif); err == nil {
				c.notifyHandler(notif.Method, notif.Params)
			}
		}
	}
}
