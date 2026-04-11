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
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ACPClient is a client for communicating with the ACP server in interactive mode.
type ACPClient struct {
	sessionMgr   *SessionManager       // for session loading
	resumeID     string                // optional session ID to resume
	loadedTurns  []Turn                // turns loaded from resume session
	loadError    error                 // tracks session load errors
	approvalFunc agentsdk.ApprovalFunc // callback for interactive tool approval; nil = auto-approve
	nextID       int64
	mu           sync.Mutex
	dispatcher   *acp.ResponseDispatcher
	server       *acp.Server
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
			client.loadError = err
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
			client.loadError = err
		}
	}

	return client
}

// NewACPClientWithApprovalFunc creates an ACPClient with an approval callback.
// This is a lightweight constructor for testing that doesn't require a server.
// When approvalFunc is nil, ApprovalRequest auto-approves all calls.
func NewACPClientWithApprovalFunc(approvalFunc agentsdk.ApprovalFunc) *ACPClient {
	return &ACPClient{
		loadedTurns:  []Turn{},
		nextID:       1,
		approvalFunc: approvalFunc,
	}
}

// SetApprovalFunc sets the approval callback for interactive tool approval.
// Must be called during initialization, before concurrent use of the client.
func (c *ACPClient) SetApprovalFunc(fn agentsdk.ApprovalFunc) {
	c.approvalFunc = fn
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

// ApprovalRequest asks the user whether to approve a tool call.
// When an approvalFunc is configured, it delegates to that callback (which
// typically bridges to the TUI approval overlay). When no callback is set,
// it falls back to auto-approve for backward compatibility.
func (c *ACPClient) ApprovalRequest(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
	if c.approvalFunc != nil {
		return c.approvalFunc(ctx, tool, input)
	}

	// Fallback: auto-approve when no callback is configured.
	return true, nil
}
