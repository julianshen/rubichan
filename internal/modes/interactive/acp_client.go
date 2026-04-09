package interactive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
)

// ACPClient is a client for communicating with the ACP server in interactive mode.
type ACPClient struct {
	nextID     int64
	mu         sync.Mutex
	dispatcher *acp.ResponseDispatcher
	server     *acp.Server
}

// NewACPClient creates an interactive ACP client given a server instance.
func NewACPClient(server *acp.Server) *ACPClient {
	// Create a stdio transport connected to the server
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
	}
}

// getNextID returns the next request ID and increments the counter.
func (c *ACPClient) getNextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// GetNextID returns the next request ID and increments the counter (for testing).
func (c *ACPClient) GetNextID() int64 {
	return c.getNextID()
}

// Close stops the dispatcher and cleans up resources.
func (c *ACPClient) Close() error {
	if c.dispatcher != nil {
		c.dispatcher.Stop()
	}
	return nil
}

// SetDispatcher sets the dispatcher for testing purposes.
func (c *ACPClient) SetDispatcher(d *acp.ResponseDispatcher) {
	c.dispatcher = d
}

// Initialize sends an initialize request to the ACP server.
func (c *ACPClient) Initialize(clientName string) (*acp.InitializeResponse, error) {
	// Build the initialize request
	initParams := acp.InitializeParams{
		ClientInfo: acp.ClientInfo{
			Name:    "rubichan-interactive",
			Version: "1.0.0",
		},
	}

	paramsData, err := json.Marshal(initParams)
	if err != nil {
		return nil, fmt.Errorf("marshal initialize params: %w", err)
	}

	// Get next request ID
	id := c.getNextID()

	// Build request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  acp.MethodInitialize,
		Params:  paramsData,
	}

	// Send request and wait for response (5 second timeout for interactive mode)
	resp, err := c.dispatcher.SendRequest(context.Background(), req, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("initialize request failed: %w", err)
	}

	// Parse response
	if resp.Error != nil {
		return nil, fmt.Errorf("initialize error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var initResp acp.InitializeResponse
	if err := json.Unmarshal(*resp.Result, &initResp); err != nil {
		return nil, fmt.Errorf("unmarshal initialize response: %w", err)
	}

	return &initResp, nil
}

// Prompt sends a prompt request to the agent.
func (c *ACPClient) Prompt(prompt string, maxTurns int) (*acp.Response, error) {
	id := c.getNextID()

	paramsStruct := map[string]interface{}{
		"prompt":   prompt,
		"maxTurns": maxTurns,
	}
	paramsData, err := json.Marshal(paramsStruct)
	if err != nil {
		return nil, fmt.Errorf("marshal prompt params: %w", err)
	}
	params := json.RawMessage(paramsData)

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "agent/prompt",
		Params:  params,
	}

	// TODO: Send request over transport and wait for response
	_ = req
	return nil, nil
}

// ExecuteTool executes a tool via ACP.
func (c *ACPClient) ExecuteTool(name string, input json.RawMessage) (*acp.Response, error) {
	id := c.getNextID()

	paramsStruct := map[string]interface{}{
		"tool":  name,
		"input": json.RawMessage(input),
	}
	paramsData, err := json.Marshal(paramsStruct)
	if err != nil {
		return nil, fmt.Errorf("marshal execute tool params: %w", err)
	}
	params := json.RawMessage(paramsData)

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tool/execute",
		Params:  params,
	}

	// TODO: Send request over transport and wait for response
	_ = req
	return nil, nil
}

// InvokeSkill invokes a skill via ACP.
func (c *ACPClient) InvokeSkill(skillName, action string, input json.RawMessage) (*acp.SkillInvokeResponse, error) {
	id := c.getNextID()

	params := acp.SkillInvokeRequest{
		SkillName: skillName,
		Action:    action,
		Input:     input,
	}
	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal skill invoke params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "skill/invoke",
		Params:  paramsData,
	}

	// TODO: Send request over transport and wait for response
	_ = req
	return nil, nil
}

// ApprovalRequest asks the user to approve a security verdict.
func (c *ACPClient) ApprovalRequest(verdict acp.SecurityApprovalRequest) (*acp.SecurityApprovalResponse, error) {
	id := c.getNextID()

	paramsData, err := json.Marshal(verdict)
	if err != nil {
		return nil, fmt.Errorf("marshal approval request params: %w", err)
	}
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "security/requestApproval",
		Params:  paramsData,
	}

	// TODO: Send request over transport and wait for response
	_ = req
	return nil, nil
}
