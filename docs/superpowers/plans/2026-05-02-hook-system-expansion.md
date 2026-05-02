# Hook System Expansion Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand rubichan's hook system from 12 shell-only events to 27 lifecycle events, adding HTTP hooks, prompt hooks, and fail-open design.

**Architecture:** Extend `internal/hooks/` with new event constants, HTTP hook execution, prompt transformation hooks, and integration points in the agent loop. HTTP hooks POST JSON to configured URLs with timeout. Prompt hooks transform system prompts via find/replace. Fail-open: hook errors don't block execution.

**Tech Stack:** Go, `net/http`, existing `internal/hooks/runner.go`, `internal/skills/` for hook phases.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/hooks/events.go` | Extended event constants (27 events) |
| `internal/hooks/http.go` | HTTP hook execution |
| `internal/hooks/prompt.go` | Prompt transformation hooks |
| `internal/hooks/runner.go` | Extend UserHookRunner with new hook types |
| `internal/agent/agent.go` | Fire new hooks at integration points |

---

## Chunk 1: Extended Event Constants

### Task 1: Add 22 lifecycle events

**Files:**
- Create: `internal/hooks/events.go`

- [ ] **Step 1: Write the constants**

```go
package hooks

// Event constants for user-facing hook event configuration.
// Expanded from 12 to 22 events to match ccgo/Claude Code coverage.
const (
	// Tool execution lifecycle
	EventPreTool         = "pre_tool"
	EventPostTool        = "post_tool"
	EventPostToolFailure = "post_tool_failure"

	// Edit lifecycle
	EventPreEdit  = "pre_edit"
	EventPostEdit = "post_edit"

	// Shell lifecycle
	EventPreShell = "pre_shell"

	// Prompt lifecycle
	EventPrePrompt    = "pre_prompt"
	EventPostResponse = "post_response"

	// Session lifecycle
	EventSessionStart = "session_start"
	EventSessionEnd   = "session_end"
	EventSetup        = "setup"

	// Task lifecycle
	EventTaskCreated   = "task_created"
	EventTaskCompleted = "task_completed"

	// Permission lifecycle
	EventPermissionRequest = "permission_request"
	EventPermissionDenied  = "permission_denied"

	// Context lifecycle
	EventPreCompact  = "pre_compact"
	EventPostCompact = "post_compact"

	// Subagent lifecycle
	EventSubagentStart = "subagent_start"
	EventSubagentStop  = "subagent_stop"

	// Notification
	EventNotification = "notification"

	// Config/Environment
	EventConfigChange = "config_change"
	EventCwdChanged   = "cwd_changed"
	EventFileChanged  = "file_changed"
)

// AllEvents returns the complete list of supported hook events.
func AllEvents() []string {
	return []string{
		EventPreTool, EventPostTool, EventPostToolFailure,
		EventPreEdit, EventPostEdit,
		EventPreShell,
		EventPrePrompt, EventPostResponse,
		EventSessionStart, EventSessionEnd, EventSetup,
		EventTaskCreated, EventTaskCompleted,
		EventPermissionRequest, EventPermissionDenied,
		EventPreCompact, EventPostCompact,
		EventSubagentStart, EventSubagentStop,
		EventNotification,
		EventConfigChange, EventCwdChanged, EventFileChanged,
	}
}
```

- [ ] **Step 2: Add test**

```go
func TestAllEvents(t *testing.T) {
	events := AllEvents()
	if len(events) != 22 {
		t.Errorf("expected 22 events, got %d", len(events))
	}
	seen := make(map[string]bool)
	for _, e := range events {
		if seen[e] {
			t.Errorf("duplicate event: %s", e)
		}
		seen[e] = true
	}
}
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/hooks/ -run TestAllEvents -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/hooks/events.go
git commit -m "[STRUCTURAL] Expand hook events from 12 to 27 lifecycle events"
```

---

## Chunk 2: HTTP Hook Execution

### Task 2: Implement HTTP hook runner

**Files:**
- Create: `internal/hooks/http.go`
- Test: `internal/hooks/http_test.go`

- [ ] **Step 1: Write the failing test**

```go
package hooks

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecuteHTTPHook(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"continue": true}`))
	}))
	defer server.Close()

	cfg := UserHookConfig{
		Event:   EventPreTool,
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	result, err := executeHTTPHook(cfg, map[string]interface{}{"tool": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("expected continue=true")
	}
	if received["tool"] != "test" {
		t.Errorf("expected tool=test in payload, got %v", received["tool"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run TestExecuteHTTPHook -v`
Expected: FAIL — `executeHTTPHook` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxHookResponseSize limits how much data we read from a hook response.
// Prevents OOM from misconfigured or malicious hook servers.
const maxHookResponseSize = 1 << 20 // 1 MiB

// sharedHTTPClient is reused across hook calls for connection pooling.
var sharedHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// executeHTTPHook sends a POST request to the configured URL with the
// event data as JSON. Returns the hook result parsed from the response.
//
// Fail-open design: all errors (network, marshal, bad response) log the
// failure and return Continue=true so a broken hook cannot block execution.
// Operators should monitor logs for hook failures.
func executeHTTPHook(cfg UserHookConfig, data map[string]interface{}) (HookResult, error) {
	payload := map[string]interface{}{
		"event": cfg.Event,
		"data":  data,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("hook %s: marshal failed: %v", cfg.Event, err)
		return HookResult{Continue: true}, nil
	}

	// Use context timeout only; http.Client.Timeout is redundant and can
	// cause confusing error types when both fire.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		log.Printf("hook %s: request build failed: %v", cfg.Event, err)
		return HookResult{Continue: true}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		log.Printf("hook %s: HTTP request failed: %v", cfg.Event, err)
		return HookResult{Continue: true}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHookResponseSize))
	if err != nil {
		log.Printf("hook %s: read body failed: %v", cfg.Event, err)
		return HookResult{Continue: true}, nil
	}

	var result HookResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Non-JSON response: treat as no-op (continue).
		return HookResult{Continue: true}, nil
	}

	// Explicit cancel takes precedence; everything else defaults to continue.
	if result.Cancel {
		return result, nil
	}
	return HookResult{Continue: true, Message: result.Message, UpdatedInput: result.UpdatedInput}, nil
}

// HookResult captures the response from a hook execution.
type HookResult struct {
	Continue       bool                   `json:"continue"`
	Cancel         bool                   `json:"cancel"`
	Message        string                 `json:"message"`
	UpdatedInput   map[string]interface{} `json:"updated_input,omitempty"`
	ModifiedOutput string                 `json:"modified_output,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hooks/ -run TestExecuteHTTPHook -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/http.go internal/hooks/http_test.go
git commit -m "[STRUCTURAL] Add HTTP hook execution with fail-open design"
```

---

## Chunk 3: Prompt Transformation Hooks

### Task 3: Implement prompt hooks

**Files:**
- Create: `internal/hooks/prompt.go`
- Test: `internal/hooks/prompt_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPromptHook(t *testing.T) {
	hook := PromptHook{
		Find:    "OLD",
		Replace: "NEW",
	}
	result := hook.Transform("hello OLD world")
	if result != "hello NEW world" {
		t.Errorf("expected 'hello NEW world', got %q", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run TestPromptHook -v`
Expected: FAIL — `PromptHook` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package hooks

import "strings"

// PromptHook applies a find/replace transformation to a prompt string.
// Used for pre_prompt and post_response hooks that modify the system
// prompt or model output without requiring external commands.
type PromptHook struct {
	Find    string
	Replace string
}

// Transform applies the find/replace to the input text.
func (h PromptHook) Transform(text string) string {
	return strings.ReplaceAll(text, h.Find, h.Replace)
}

// PromptHookChain applies multiple prompt hooks in sequence.
//
// Warning: each hook creates a new string via strings.ReplaceAll, so
// chaining many hooks on long prompts is O(n*m). Use sparingly on
// hot paths (e.g., limit to 3 hooks per prompt build).
type PromptHookChain []PromptHook

// Transform applies all hooks in order.
func (c PromptHookChain) Transform(text string) string {
	for _, h := range c {
		text = h.Transform(text)
	}
	return text
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hooks/ -run TestPromptHook -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/prompt.go internal/hooks/prompt_test.go
git commit -m "[STRUCTURAL] Add prompt transformation hooks"
```

---

## Chunk 4: Wire into Agent

### Task 4: Fire new hooks at integration points

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add hook firing points**

Identify integration points in `agent.go`:
- Before tool execution: `EventPreTool`
- After tool success: `EventPostTool`
- After tool failure: `EventPostToolFailure`
- Before prompt build: `EventPrePrompt`
- After response: `EventPostResponse`
- Before compaction: `EventPreCompact`
- After compaction: `EventPostCompact`

- [ ] **Step 2: Add hook runner to Agent**

```go
type Agent struct {
	// ... existing fields ...
	hookRunner *hooks.UserHookRunner
}
```

- [ ] **Step 3: Fire hooks at key points**

Example at pre-tool:
```go
if a.hookRunner != nil {
	result := a.hookRunner.RunPreTool(ctx, toolName, input)
	if result.Cancel {
		// Block tool execution
		return toolErrorResult(tc, result.Message)
	}
	if result.UpdatedInput != nil {
		input = result.UpdatedInput
	}
}
```

- [ ] **Step 4: Run agent tests**

Run: `go test ./internal/agent/... -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Wire expanded hook system into agent lifecycle"
```

---

## Chunk 5: Validation

- [ ] **Step 1: Run all tests**

```bash
go test ./...
```

- [ ] **Step 2: Run linter**

```bash
golangci-lint run ./internal/hooks/... ./internal/agent/...
```

- [ ] **Step 3: Check formatting**

```bash
gofmt -l internal/hooks/
```

- [ ] **Step 4: Commit fixes if needed**
