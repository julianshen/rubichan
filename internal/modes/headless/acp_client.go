package headless

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/acp"
)

// ACPClient is a headless (CI/CD) ACP client.
type ACPClient struct {
	nextID  int64
	mu      sync.Mutex
	timeout int // seconds
}

// NewACPClient creates a new headless ACP client.
func NewACPClient() *ACPClient {
	return &ACPClient{
		nextID:  1,
		timeout: 60,
	}
}

func (c *ACPClient) getNextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// RunCodeReview runs a code review on a file or directory.
func (c *ACPClient) RunCodeReview(target string, maxTurns int) (*acp.Response, error) {
	id := c.getNextID()

	paramsStruct := map[string]interface{}{
		"prompt":   fmt.Sprintf("Review this code for bugs, performance, and security: %s", target),
		"maxTurns": maxTurns,
	}
	paramsData, err := json.Marshal(paramsStruct)
	if err != nil {
		return nil, fmt.Errorf("marshal code review params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "agent/prompt",
		Params:  paramsData,
	}

	// TODO: Send request over transport and collect response
	_ = req
	return nil, nil
}

// RunSecurityScan runs a security scan on a target.
func (c *ACPClient) RunSecurityScan(target string, interactive bool) (*acp.SecurityScanResponse, error) {
	id := c.getNextID()

	scanReq := acp.SecurityScanRequest{
		Scope:       "project",
		Target:      target,
		Interactive: interactive,
	}
	paramsData, err := json.Marshal(scanReq)
	if err != nil {
		return nil, fmt.Errorf("marshal security scan params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "security/scan",
		Params:  paramsData,
	}

	// TODO: Send request over transport and collect response
	_ = req
	return nil, nil
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
