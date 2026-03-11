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

// maxContentLength is the maximum allowed Content-Length from a language server.
// Prevents unbounded memory allocation from buggy or malicious servers.
const maxContentLength = 64 * 1024 * 1024 // 64 MB

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

// ErrorHandler is called for non-fatal protocol errors (e.g., malformed JSON).
type ErrorHandler func(err error)

// Client is a JSON-RPC 2.0 client that communicates over an io.ReadWriteCloser
// using LSP Content-Length framing. NewClient starts a background goroutine;
// callers must call Close to release resources and stop the goroutine.
type Client struct {
	rwc     io.ReadWriteCloser
	reader  *bufio.Reader
	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan jsonrpcResponse

	notifyHandler NotificationHandler
	onError       ErrorHandler

	done    chan struct{}
	wg      sync.WaitGroup
	readErr atomic.Value // stores the error from the read loop
}

// NewClient creates a new JSON-RPC client over the given transport.
// The onNotify handler is called for server-initiated notifications (e.g.,
// textDocument/publishDiagnostics). It may be nil if notifications are not needed.
// Callers must call Close to stop the background read goroutine.
func NewClient(rwc io.ReadWriteCloser, onNotify NotificationHandler) *Client {
	return newClient(rwc, onNotify, nil)
}

// newClient creates a client with an optional error handler for protocol errors.
func newClient(rwc io.ReadWriteCloser, onNotify NotificationHandler, onError ErrorHandler) *Client {
	if rwc == nil {
		panic("lsp.NewClient: rwc must not be nil")
	}
	c := &Client{
		rwc:           rwc,
		reader:        bufio.NewReaderSize(rwc, 64*1024),
		pending:       make(map[int64]chan jsonrpcResponse),
		notifyHandler: onNotify,
		onError:       onError,
		done:          make(chan struct{}),
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.readLoop()
	}()
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
	select {
	case <-c.done:
		return fmt.Errorf("client closed")
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// jsonrpcRequest with ID=0 produces the correct notification wire format
	// because the ID field has omitempty.
	msg := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(msg)
}

// Close shuts down the client and waits for the read goroutine to exit.
func (c *Client) Close() error {
	select {
	case <-c.done:
		return nil // already closed
	default:
		close(c.done)
	}
	err := c.rwc.Close()
	c.wg.Wait()
	return err
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

// readLoop reads messages from the transport and dispatches them. If a read
// error occurs while the client is still active, it closes the done channel
// to trigger shutdown. On exit, it drains all pending callers via drainPending
// so blocked Call() invocations unblock with an error.
func (c *Client) readLoop() {
	defer c.drainPending()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		data, err := c.readMessage()
		if err != nil {
			// Store the transport error so drainPending can include a root cause.
			c.readErr.Store(err)
			// Transport closed or read error — signal shutdown.
			select {
			case <-c.done:
				// Already shutting down, expected.
			default:
				close(c.done)
			}
			return
		}

		c.dispatch(data)
	}
}

// drainPending unblocks all goroutines waiting in Call() by sending them
// a synthetic error response (code -1, "transport closed") and clearing the
// pending map.
func (c *Client) drainPending() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	msg := "transport closed"
	if stored := c.readErr.Load(); stored != nil {
		msg = fmt.Sprintf("transport closed: %v", stored)
	}

	for id, ch := range c.pending {
		ch <- jsonrpcResponse{
			ID:    id,
			Error: &jsonrpcError{Code: -1, Message: msg},
		}
		delete(c.pending, id)
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
	if contentLength > maxContentLength {
		return nil, fmt.Errorf("content-length %d exceeds maximum %d", contentLength, maxContentLength)
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// dispatch routes a received message to the appropriate handler.
func (c *Client) dispatch(data []byte) {
	var probe struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *jsonrpcError   `json:"error"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		if c.onError != nil {
			c.onError(fmt.Errorf("malformed message (%d bytes): %w", len(data), err))
		}
		return
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
		// Server notification — use params from the probe directly.
		if c.notifyHandler != nil {
			c.notifyHandler(probe.Method, probe.Params)
		}
		return
	}

	// Server-to-client request (has both ID and Method). We don't support
	// handling server requests; report via error handler so it's not silent.
	if probe.ID != nil && probe.Method != "" {
		if c.onError != nil {
			c.onError(fmt.Errorf("unsupported server request: %s (id=%d)", probe.Method, *probe.ID))
		}
	}
}
