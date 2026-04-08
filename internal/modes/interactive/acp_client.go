package interactive

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/acp"
)

// ACPClient is a client for communicating with the ACP server in interactive mode.
type ACPClient struct {
	nextID int64
	mu     sync.Mutex
}

// NewACPClient creates a new interactive ACP client.
func NewACPClient() *ACPClient {
	return &ACPClient{
		nextID: 1,
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

// Initialize sends an initialize request to the ACP server.
func (c *ACPClient) Initialize(clientName string) (*acp.InitializeResponse, error) {
	id := c.getNextID()

	params := acp.InitializeParams{
		ClientInfo: acp.ClientInfo{
			Name:    clientName,
			Version: "1.0.0",
		},
	}

	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal initialize params: %w", err)
	}
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params:  paramsData,
	}

	// TODO: Send request over transport and wait for response
	// For now, return a dummy response
	resp := &acp.InitializeResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: acp.InitializeResult{
			ServerInfo: acp.ServerInfo{
				Name:    "rubichan",
				Version: "1.0.0",
			},
		},
	}

	_ = req
	return resp, nil
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
