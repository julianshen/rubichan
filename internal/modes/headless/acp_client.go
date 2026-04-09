package headless

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
)

// ACPClient is a headless (CI/CD) ACP client.
type ACPClient struct {
	nextID     int64
	mu         sync.Mutex
	dispatcher *acp.ResponseDispatcher
	server     *acp.Server
	timeout    int // seconds, default 30 for CI/CD operations
}

// NewACPClient creates a new headless ACP client using the provided server.
// The server must already have capabilities registered by the agent.
// Returns an error if the dispatcher fails to start.
func NewACPClient(server *acp.Server) (*ACPClient, error) {
	// Create a stdio transport connected to the server
	transport := acp.NewStdioTransport(os.Stdin, os.Stdout, server)

	// Create dispatcher to route responses
	dispatcher := acp.NewResponseDispatcher(transport, server)

	// Start transport listener in background with error signal
	startedCh := make(chan error, 1)
	go func() {
		startedCh <- dispatcher.Start()
	}()

	// Wait for dispatcher to signal startup (success or error)
	// Use non-blocking select with timeout to detect startup failures
	// In test environments or when stdin is not available, allow graceful degradation
	select {
	case err := <-startedCh:
		// Dispatcher exited (either started successfully or failed)
		// In production, this would be an error; in tests, it's expected when stdin is unavailable
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("dispatcher startup failed: %w", err)
		}
		// If EOF, it means stdin closed (common in tests) - continue anyway
	case <-time.After(500 * time.Millisecond):
		// Dispatcher is running, continue initialization
	}

	client := &ACPClient{
		nextID:     1,
		dispatcher: dispatcher,
		server:     server,
		timeout:    30, // 30-second timeout for CI/CD operations
	}

	// Ensure startedCh is drained on error to avoid goroutine leak
	go func() {
		<-startedCh // Wait for dispatcher to eventually exit
	}()

	return client, nil
}

func (c *ACPClient) getNextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// RunCodeReview runs a code review on a file or directory.
func (c *ACPClient) RunCodeReview(codeInput string) (*acp.Response, error) {
	reviewReq := map[string]interface{}{
		"code": codeInput,
	}

	paramsData, err := json.Marshal(reviewReq)
	if err != nil {
		return nil, fmt.Errorf("marshal code review params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "agent/codeReview",
		Params:  paramsData,
	}

	timeout := time.Duration(c.Timeout()) * time.Second
	resp, err := c.dispatcher.SendRequest(context.Background(), req, timeout)
	if err != nil {
		return nil, fmt.Errorf("code review request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("code review error: %s", resp.Error.Message)
	}

	return resp, nil
}

// RunSecurityScan runs a security scan on a target.
func (c *ACPClient) RunSecurityScan(interactive bool) (*acp.SecurityScanResponse, error) {
	scanReq := acp.SecurityScanRequest{
		Interactive: interactive,
	}

	paramsData, err := json.Marshal(scanReq)
	if err != nil {
		return nil, fmt.Errorf("marshal security scan params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  acp.MethodSecurityScan,
		Params:  paramsData,
	}

	timeout := time.Duration(c.Timeout()) * time.Second
	resp, err := c.dispatcher.SendRequest(context.Background(), req, timeout)
	if err != nil {
		return nil, fmt.Errorf("security scan request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("security scan error: %s", resp.Error.Message)
	}

	var scanResp acp.SecurityScanResponse
	if err := json.Unmarshal(*resp.Result, &scanResp); err != nil {
		return nil, fmt.Errorf("unmarshal scan response: %w", err)
	}

	return &scanResp, nil
}

// SetTimeout sets the request timeout in seconds.
func (c *ACPClient) SetTimeout(seconds int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = seconds
}

// Timeout returns the current timeout in seconds.
func (c *ACPClient) Timeout() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timeout
}

// Close stops the dispatcher and cleans up resources.
func (c *ACPClient) Close() error {
	if c.dispatcher != nil {
		c.dispatcher.Stop()
	}
	return nil
}
