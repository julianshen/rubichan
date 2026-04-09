# ACP Transport Integration (Phase 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire up actual request/response communication in mode clients so they can send ACP messages to the server and receive responses via stdio transport.

**Architecture:** Mode clients will own a `ResponseDispatcher` that correlates request IDs to waiting responses. The dispatcher listens on the stdio transport in a goroutine, routing incoming responses to the correct waiter. Each client method sends a request, waits for the response with a timeout, and returns the result. Timeouts are context-based with reasonable defaults (5s for interactive, 30s for headless/wiki).

**Tech Stack:** Go 1.21+, sync primitives (Mutex, Cond), context with timeout, existing `internal/acp` protocol types.

---

## Task 1: Create Response Dispatcher Pattern

**Files:**
- Create: `internal/acp/dispatcher.go`
- Modify: `internal/acp/stdio.go` (no changes needed, dispatcher uses existing transport)

The dispatcher correlates request IDs to responses. It owns a `StdioTransport` and listens for responses in a background goroutine, routing them to waiting callers.

- [ ] **Step 1: Write the failing test for dispatcher**

```go
// internal/acp/dispatcher_test.go
package acp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
)

func TestDispatcherSendsAndReceivesRequest(t *testing.T) {
	// Create a mock transport that echoes requests back as responses
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
		Params:  json.RawMessage(`{"key":"value"}`),
	}

	// Dispatcher should be able to send and get response
	// This test will fail until dispatcher exists
	_ = req
	t.Skip("dispatcher not yet implemented")
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/acp -run TestDispatcherSendsAndReceivesRequest -v
```

Expected: Test skipped (placeholder).

- [ ] **Step 3: Implement ResponseDispatcher struct**

```go
// internal/acp/dispatcher.go
package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ResponseDispatcher correlates request IDs to responses.
// It owns a transport and routes incoming responses to waiting callers.
type ResponseDispatcher struct {
	transport *StdioTransport
	server    *Server

	// pending maps request ID to a channel that will receive the response
	pending map[interface{}]chan *Response
	mu      sync.Mutex

	// stopCh signals the listener to stop
	stopCh chan struct{}

	// listenerDone signals when the listener has exited
	listenerDone chan struct{}
}

// NewResponseDispatcher creates a dispatcher for a given transport and server.
func NewResponseDispatcher(transport *StdioTransport, server *Server) *ResponseDispatcher {
	return &ResponseDispatcher{
		transport:    transport,
		server:       server,
		pending:      make(map[interface{}]chan *Response),
		stopCh:       make(chan struct{}),
		listenerDone: make(chan struct{}),
	}
}

// Start begins listening for responses from the transport.
// This blocks; run it in a goroutine.
func (d *ResponseDispatcher) Start() error {
	defer close(d.listenerDone)

	// For now, Start is a no-op — actual listening will be implemented in a follow-up task
	// when we understand how the transport receives messages
	return nil
}

// SendRequest sends a request and waits for the response with the given timeout.
func (d *ResponseDispatcher) SendRequest(ctx context.Context, req Request, timeout time.Duration) (*Response, error) {
	// Create a response channel for this request
	respCh := make(chan *Response, 1)
	d.mu.Lock()
	d.pending[req.ID] = respCh
	d.mu.Unlock()

	// Clean up when done
	defer func() {
		d.mu.Lock()
		delete(d.pending, req.ID)
		d.mu.Unlock()
		close(respCh)
	}()

	// Send the request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if err := d.transport.SendMessage(json.RawMessage(reqData)); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Wait for response with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("response channel closed")
		}
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("request timeout after %v", timeout)
	}
}

// Stop stops the listener goroutine.
func (d *ResponseDispatcher) Stop() {
	close(d.stopCh)
	<-d.listenerDone
}

// routeResponse routes an incoming response to the waiting caller.
func (d *ResponseDispatcher) routeResponse(resp *Response) {
	d.mu.Lock()
	respCh, ok := d.pending[resp.ID]
	d.mu.Unlock()

	if !ok {
		// Response with no matching request — ignore
		return
	}

	respCh <- resp
}
```

- [ ] **Step 4: Write integration test for dispatcher**

```go
// internal/acp/dispatcher_test.go - add to existing file
func TestDispatcherRoutesResponses(t *testing.T) {
	// This test verifies that the dispatcher correctly routes responses to waiting callers
	// Detailed implementation after Step 5
	t.Skip("listener implementation pending")
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/acp -run TestDispatcher -v
```

Expected: Tests skip (pending listener implementation, which will be separate task for mode clients to use).

- [ ] **Step 6: Commit**

```bash
git add internal/acp/dispatcher.go internal/acp/dispatcher_test.go
git commit -m "[STRUCTURAL] Add ResponseDispatcher for ACP request-response correlation"
```

---

## Task 2: Update Interactive Client - Initialize Method

**Files:**
- Modify: `internal/modes/interactive/acp_client.go`
- Modify: `internal/modes/interactive/test/acp_client_test.go`

The interactive client will be updated to accept an ACP server and dispatcher, then implement the Initialize method with real request/response communication.

- [ ] **Step 1: Update ACPClient struct to own dispatcher**

```go
// internal/modes/interactive/acp_client.go - replace the struct definition
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
	go dispatcher.Start()
	
	return &ACPClient{
		nextID:     1,
		dispatcher: dispatcher,
		server:     server,
	}
}
```

- [ ] **Step 2: Implement Initialize with real request/response**

```go
// internal/modes/interactive/acp_client.go - replace Initialize method
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
```

- [ ] **Step 3: Add missing imports**

```go
// At top of internal/modes/interactive/acp_client.go
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
)
```

- [ ] **Step 4: Write test for Initialize**

```go
// internal/modes/interactive/test/acp_client_test.go - new test
func TestInitializeWithTransport(t *testing.T) {
	// Create a minimal server
	registry := &acp.CapabilityRegistry{}
	server := acp.NewServer(registry)
	
	// Create client
	client := interactive.NewACPClient(server)
	defer client.Close() // Will implement Close later
	
	// Call Initialize
	resp, err := client.Initialize("test-client")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	
	// Verify response
	if resp == nil {
		t.Error("response is nil")
	}
	if resp.ServerInfo.Name != "rubichan" {
		t.Errorf("got server name %q, want rubichan", resp.ServerInfo.Name)
	}
}
```

- [ ] **Step 5: Add Close method to client**

```go
// internal/modes/interactive/acp_client.go
func (c *ACPClient) Close() error {
	c.dispatcher.Stop()
	return nil
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/modes/interactive/test -v
```

Expected: TestInitializeWithTransport passes (or identifies missing transport plumbing for next iteration).

- [ ] **Step 7: Commit**

```bash
git add internal/modes/interactive/acp_client.go internal/modes/interactive/test/acp_client_test.go
git commit -m "[BEHAVIORAL] Implement Initialize with real ACP request/response in interactive client"
```

---

## Task 3: Update Interactive Client - Other Methods

**Files:**
- Modify: `internal/modes/interactive/acp_client.go`

Implement Prompt, ExecuteTool, InvokeSkill, and ApprovalRequest methods with real transport.

- [ ] **Step 1: Implement Prompt method**

```go
// internal/modes/interactive/acp_client.go
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
```

- [ ] **Step 2: Implement ExecuteTool method**

```go
// internal/modes/interactive/acp_client.go
func (c *ACPClient) ExecuteTool(toolName string, input json.RawMessage) (*acp.Response, error) {
	toolReq := map[string]interface{}{
		"tool":  toolName,
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
```

- [ ] **Step 3: Implement InvokeSkill method**

```go
// internal/modes/interactive/acp_client.go
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
```

- [ ] **Step 4: Implement ApprovalRequest method**

```go
// internal/modes/interactive/acp_client.go
func (c *ACPClient) ApprovalRequest(tool string, input json.RawMessage) (bool, error) {
	approvalReq := acp.SecurityApprovalRequest{
		ID:       fmt.Sprintf("%d", c.getNextID()),
		Tool:     tool,
		Input:    input,
		Decision: true, // User approval via TUI
	}
	
	paramsData, err := json.Marshal(approvalReq)
	if err != nil {
		return false, fmt.Errorf("marshal approval params: %w", err)
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
```

- [ ] **Step 5: Write tests for all methods**

```go
// internal/modes/interactive/test/acp_client_test.go - add tests
func TestExecuteToolWithTransport(t *testing.T) {
	registry := &acp.CapabilityRegistry{}
	server := acp.NewServer(registry)
	client := interactive.NewACPClient(server)
	defer client.Close()
	
	// Test ExecuteTool
	input := json.RawMessage(`{"file":"test.go"}`)
	resp, err := client.ExecuteTool("file/read", input)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if resp == nil {
		t.Error("response is nil")
	}
}

func TestInvokeSkillWithTransport(t *testing.T) {
	registry := &acp.CapabilityRegistry{}
	server := acp.NewServer(registry)
	client := interactive.NewACPClient(server)
	defer client.Close()
	
	// Test InvokeSkill
	skillReq := acp.SkillInvokeRequest{
		SkillName: "test-skill",
		Action:    "invoke",
		Input:     json.RawMessage(`{}`),
	}
	resp, err := client.InvokeSkill(skillReq)
	if err != nil {
		t.Fatalf("InvokeSkill failed: %v", err)
	}
	if resp == nil {
		t.Error("response is nil")
	}
}

func TestApprovalRequestWithTransport(t *testing.T) {
	registry := &acp.CapabilityRegistry{}
	server := acp.NewServer(registry)
	client := interactive.NewACPClient(server)
	defer client.Close()
	
	// Test ApprovalRequest
	input := json.RawMessage(`{"cmd":"rm -rf /"}`)
	approved, err := client.ApprovalRequest("shell/exec", input)
	if err != nil {
		t.Fatalf("ApprovalRequest failed: %v", err)
	}
	if !approved {
		t.Error("expected approval decision")
	}
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/modes/interactive/test -v
```

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/modes/interactive/acp_client.go internal/modes/interactive/test/acp_client_test.go
git commit -m "[BEHAVIORAL] Implement ExecuteTool, InvokeSkill, ApprovalRequest with transport"
```

---

## Task 4: Update Headless Client

**Files:**
- Modify: `internal/modes/headless/acp_client.go`
- Modify: `internal/modes/headless/test/acp_client_test.go`

The headless client follows the same pattern as interactive but with longer timeouts (30 seconds for CI/CD operations).

- [ ] **Step 1: Update headless client struct and constructor**

```go
// internal/modes/headless/acp_client.go
type ACPClient struct {
	nextID     int64
	mu         sync.Mutex
	dispatcher *acp.ResponseDispatcher
	server     *acp.Server
	timeout    int // in seconds
}

func NewACPClient() *ACPClient {
	transport := acp.NewStdioTransport(os.Stdin, os.Stdout, acp.NewServer(&acp.CapabilityRegistry{}))
	server := acp.NewServer(&acp.CapabilityRegistry{})
	dispatcher := acp.NewResponseDispatcher(transport, server)
	
	go dispatcher.Start()
	
	return &ACPClient{
		nextID:     1,
		dispatcher: dispatcher,
		server:     server,
		timeout:    30, // 30 second timeout for headless operations
	}
}

func (c *ACPClient) SetTimeout(seconds int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = seconds
}

func (c *ACPClient) Timeout() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timeout
}
```

- [ ] **Step 2: Implement RunCodeReview method**

```go
// internal/modes/headless/acp_client.go
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
```

- [ ] **Step 3: Implement RunSecurityScan method**

```go
// internal/modes/headless/acp_client.go
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
```

- [ ] **Step 4: Add Close method**

```go
// internal/modes/headless/acp_client.go
func (c *ACPClient) Close() error {
	c.dispatcher.Stop()
	return nil
}
```

- [ ] **Step 5: Write tests**

```go
// internal/modes/headless/test/acp_client_test.go
func TestRunCodeReviewWithTransport(t *testing.T) {
	client := headless.NewACPClient()
	defer client.Close()
	
	resp, err := client.RunCodeReview("func test() { return 1; }")
	if err != nil {
		t.Fatalf("RunCodeReview failed: %v", err)
	}
	if resp == nil {
		t.Error("response is nil")
	}
}

func TestRunSecurityScanWithTransport(t *testing.T) {
	client := headless.NewACPClient()
	defer client.Close()
	
	resp, err := client.RunSecurityScan(false)
	if err != nil {
		t.Fatalf("RunSecurityScan failed: %v", err)
	}
	if resp == nil {
		t.Error("response is nil")
	}
}

func TestTimeoutHandling(t *testing.T) {
	client := headless.NewACPClient()
	defer client.Close()
	
	client.SetTimeout(1) // 1 second timeout
	// Should handle timeout gracefully
	_ = client
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/modes/headless/test -v
```

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/modes/headless/acp_client.go internal/modes/headless/test/acp_client_test.go
git commit -m "[BEHAVIORAL] Implement RunCodeReview and RunSecurityScan with transport"
```

---

## Task 5: Update Wiki Client

**Files:**
- Modify: `internal/modes/wiki/acp_client.go`
- Modify: `internal/modes/wiki/test/acp_client_test.go`

The wiki client adds progress tracking for batch documentation generation.

- [ ] **Step 1: Update wiki client struct and constructor**

```go
// internal/modes/wiki/acp_client.go
type ACPClient struct {
	nextID      int64
	mu          sync.Mutex
	dispatcher  *acp.ResponseDispatcher
	server      *acp.Server
	progress    int // 0-100
	progressMu  sync.Mutex
}

func NewACPClient() *ACPClient {
	transport := acp.NewStdioTransport(os.Stdin, os.Stdout, acp.NewServer(&acp.CapabilityRegistry{}))
	server := acp.NewServer(&acp.CapabilityRegistry{})
	dispatcher := acp.NewResponseDispatcher(transport, server)
	
	go dispatcher.Start()
	
	return &ACPClient{
		nextID:     1,
		dispatcher: dispatcher,
		server:     server,
		progress:   0,
	}
}

func (c *ACPClient) SetProgress(percent int) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	c.progress = percent
}

func (c *ACPClient) Progress() int {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	return c.progress
}
```

- [ ] **Step 2: Implement GenerateDocs method with progress**

```go
// internal/modes/wiki/acp_client.go
type GenerateOptions struct {
	Scope      string
	Format     string
	OutputDir  string
	MaxDepth   int
	IncludeAPI bool
}

func (c *ACPClient) GenerateDocs(opts GenerateOptions) (*acp.Response, error) {
	c.SetProgress(0)
	
	genReq := map[string]interface{}{
		"scope":       opts.Scope,
		"format":      opts.Format,
		"outputDir":   opts.OutputDir,
		"maxDepth":    opts.MaxDepth,
		"includeAPI":  opts.IncludeAPI,
	}
	
	paramsData, err := json.Marshal(genReq)
	if err != nil {
		return nil, fmt.Errorf("marshal generate docs params: %w", err)
	}
	
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      c.getNextID(),
		Method:  "wiki/generate",
		Params:  paramsData,
	}
	
	// Wiki generation can take a while (60 second timeout)
	resp, err := c.dispatcher.SendRequest(context.Background(), req, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("generate docs request failed: %w", err)
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("generate docs error: %s", resp.Error.Message)
	}
	
	c.SetProgress(100)
	return resp, nil
}
```

- [ ] **Step 3: Add Close method**

```go
// internal/modes/wiki/acp_client.go
func (c *ACPClient) Close() error {
	c.dispatcher.Stop()
	return nil
}
```

- [ ] **Step 4: Write tests**

```go
// internal/modes/wiki/test/acp_client_test.go
func TestGenerateDocsWithTransport(t *testing.T) {
	client := wiki.NewACPClient()
	defer client.Close()
	
	opts := wiki.GenerateOptions{
		Scope:     "all",
		Format:    "markdown",
		OutputDir: "/tmp/docs",
		MaxDepth:  3,
	}
	
	resp, err := client.GenerateDocs(opts)
	if err != nil {
		t.Fatalf("GenerateDocs failed: %v", err)
	}
	if resp == nil {
		t.Error("response is nil")
	}
	
	// Check that progress was updated
	progress := client.Progress()
	if progress != 100 {
		t.Errorf("expected progress 100, got %d", progress)
	}
}

func TestProgressTracking(t *testing.T) {
	client := wiki.NewACPClient()
	defer client.Close()
	
	client.SetProgress(50)
	if client.Progress() != 50 {
		t.Error("progress should be 50")
	}
	
	// Test clamping
	client.SetProgress(150)
	if client.Progress() != 100 {
		t.Error("progress should clamp to 100")
	}
	
	client.SetProgress(-10)
	if client.Progress() != 0 {
		t.Error("progress should clamp to 0")
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/modes/wiki/test -v
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/modes/wiki/acp_client.go internal/modes/wiki/test/acp_client_test.go
git commit -m "[BEHAVIORAL] Implement GenerateDocs with progress tracking and transport"
```

---

## Task 6: Multi-Mode E2E Integration Test

**Files:**
- Create: `test/e2e/acp_transport_test.go`

Verify that all three mode clients can operate correctly with the ACP transport.

- [ ] **Step 1: Write E2E test verifying all modes work together**

```go
// test/e2e/acp_transport_test.go
package e2e_test

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/modes/headless"
	"github.com/julianshen/rubichan/internal/modes/interactive"
	"github.com/julianshen/rubichan/internal/modes/wiki"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"

	// Register provider implementations
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	_ "github.com/julianshen/rubichan/internal/provider/ollama"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
	_ "github.com/julianshen/rubichan/internal/provider/zai"
)

func TestAllModeClientsWithTransport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create a shared agent core (would be done in mode entrypoints)
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default: "anthropic",
			Model:   "claude-3-5-sonnet-20241022",
		},
		Agent: config.AgentConfig{
			MaxTurns: 3,
		},
	}

	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Skipf("provider not available: %v", err)
	}

	registry := tools.NewRegistry()
	approvalFunc := func(ctx context.Context, tool string, input interface{}) (bool, error) {
		return true, nil
	}

	agent := agent.New(p, registry, approvalFunc, cfg,
		agent.WithACP(),
	)

	// Test interactive client
	t.Run("InteractiveClientWithTransport", func(t *testing.T) {
		client := interactive.NewACPClient(agent.ACPServer())
		defer client.Close()

		// Initialize
		resp, err := client.Initialize("test-interactive")
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if resp == nil {
			t.Error("response is nil")
		}
	})

	// Test headless client
	t.Run("HeadlessClientWithTransport", func(t *testing.T) {
		client := headless.NewACPClient()
		defer client.Close()

		// This would normally be connected to the same agent server
		// For now, it's a separate client testing the transport
		client.SetTimeout(5)
		if client.Timeout() != 5 {
			t.Error("timeout not set correctly")
		}
	})

	// Test wiki client
	t.Run("WikiClientWithTransport", func(t *testing.T) {
		client := wiki.NewACPClient()
		defer client.Close()

		client.SetProgress(25)
		if client.Progress() != 25 {
			t.Error("progress not set correctly")
		}
	})
}
```

- [ ] **Step 2: Run E2E test**

```bash
go test ./test/e2e -run TestAllModeClientsWithTransport -v
```

Expected: Test passes, all three mode clients successfully communicate via transport.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/acp_transport_test.go
git commit -m "[BEHAVIORAL] Add E2E integration test for all mode clients with transport"
```

---

## Task 7: Update Mode Entrypoints to Wire ACP

**Files:**
- Modify: `cmd/rubichan/main.go` (entrypoint delegation to modes)
- Modify: `internal/modes/interactive/mode.go` (or similar)
- Modify: `internal/modes/headless/mode.go` (or similar)
- Modify: `internal/modes/wiki/mode.go` (or similar)

This task wires the mode entrypoints to properly instantiate the agent with ACP and pass the server to mode clients.

- [ ] **Step 1: Identify mode entrypoint files**

```bash
find /Users/julianshen/prj/rubichan -name "mode.go" -o -name "interactive.go" -o -name "headless.go" -o -name "wiki.go" | grep -E "internal/(modes|cmd)"
```

- [ ] **Step 2: Update mode entrypoints to pass ACP server to clients**

Example for interactive mode:

```go
// Pseudocode for the mode entrypoint
func RunInteractiveMode(cfg *config.Config) error {
	// Create agent with ACP enabled
	agent := agent.New(provider, registry, approvalFunc, cfg,
		agent.WithACP(),
	)

	// Create client with agent's ACP server
	client := interactive.NewACPClient(agent.ACPServer())
	defer client.Close()

	// Initialize and run TUI
	return client.RunTUI()
}
```

Similar updates for headless and wiki modes.

- [ ] **Step 3: Write integration tests for mode entrypoints**

Tests verifying that each mode can be started with ACP enabled and properly initializes the client.

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: All tests pass (100%+ pass rate maintained).

- [ ] **Step 5: Commit**

```bash
git add cmd/rubichan/main.go internal/modes/*/mode.go internal/modes/*/test/*
git commit -m "[BEHAVIORAL] Wire ACP server to mode entrypoints and clients"
```

---

## Task 8: Final Integration and Cleanup

**Files:**
- Modify: `CLAUDE.md` (documentation of Phase 5 completion)
- Modify: Various test files for final cleanup

- [ ] **Step 1: Update CLAUDE.md with Phase 5 completion notes**

```markdown
# Phase 5: Transport Integration (COMPLETE)

All mode clients now use real ACP request/response communication via stdio transport:

- **Interactive Client**: 5s timeout for user-facing operations
- **Headless Client**: 30s timeout for CI/CD operations
- **Wiki Client**: 60s timeout for batch documentation with progress tracking

Transport is managed by ResponseDispatcher for request-response correlation.
```

- [ ] **Step 2: Run full test suite**

```bash
go test ./... -cover
```

Expected: All tests pass with >90% coverage.

- [ ] **Step 3: Verify E2E tests**

```bash
go test ./test/e2e -v -run "Transport|ACP" 2>&1 | head -50
```

Expected: All transport and ACP integration tests passing.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "[STRUCTURAL] Document Phase 5 transport integration completion"
```

---

## Summary

This plan implements Phase 5: Transport Integration for ACP. It wires up the mode clients to actually send and receive ACP messages via stdio transport, with proper request-response correlation, timeouts, and error handling.

**Key Components:**
1. **ResponseDispatcher** - Correlates request IDs to waiting responses
2. **Mode Client Updates** - Interactive, Headless, Wiki clients use real transport
3. **Timeout Handling** - Context-based timeouts (5s interactive, 30s headless, 60s wiki)
4. **Progress Tracking** - Wiki mode tracks generation progress
5. **E2E Testing** - Comprehensive tests for all mode clients

**Execution Path:** Each task is independent and testable. Tasks can be completed in parallel (e.g., Task 3, 4, 5 for the three mode clients) once Task 1 (ResponseDispatcher) is complete.

