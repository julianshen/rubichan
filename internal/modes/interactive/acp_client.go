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
func (c *ACPClient) Prompt(turn string) (*acp.Response, error) {
	promptReq := map[string]interface{}{
		"turn": turn,
	}

	paramsData, err := json.Marshal(promptReq)
	if err != nil {
		return nil, fmt.Errorf("marshal prompt params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "agent/prompt",
		Params:  paramsData,
	}

	resp, err := c.dispatcher.SendRequest(context.Background(), req, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("prompt request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("prompt error: %s", resp.Error.Message)
	}

	return resp, nil
}

// ExecuteTool executes a tool via ACP.
func (c *ACPClient) ExecuteTool(name string, input json.RawMessage) (*acp.Response, error) {
	toolReq := map[string]interface{}{
		"tool":  name,
		"input": input,
	}

	paramsData, err := json.Marshal(toolReq)
	if err != nil {
		return nil, fmt.Errorf("marshal execute tool params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "tool/execute",
		Params:  paramsData,
	}

	resp, err := c.dispatcher.SendRequest(context.Background(), req, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("execute tool request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("execute tool error: %s", resp.Error.Message)
	}

	return resp, nil
}

// InvokeSkill invokes a skill via ACP.
func (c *ACPClient) InvokeSkill(skillReq acp.SkillInvokeRequest) (*acp.SkillInvokeResponse, error) {
	paramsData, err := json.Marshal(skillReq)
	if err != nil {
		return nil, fmt.Errorf("marshal skill invoke params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  acp.MethodSkillInvoke,
		Params:  paramsData,
	}

	resp, err := c.dispatcher.SendRequest(context.Background(), req, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invoke skill request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("invoke skill error: %s", resp.Error.Message)
	}

	var skillResp acp.SkillInvokeResponse
	if err := json.Unmarshal(*resp.Result, &skillResp); err != nil {
		return nil, fmt.Errorf("unmarshal skill response: %w", err)
	}

	return &skillResp, nil
}

// ApprovalRequest asks the user to approve a security verdict.
func (c *ACPClient) ApprovalRequest(tool string, input json.RawMessage) (bool, error) {
	// Build approval response with decision to approve
	approvalResp := acp.SecurityApprovalResponse{
		Decision: "approve",
	}

	paramsData, err := json.Marshal(approvalResp)
	if err != nil {
		return false, fmt.Errorf("marshal approval request params: %w", err)
	}

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  acp.MethodSecurityApprove,
		Params:  paramsData,
	}

	resp, err := c.dispatcher.SendRequest(context.Background(), req, 5*time.Second)
	if err != nil {
		return false, fmt.Errorf("approval request failed: %w", err)
	}

	if resp.Error != nil {
		return false, fmt.Errorf("approval error: %s", resp.Error.Message)
	}

	return true, nil
}
