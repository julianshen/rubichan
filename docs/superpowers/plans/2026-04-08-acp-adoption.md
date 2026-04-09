# Agent Client Protocol (ACP) Adoption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adopt the Agent Client Protocol as Rubichan's standardized backbone for agent-editor and agent-mode communication, replacing the current custom JSON-RPC implementation while maintaining all three execution modes (Interactive TUI, Headless CI/CD, Wiki Generator).

**Architecture:** 
ACP will serve as the transport layer standardizing communication between Rubichan's agent core and external consumers (editors, CI/CD systems, wiki batch jobs). Rubichan will implement an ACP server that runs as a subprocess (stdio-based JSON-RPC for local Interactive/Headless modes) or as a service (HTTP/WS for future remote scenarios). We'll extend ACP with Rubichan-specific capability blocks for the skill system, security verdicts, and wiki batch operations. The three execution modes become thin "client" adapters that speak ACP to the common agent core rather than direct API calls.

**Tech Stack:** 
- Agent Client Protocol (standardized JSON-RPC over stdio / HTTP / WebSocket)
- Existing: Go, Cobra CLI, Charm TUI, LLM providers
- New: ACP Go server library (custom minimal implementation or stdlib JSON-RPC)

---

## File Structure

**New ACP Transport Layer:**
- `internal/acp/` — ACP protocol implementation
  - `server.go` — ACP server (JSON-RPC responder, message routing)
  - `types.go` — ACP message types and Rubichan extensions
  - `capabilities.go` — Capability definitions (tools, skills, security)
  - `stdio.go` — stdio JSON-RPC transport (local)
  - `http.go` — HTTP/WS transport (remote, optional Phase 2)
  - `skill_protocol.go` — Skill system extension to ACP
  - `security_protocol.go` — Security verdict extension to ACP

**Mode-Specific Adapters:**
- `internal/modes/` — Mode-specific clients
  - `interactive/acp_client.go` — TUI mode ↔ ACP server adapter
  - `headless/acp_client.go` — CI/CD mode ↔ ACP server adapter
  - `wiki/acp_client.go` — Wiki batch mode ↔ ACP server adapter

**Refactored Agent Core:**
- `internal/agent/agent.go` — Remove direct JSON-RPC (now ACP-only)
- `internal/agent/` — Core logic unchanged, accessed via ACP messages

**Testing:**
- `internal/acp/test/` — ACP server unit tests
- `internal/modes/*/test/` — Mode adapter integration tests
- `test/e2e/acp_*` — End-to-end ACP protocol tests

---

## Tasks

### Task 1.1: Define ACP Message Type Structure

**Files:**
- Create: `internal/acp/types.go`
- Create: `internal/acp/test/types_test.go`

- [ ] **Step 1: Write test for ACP message types**

```go
package acp_test

import (
	"encoding/json"
	"testing"

	"rubichan/internal/acp"
)

func TestACPRequest(t *testing.T) {
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"rubichan-tui"}}`),
	}
	
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	
	var decoded acp.Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	
	if decoded.Method != "initialize" {
		t.Errorf("got %q, want %q", decoded.Method, "initialize")
	}
}

func TestACPResponse(t *testing.T) {
	result := json.RawMessage(`{"status":"ready"}`)
	resp := acp.Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  &result,
	}
	
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	
	var decoded acp.Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	
	if decoded.Result == nil {
		t.Error("result is nil")
	}
}

func TestACPError(t *testing.T) {
	resp := acp.Response{
		JSONRPC: "2.0",
		ID:      1,
		Error: &acp.RPCError{
			Code:    -32601,
			Message: "Method not found",
		},
	}
	
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	
	var decoded acp.Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	
	if decoded.Error == nil {
		t.Error("error is nil")
	}
	if decoded.Error.Code != -32601 {
		t.Errorf("got code %d, want -32601", decoded.Error.Code)
	}
}

func TestToolCapability(t *testing.T) {
	toolCap := acp.ToolCapability{
		Tool: acp.Tool{
			Name:        "file.read",
			Description: "Read file contents",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	}
	
	data, err := json.Marshal(toolCap)
	if err != nil {
		t.Fatal(err)
	}
	
	var decoded acp.ToolCapability
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	
	if decoded.Tool.Name != "file.read" {
		t.Errorf("got %q, want %q", decoded.Tool.Name, "file.read")
	}
}

func TestSkillCapability(t *testing.T) {
	skillCap := acp.SkillCapability{
		Skill: acp.Skill{
			Name:        "my_skill",
			Manifest:    json.RawMessage(`{"version":"1.0","backend":"starlark"}`),
			Permissions: []string{"file:read", "shell:exec"},
		},
	}
	
	data, err := json.Marshal(skillCap)
	if err != nil {
		t.Fatal(err)
	}
	
	var decoded acp.SkillCapability
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	
	if len(decoded.Skill.Permissions) != 2 {
		t.Errorf("got %d permissions, want 2", len(decoded.Skill.Permissions))
	}
}

func TestSecurityVerdict(t *testing.T) {
	verdict := acp.SecurityVerdict{
		ID:         "finding-1",
		Status:     "flagged",
		Severity:   "high",
		Message:    "Hardcoded secret detected",
		Confidence: 0.95,
	}
	
	data, err := json.Marshal(verdict)
	if err != nil {
		t.Fatal(err)
	}
	
	var decoded acp.SecurityVerdict
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	
	if decoded.Severity != "high" {
		t.Errorf("got %q, want %q", decoded.Severity, "high")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/julianshen/prj/rubichan
go test -v ./internal/acp/test -run TestACP
```

Expected: FAIL — package `acp` does not exist

- [ ] **Step 3: Implement ACP types in `internal/acp/types.go`**

```go
package acp

import (
	"encoding/json"
)

// Request is a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (request with no ID).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// --- Capability Blocks (Rubichan Extensions to ACP) ---

// Tool represents a tool the agent can execute.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCapability exposes a tool via ACP.
type ToolCapability struct {
	Tool Tool `json:"tool"`
}

// Skill represents a skill (plugin) available to the agent.
type Skill struct {
	Name        string            `json:"name"`
	Manifest    json.RawMessage   `json:"manifest"`
	Permissions []string          `json:"permissions"`
}

// SkillCapability exposes a skill via ACP.
type SkillCapability struct {
	Skill Skill `json:"skill"`
}

// SecurityVerdict represents a security finding or approval decision.
type SecurityVerdict struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"` // "flagged", "approved", "escalated"
	Severity   string  `json:"severity"` // "critical", "high", "medium", "low", "info"
	Message    string  `json:"message"`
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence,omitempty"`
}

// SecurityCapability exposes security verdicts via ACP.
type SecurityCapability struct {
	Verdict SecurityVerdict `json:"verdict"`
}

// InitializeRequest is the client → server initialize request.
type InitializeRequest struct {
	ClientInfo   ClientInfo `json:"clientInfo"`
	Capabilities []string   `json:"capabilities,omitempty"`
}

// ClientInfo describes the client connecting to the agent.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResponse is the server → client initialize response.
type InitializeResponse struct {
	ServerInfo   ServerInfo              `json:"serverInfo"`
	Capabilities []CapabilityDefinition `json:"capabilities"`
	Methods      []string                `json:"methods"`
}

// ServerInfo describes the Rubichan agent server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// CapabilityDefinition declares a capability supported by the server.
type CapabilityDefinition struct {
	Type string          `json:"type"` // "tool", "skill", "security"
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v ./internal/acp/test -run TestACP
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/acp/types.go internal/acp/test/types_test.go
git commit -m "[STRUCTURAL] Define ACP message types and capability registry"
```

---

### Task 1.2: Define ACP RPC Methods & Error Codes

**Files:**
- Modify: `internal/acp/types.go` (add constants)
- Create: `internal/acp/test/methods_test.go`

- [ ] **Step 1: Write test for RPC method routing**

```go
package acp_test

import (
	"encoding/json"
	"testing"

	"rubichan/internal/acp"
)

func TestListToolsRequest(t *testing.T) {
	msg := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  acp.MethodListTools,
		Params:  nil,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaled acp.Request
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}
	
	if unmarshaled.Method != acp.MethodListTools {
		t.Errorf("got %q, want %q", unmarshaled.Method, acp.MethodListTools)
	}
}

func TestCallToolRequest(t *testing.T) {
	params := json.RawMessage(`{
		"name": "file_read",
		"arguments": {"path": "/tmp/test.txt"}
	}`)

	msg := acp.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  acp.MethodCallTool,
		Params:  params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaled acp.Request
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}
	
	if unmarshaled.Method != acp.MethodCallTool {
		t.Errorf("got %q, want %q", unmarshaled.Method, acp.MethodCallTool)
	}
}

func TestErrorCodeConstants(t *testing.T) {
	if acp.ErrorCodeMethodNotFound != -32601 {
		t.Errorf("got %d, want -32601", acp.ErrorCodeMethodNotFound)
	}
	if acp.ErrorCodeToolNotFound != -32100 {
		t.Errorf("got %d, want -32100", acp.ErrorCodeToolNotFound)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v ./internal/acp/test -run TestList
```

Expected: FAIL — constants not defined

- [ ] **Step 3: Add constants to types.go**

Add after the existing type definitions in `internal/acp/types.go`:

```go
// RPC Methods
const (
	// Tool methods
	MethodListTools = "tools/list"
	MethodCallTool  = "tools/call"

	// Resource methods
	MethodListResources = "resources/list"
	MethodReadResource  = "resources/read"

	// Sampling (LLM) methods
	MethodSampling = "sampling/createMessage"

	// Prompt methods
	MethodListPrompts = "prompts/list"
	MethodCallPrompt  = "prompts/call"

	// Notification methods (server → client, no response expected)
	MethodNotificationProgress = "notifications/progress"
	MethodNotificationLog      = "notifications/log"

	// Rubichan-specific extensions
	MethodListSkills         = "skills/list"
	MethodCallSkill          = "skills/call"
	MethodGetSecurityVerdict = "security/getVerdict"
)

// Error Codes (JSON-RPC standard + custom)
const (
	ErrorCodeParseError      = -32700
	ErrorCodeInvalidRequest  = -32600
	ErrorCodeMethodNotFound  = -32601
	ErrorCodeInvalidParams   = -32602
	ErrorCodeInternalError   = -32603
	ErrorCodeServerError     = -32000
	ErrorCodeToolNotFound    = -32100
	ErrorCodeSkillNotFound   = -32101
	ErrorCodePermissionDenied = -32102
)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v ./internal/acp/test -run TestList
go test -v ./internal/acp/test -run TestError
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/acp/types.go internal/acp/test/methods_test.go
git commit -m "[STRUCTURAL] Define ACP RPC method constants and error codes"
```

---

### Task 1.3: Create Capability Registry

**Files:**
- Create: `internal/acp/capabilities.go`
- Modify: `internal/acp/test/types_test.go` (add registry test)

- [ ] **Step 1: Write test for capability registry**

Add to `internal/acp/test/types_test.go`:

```go
func TestCapabilityRegistry(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	
	tool := acp.Tool{
		Name:        "file.read",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	
	registry.RegisterTool(tool)
	
	caps, err := registry.GetCapabilities()
	if err != nil {
		t.Fatal(err)
	}
	
	if len(caps) == 0 {
		t.Error("expected at least 1 capability")
	}
}

func TestCapabilityRegistryMethods(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	
	callCount := 0
	registry.RegisterMethod("test/ping", func(params json.RawMessage) (json.RawMessage, error) {
		callCount++
		return json.RawMessage(`{"pong":true}`), nil
	})
	
	methods := registry.GetMethods()
	if len(methods) == 0 {
		t.Error("expected methods list")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v ./internal/acp/test -run TestCapability
```

Expected: FAIL — `NewCapabilityRegistry` not defined

- [ ] **Step 3: Implement capability registry in `internal/acp/capabilities.go`**

```go
package acp

import (
	"encoding/json"
	"fmt"
)

// CapabilityRegistry holds all registered capabilities (tools, skills, verdicts).
type CapabilityRegistry struct {
	tools   map[string]Tool
	skills  map[string]Skill
	methods map[string]Handler
}

// Handler is a function that processes an ACP method call.
type Handler func(params json.RawMessage) (json.RawMessage, error)

// NewCapabilityRegistry creates a new registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		tools:   make(map[string]Tool),
		skills:  make(map[string]Skill),
		methods: make(map[string]Handler),
	}
}

// RegisterTool registers a tool capability.
func (cr *CapabilityRegistry) RegisterTool(t Tool) {
	cr.tools[t.Name] = t
}

// RegisterSkill registers a skill capability.
func (cr *CapabilityRegistry) RegisterSkill(s Skill) {
	cr.skills[s.Name] = s
}

// RegisterMethod registers a handler for an ACP method.
func (cr *CapabilityRegistry) RegisterMethod(method string, handler Handler) {
	cr.methods[method] = handler
}

// GetCapabilities returns all capabilities as CapabilityDefinition slice.
func (cr *CapabilityRegistry) GetCapabilities() ([]CapabilityDefinition, error) {
	var caps []CapabilityDefinition

	// Add tool capabilities
	for _, tool := range cr.tools {
		toolCap := ToolCapability{Tool: tool}
		data, err := json.Marshal(toolCap)
		if err != nil {
			return nil, fmt.Errorf("marshal tool capability: %w", err)
		}
		caps = append(caps, CapabilityDefinition{
			Type: "tool",
			Name: tool.Name,
			Data: json.RawMessage(data),
		})
	}

	// Add skill capabilities
	for _, skill := range cr.skills {
		skillCap := SkillCapability{Skill: skill}
		data, err := json.Marshal(skillCap)
		if err != nil {
			return nil, fmt.Errorf("marshal skill capability: %w", err)
		}
		caps = append(caps, CapabilityDefinition{
			Type: "skill",
			Name: skill.Name,
			Data: json.RawMessage(data),
		})
	}

	return caps, nil
}

// GetMethods returns all registered method names.
func (cr *CapabilityRegistry) GetMethods() []string {
	methods := make([]string, 0, len(cr.methods))
	for m := range cr.methods {
		methods = append(methods, m)
	}
	return methods
}

// Call invokes a registered method handler.
func (cr *CapabilityRegistry) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	handler, ok := cr.methods[method]
	if !ok {
		return nil, fmt.Errorf("method not found: %s", method)
	}
	return handler(params)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v ./internal/acp/test -run TestCapability
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/acp/capabilities.go internal/acp/test/types_test.go
git commit -m "[STRUCTURAL] Implement capability registry for tools and methods"
```

---

**More tasks follow in similar pattern...**

This is the start of the plan. Continue with Tasks 1.4+ following the same structure.
