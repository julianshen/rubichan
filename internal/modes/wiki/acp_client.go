package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
)

// GenerateOptions configures wiki generation.
type GenerateOptions struct {
	Scope      string
	Format     string
	OutputDir  string
	MaxDepth   int
	IncludeAPI bool
}

// ACPClient is a wiki generator ACP client.
type ACPClient struct {
	nextID     int64
	mu         sync.Mutex
	dispatcher *acp.ResponseDispatcher
	server     *acp.Server

	progress   int        // 0-100
	progressMu sync.Mutex // separate mutex for progress
}

// NewACPClient creates a new wiki ACP client.
func NewACPClient() *ACPClient {
	// Create capability registry
	registry := acp.NewCapabilityRegistry()

	// Create server with registry
	server := acp.NewServer(registry)

	// Create stdio transport
	transport := acp.NewStdioTransport(os.Stdin, os.Stdout, server)

	// Create dispatcher to route responses
	dispatcher := acp.NewResponseDispatcher(transport, server)

	// Start transport listener in background
	go func() {
		_ = dispatcher.Start() // Start listener, ignoring any errors (will be logged elsewhere)
	}()

	return &ACPClient{
		nextID:     1,
		dispatcher: dispatcher,
		server:     server,
		progress:   0,
	}
}

func (c *ACPClient) getNextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// GenerateDocs generates documentation for the codebase with progress tracking.
func (c *ACPClient) GenerateDocs(opts GenerateOptions) (*acp.Response, error) {
	// Set initial progress
	c.SetProgress(0)

	// Marshal options to map
	paramsStruct := map[string]interface{}{
		"scope":      opts.Scope,
		"format":     opts.Format,
		"outputDir":  opts.OutputDir,
		"maxDepth":   opts.MaxDepth,
		"includeAPI": opts.IncludeAPI,
	}

	paramsData, err := json.Marshal(paramsStruct)
	if err != nil {
		return nil, fmt.Errorf("marshal generate docs params: %w", err)
	}

	// Build request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "wiki/generate",
		Params:  paramsData,
	}

	// Send request with 60-second timeout
	resp, err := c.dispatcher.SendRequest(context.Background(), req, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("wiki/generate request failed: %w", err)
	}

	// Check for error in response
	if resp.Error != nil {
		return nil, fmt.Errorf("wiki/generate error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	// Set progress to 100 on success
	c.SetProgress(100)

	return resp, nil
}

// Progress returns the current progress (0-100).
func (c *ACPClient) Progress() int {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	return c.progress
}

// SetProgress updates the progress (0-100), clamping to valid range.
func (c *ACPClient) SetProgress(p int) {
	// Clamp to 0-100 range
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}

	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	c.progress = p
}

// Close stops the dispatcher and cleans up resources.
func (c *ACPClient) Close() error {
	if c.dispatcher != nil {
		c.dispatcher.Stop()
	}
	return nil
}
