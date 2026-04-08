package wiki

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/acp"
)

// GenerateOptions configures wiki generation.
type GenerateOptions struct {
	Scope           string
	OutputDir       string
	IncludeSecurity bool
	MaxDepth        int
}

// ACPClient is a wiki generator ACP client.
type ACPClient struct {
	nextID   int64
	mu       sync.Mutex
	progress int // 0-100
}

// NewACPClient creates a new wiki ACP client.
func NewACPClient() *ACPClient {
	return &ACPClient{
		nextID:   1,
		progress: 0,
	}
}

func (c *ACPClient) getNextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// GenerateDocs generates documentation for the codebase.
func (c *ACPClient) GenerateDocs(rootPath string, opts *GenerateOptions) (*acp.Response, error) {
	id := c.getNextID()

	if opts == nil {
		opts = &GenerateOptions{
			Scope:     "project",
			OutputDir: "docs/generated",
		}
	}

	paramsStruct := map[string]interface{}{
		"rootPath":        rootPath,
		"scope":           opts.Scope,
		"outputDir":       opts.OutputDir,
		"includeSecurity": opts.IncludeSecurity,
		"maxDepth":        opts.MaxDepth,
	}
	paramsData, err := json.Marshal(paramsStruct)
	if err != nil {
		return nil, fmt.Errorf("marshal generate docs params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "wiki/generate",
		Params:  paramsData,
	}

	// TODO: Send request over transport and collect response
	_ = req
	return nil, nil
}

// Progress returns the current progress (0-100).
func (c *ACPClient) Progress() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progress
}

// SetProgress updates the progress (0-100).
func (c *ACPClient) SetProgress(p int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	c.progress = p
}
