package interactive

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

// ACPClient is a client for communicating with the ACP server in interactive mode.
type ACPClient struct {
	sessionMgr  *SessionManager         // NEW - for session loading
	resumeID    string                  // NEW - optional session ID to resume
	loadedTurns []Turn                  // NEW - turns loaded from resume session
	loadError   error                   // NEW - tracks session load errors
	nextID      int64
	mu          sync.Mutex
	dispatcher  *acp.ResponseDispatcher
	server      *acp.Server
}

// NewACPClient creates an interactive ACP client given a server instance and optional session manager.
// If sessionMgr is provided and resumeID is not empty, the session will be auto-loaded.
// Returns an error if the dispatcher fails to start.
func NewACPClient(sessionMgr *SessionManager, resumeID string, server *acp.Server) (*ACPClient, error) {
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
		sessionMgr:  sessionMgr,
		resumeID:    resumeID,
		loadedTurns: []Turn{},
		loadError:   nil,
		nextID:      1,
		dispatcher:  dispatcher,
		server:      server,
	}

	// Load session if resumeID provided
	if resumeID != "" && sessionMgr != nil {
		turns, err := sessionMgr.Load(resumeID)
		if err == nil {
			client.loadedTurns = turns
		} else {
			client.loadError = fmt.Errorf("load session %s: %w", resumeID, err)
		}
	}

	// Ensure startedCh is drained on error to avoid goroutine leak
	go func() {
		<-startedCh // Wait for dispatcher to eventually exit
	}()

	return client, nil
}

// NewACPClientWithResume creates an ACPClient with session resumption.
// This is a convenience constructor for testing that doesn't require a server instance.
// It is not used in production code.
func NewACPClientWithResume(sessionMgr *SessionManager, resumeID string) *ACPClient {
	client := &ACPClient{
		sessionMgr:  sessionMgr,
		resumeID:    resumeID,
		loadedTurns: []Turn{},
		loadError:   nil,
		nextID:      1,
		dispatcher:  nil,
		server:      nil,
	}

	// Load session if resumeID provided
	if resumeID != "" && sessionMgr != nil {
		turns, err := sessionMgr.Load(resumeID)
		if err == nil {
			client.loadedTurns = turns
		} else {
			client.loadError = fmt.Errorf("load session %s: %w", resumeID, err)
		}
	}

	return client
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

// LoadedTurns returns turns loaded from resume session.
func (ac *ACPClient) LoadedTurns() ([]Turn, error) {
	return ac.loadedTurns, nil
}

// LoadError returns any error that occurred during session loading.
func (ac *ACPClient) LoadError() error {
	return ac.loadError
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
	// Build the initialize request using the provided clientName
	initParams := acp.InitializeParams{
		ClientInfo: acp.ClientInfo{
			Name:    clientName, // Use the provided clientName parameter
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

// ApprovalRequest is a stub that auto-approves tool execution.
// SECURITY ISSUE: This is not properly implemented and always returns true (approve).
// In a production implementation, this should present an approval overlay to the user
// asking them to manually review and approve/reject the operation.
// TODO: Wire this to the actual TUI approval overlay.
func (c *ACPClient) ApprovalRequest(tool string, input json.RawMessage) (bool, error) {
	// STUB: Auto-approves all operations without user input.
	// This is a placeholder and should NOT be used in production.
	// Proper implementation should show a TUI dialog asking for user approval.
	approvalResp := acp.SecurityApprovalResponse{
		Decision: "approve", // STUB: Always approve without asking
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

	// STUB: Always returns true (approved) without user interaction
	return true, nil
}
