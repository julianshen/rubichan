package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

)

// Compile-time check: SSETransport implements Transport.
var _ Transport = (*SSETransport)(nil)

// SSETransport communicates with an MCP server over HTTP SSE.
// It reads server-sent events for responses and sends requests via HTTP POST.
type SSETransport struct {
	sseURL     string
	postURL    string
	client     *http.Client
	responseCh chan json.RawMessage
	cancel     context.CancelFunc
	closeOnce  sync.Once
	done       chan struct{}
}

// NewSSETransport connects to an MCP server's SSE endpoint and waits for
// the "endpoint" event that tells us where to POST requests.
func NewSSETransport(ctx context.Context, sseURL string) (*SSETransport, error) {
	sseCtx, cancel := context.WithCancel(ctx)

	t := &SSETransport{
		sseURL:     sseURL,
		client:     &http.Client{},
		responseCh: make(chan json.RawMessage, 64),
		cancel:     cancel,
		done:       make(chan struct{}),
	}

	// Connect to SSE endpoint
	req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, sseURL, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect to SSE endpoint: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("SSE endpoint returned %d", resp.StatusCode)
	}

	// Wait for the "endpoint" event to learn the POST URL
	endpointCh := make(chan string, 1)
	go t.readSSE(sseCtx, resp.Body, endpointCh)

	select {
	case endpoint := <-endpointCh:
		// Resolve endpoint URL against the SSE base URL using net/url for
		// robust handling of relative paths, ports, userinfo, etc.
		baseURL, err := url.Parse(sseURL)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("parse SSE base URL: %w", err)
		}
		ref, err := url.Parse(endpoint)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("parse endpoint URL: %w", err)
		}
		t.postURL = baseURL.ResolveReference(ref).String()
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}

	return t, nil
}

// readSSE reads the SSE stream, dispatching "endpoint" and "message" events.
func (t *SSETransport) readSSE(ctx context.Context, body io.ReadCloser, endpointCh chan<- string) {
	defer close(t.done)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var eventType string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "endpoint":
				select {
				case endpointCh <- data:
				default:
				}
			case "message":
				select {
				case t.responseCh <- json.RawMessage(data):
				case <-ctx.Done():
					return
				}
			}
			eventType = ""
			continue
		}
	}
}

// Send marshals the message and POSTs it to the MCP server.
func (t *SSETransport) Send(ctx context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.postURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send POST: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("POST returned %d", resp.StatusCode)
	}
	return nil
}

// Receive reads the next message from the SSE response channel.
func (t *SSETransport) Receive(ctx context.Context, result any) error {
	select {
	case msg, ok := <-t.responseCh:
		if !ok {
			return io.EOF
		}
		return json.Unmarshal(msg, result)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close shuts down the SSE connection.
func (t *SSETransport) Close() error {
	t.closeOnce.Do(func() {
		t.cancel()
	})
	<-t.done
	return nil
}
