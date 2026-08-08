# A2A Protocol Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add A2A (Agent2Agent) protocol support to rubichan in three phases — client (discover/call remote agents), server (expose rubichan as a callable agent), orchestration (registry + capability-based dispatch) — per `docs/superpowers/specs/2026-08-08-a2a-protocol-support-design.md`.

**Architecture:** New `internal/a2a` package holds all protocol-level code (types, client, server, task storage adapter, agent card builder, registry). New `internal/tools/a2a` package exposes `a2a_discover`, `a2a_call`, `a2a_dispatch` as ordinary tools in the existing registry. A new `rubichan a2a serve` subcommand starts the Phase 2 HTTP server, driving a real `internal/agent.Agent`.

**Tech Stack:** Go stdlib `net/http` (custom client/server, no vendor A2A SDK, matching `internal/tools/mcp` and `internal/provider`'s existing no-vendor-SDK convention), `modernc.org/sqlite` via the existing `internal/store.Store` (no new DB connection), `spf13/cobra` for the CLI subcommand.

## Global Constraints

- TDD strictly: one test at a time, red-green-refactor-commit. Run `go test ./...` after every change.
- Commit prefixes: `[STRUCTURAL]` (no behavior change) or `[BEHAVIORAL]` (new feature/logic change). Never mix both in one commit.
- Target >90% test coverage on all new code (`go test -cover`).
- `gofmt -l .` must report nothing before any commit.
- No vendor A2A SDK dependency — implement the HTTP+JSON-RPC+SSE binding directly, matching `internal/tools/mcp`'s existing custom MCP client and `internal/provider`'s custom per-provider HTTP+SSE clients (ADR-006).
- `internal/a2a` does not import `internal/acp` and vice versa — they are independent packages with independent JSON-RPC type declarations, per the spec's "Relationship to internal/acp" section.
- **Deviation from the spec's literal file layout, decided during planning:** the spec describes a standalone `internal/a2a/sqlitetaskstore.go` owning its own SQLite connection. Codebase exploration (`internal/store/store.go`) found that all of rubichan's persistence already lives in one `internal/store.Store` type wrapping a single `*sql.DB` (used today for sessions, messages, memories, approvals, etc.), with a precedent (`session_snapshots.messages`) for storing a JSON blob in a `TEXT` column. This plan adds the `a2a_tasks` table and CRUD methods to `internal/store/store.go` instead of opening a second SQLite connection, and implements `internal/a2a`'s `TaskStore` interface with a thin adapter over `*store.Store`. This satisfies the spec's actual requirement ("same database file already used for conversation persistence") while following the codebase's established one-`Store` pattern. The `TaskStore` interface itself — the part other code depends on — is unchanged from the spec.
- Task lifecycle states use the exact lowercase-hyphenated strings from the A2A spec: `submitted`, `working`, `input-required`, `auth-required`, `completed`, `failed`, `canceled`, `rejected`.

---

## Phase 1: Client

### Task 1: Protocol types

**Files:**
- Create: `internal/a2a/types.go`
- Test: `internal/a2a/types_test.go`

**Interfaces:**
- Produces: `AgentCard`, `Skill`, `Capabilities`, `AuthScheme`, `TaskState` (and its 8 constants), `Task`, `Message`, `Part`, `Artifact`, `RPCRequest`, `RPCResponse`, `RPCError` — every later task in this plan depends on these exact names and fields.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskJSONRoundTrip(t *testing.T) {
	task := Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		State:     TaskCompleted,
		Messages: []Message{
			{Role: "agent", Parts: []Part{{Kind: "text", Text: "hello"}}},
		},
		Artifacts: []Artifact{
			{Name: "result", Parts: []Part{{Kind: "text", Text: "42"}}},
		},
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	var decoded Task
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, task, decoded)
	assert.Contains(t, string(data), `"state":"completed"`)
}

func TestTaskStateConstants(t *testing.T) {
	assert.Equal(t, TaskState("submitted"), TaskSubmitted)
	assert.Equal(t, TaskState("working"), TaskWorking)
	assert.Equal(t, TaskState("input-required"), TaskInputRequired)
	assert.Equal(t, TaskState("auth-required"), TaskAuthRequired)
	assert.Equal(t, TaskState("completed"), TaskCompleted)
	assert.Equal(t, TaskState("failed"), TaskFailed)
	assert.Equal(t, TaskState("canceled"), TaskCanceled)
	assert.Equal(t, TaskState("rejected"), TaskRejected)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run 'TestTaskJSONRoundTrip|TestTaskStateConstants' -v`
Expected: FAIL — `package a2a` does not exist / undefined types.

- [ ] **Step 3: Write minimal implementation**

```go
// Package a2a implements the client and server sides of Google's A2A
// (Agent2Agent) protocol: HTTP + JSON-RPC 2.0, with SSE for streaming.
// It is independent of internal/acp (Anthropic's unrelated Agent Client
// Protocol, a persistent bidirectional stream) — the two protocols use
// different transports and this package declares its own JSON-RPC types
// rather than sharing acp's.
package a2a

import "encoding/json"

// AgentCard is the "business card" an A2A agent serves at
// /.well-known/agent.json, advertising what it can do.
type AgentCard struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	URL          string       `json:"url"`
	Skills       []Skill      `json:"skills"`
	Capabilities Capabilities `json:"capabilities"`
	Auth         []AuthScheme `json:"authentication,omitempty"`
}

// Skill describes one capability an agent advertises.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

// Capabilities describes optional protocol features an agent supports.
type Capabilities struct {
	Streaming         bool `json:"streaming,omitempty"`
	PushNotifications bool `json:"pushNotifications,omitempty"`
}

// AuthScheme describes one authentication scheme an agent accepts.
type AuthScheme struct {
	Scheme string `json:"scheme"` // e.g. "bearer"
}

// TaskState is one state in a Task's lifecycle.
type TaskState string

const (
	TaskSubmitted     TaskState = "submitted"
	TaskWorking       TaskState = "working"
	TaskInputRequired TaskState = "input-required"
	TaskAuthRequired  TaskState = "auth-required"
	TaskCompleted     TaskState = "completed"
	TaskFailed        TaskState = "failed"
	TaskCanceled      TaskState = "canceled"
	TaskRejected      TaskState = "rejected"
)

// Task is the unit of work in A2A: a request that moves through
// TaskState until it reaches a terminal state.
type Task struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId"`
	State     TaskState  `json:"state"`
	Messages  []Message  `json:"history,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// Message is one turn of communication, sent by "user" or "agent".
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part is a tagged union over A2A's part kinds. Exactly one of Text,
// DataJSON, or (FileURL or FileBytes) is populated, selected by Kind.
type Part struct {
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	DataJSON  json.RawMessage `json:"data,omitempty"`
	FileURL   string          `json:"fileUrl,omitempty"`
	FileBytes []byte          `json:"fileBytes,omitempty"`
}

// Artifact is a named result produced by a Task.
type Artifact struct {
	Name  string `json:"name"`
	Parts []Part `json:"parts"`
}

// RPCRequest is a JSON-RPC 2.0 request envelope.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse is a JSON-RPC 2.0 response envelope.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -run 'TestTaskJSONRoundTrip|TestTaskStateConstants' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/types.go internal/a2a/types_test.go
git commit -m "[BEHAVIORAL] Add A2A protocol data types"
```

---

### Task 2: Client — Discover

**Files:**
- Create: `internal/a2a/client.go`
- Test: `internal/a2a/client_test.go`

**Interfaces:**
- Consumes: `AgentCard` (Task 1).
- Produces: `Client` struct, `NewClient(baseURL, apiKey string, httpClient *http.Client) *Client`, `(*Client) Discover(ctx context.Context) (*AgentCard, error)`. Later tasks (3, 4, 9) add more methods to the same `Client` type and construct it the same way.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"context"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientDiscover(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [{"id": "echo", "name": "Echo", "description": "echoes input"}],
			"capabilities": {"streaming": true}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	card, err := client.Discover(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-agent", card.Name)
	assert.Equal(t, "echo", card.Skills[0].ID)
	assert.True(t, card.Capabilities.Streaming)
}

func TestClientDiscoverNotFound(t *testing.T) {
	handler := http.NewServeMux()
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Discover(context.Background())
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestClientDiscover -v`
Expected: FAIL — `NewClient` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client calls a remote A2A agent over HTTP + JSON-RPC 2.0.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a Client for the agent at baseURL. apiKey, if non-empty,
// is sent as a Bearer token on every request. httpClient defaults to
// http.DefaultClient if nil.
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient, baseURL: baseURL, apiKey: apiKey}
}

// Discover fetches and parses {baseURL}/.well-known/agent.json.
func (c *Client) Discover(ctx context.Context) (*AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/.well-known/agent.json", nil)
	if err != nil {
		return nil, fmt.Errorf("build discover request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch agent card: unexpected status %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("parse agent card: %w", err)
	}
	return &card, nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -run TestClientDiscover -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/client.go internal/a2a/client_test.go
git commit -m "[BEHAVIORAL] Add A2A client Discover"
```

---

### Task 3: Client — Send, GetTask, CancelTask

**Files:**
- Modify: `internal/a2a/client.go`
- Test: `internal/a2a/client_test.go`

**Interfaces:**
- Consumes: `Client`, `RPCRequest`/`RPCResponse`/`RPCError` (Task 1), `Message`/`Task` (Task 1).
- Produces: `(*Client) Send(ctx, msg Message) (*Task, error)`, `(*Client) GetTask(ctx, taskID string) (*Task, error)`, `(*Client) CancelTask(ctx, taskID string) error`, and the shared helper `(*Client) call(ctx, method string, params, result any) error` that Task 4's `Stream` also uses.

- [ ] **Step 1: Write the failing test**

```go
func TestClientSendReturnsTask(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "message/send", req.Method)

		resp := RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mustMarshal(t, Task{
				ID: "task-1", State: TaskCompleted,
				Artifacts: []Artifact{{Name: "result", Parts: []Part{{Kind: "text", Text: "done"}}}},
			}),
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	task, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}})
	require.NoError(t, err)

	assert.Equal(t, TaskCompleted, task.State)
	assert.Equal(t, "done", task.Artifacts[0].Parts[0].Text)
}

func TestClientSendRPCError(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		resp := RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32000, Message: "boom"}}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestClientGetTaskAndCancelTask(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		var result json.RawMessage
		switch req.Method {
		case "tasks/get":
			result = mustMarshal(t, Task{ID: "task-1", State: TaskWorking})
		case "tasks/cancel":
			result = mustMarshal(t, Task{ID: "task-1", State: TaskCanceled})
		}
		resp := RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())

	task, err := client.GetTask(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskWorking, task.State)

	require.NoError(t, client.CancelTask(context.Background(), "task-1"))
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run 'TestClientSend|TestClientGetTaskAndCancelTask' -v`
Expected: FAIL — `Send`/`GetTask`/`CancelTask` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/a2a/client.go`:

```go
import (
	"bytes"
	"sync/atomic"
	// ...existing imports
)

var clientNextID int64 // shared across Clients; only uniqueness within a process matters

// call sends a JSON-RPC request to method with params, decodes the result
// into result (a pointer), and returns any RPC-level error as a Go error.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params for %s: %w", method, err)
		}
		rawParams = data
	}

	id := atomic.AddInt64(&clientNextID, 1)
	body, err := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: rawParams})
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request for %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer resp.Body.Close()

	var rpcResp RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("decode response for %s: %w", method, err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("%s failed: %s (code %d)", method, rpcResp.Error.Message, rpcResp.Error.Code)
	}
	if result != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("unmarshal result for %s: %w", method, err)
		}
	}
	return nil
}

// Send makes a message/send call and returns the resulting Task. If the
// task isn't terminal yet, the caller polls GetTask.
func (c *Client) Send(ctx context.Context, msg Message) (*Task, error) {
	var task Task
	if err := c.call(ctx, "message/send", struct {
		Message Message `json:"message"`
	}{Message: msg}, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTask fetches the current state of a previously created task.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	var task Task
	if err := c.call(ctx, "tasks/get", struct {
		ID string `json:"id"`
	}{ID: taskID}, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// CancelTask requests cancellation of a running task.
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	return c.call(ctx, "tasks/cancel", struct {
		ID string `json:"id"`
	}{ID: taskID}, nil)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS (all tests so far)

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/client.go internal/a2a/client_test.go
git commit -m "[BEHAVIORAL] Add A2A client Send, GetTask, CancelTask"
```

---

### Task 4: Client — Streaming (SSE)

**Files:**
- Create: `internal/a2a/sse.go`
- Modify: `internal/a2a/client.go`
- Test: `internal/a2a/sse_test.go`, `internal/a2a/client_test.go`

**Interfaces:**
- Consumes: `Task` (Task 1), `Client` (Tasks 2-3).
- Produces: `parseSSEEvents(body io.Reader) <-chan json.RawMessage` (internal helper other files don't need, but Task 12's server also imports the "event: message\ndata: ...\n\n" framing convention this establishes), `(*Client) Stream(ctx, msg Message) (<-chan Task, error)`.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSSEEvents(t *testing.T) {
	body := "event: message\ndata: {\"a\":1}\n\nevent: message\ndata: {\"a\":2}\n\n"
	ch := parseSSEEvents(strings.NewReader(body))

	first := <-ch
	assert.JSONEq(t, `{"a":1}`, string(first))
	second := <-ch
	assert.JSONEq(t, `{"a":2}`, string(second))

	_, ok := <-ch
	assert.False(t, ok, "channel should close after the stream ends")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestParseSSEEvents -v`
Expected: FAIL — `parseSSEEvents` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2a

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// parseSSEEvents reads Server-Sent Events framed as "event: message\ndata:
// <json>\n\n" blocks and emits each event's data payload. It follows the
// same line-scanning shape as internal/tools/mcp's SSE transport
// (event:/data: prefix detection over a bufio.Scanner) adapted for A2A's
// single-response-stream model: no separate "endpoint" handshake event,
// every "message" event carries one JSON-RPC-wrapped Task snapshot.
func parseSSEEvents(body io.Reader) <-chan json.RawMessage {
	out := make(chan json.RawMessage, 8)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		var eventType string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if eventType == "message" {
					out <- json.RawMessage(strings.TrimPrefix(line, "data: "))
				}
				eventType = ""
			}
		}
	}()
	return out
}
```

Add to `internal/a2a/client.go`:

```go
// Stream makes a message/stream call and returns a channel of Task
// snapshots, closed when the task reaches a terminal state, the stream
// ends, or ctx is done.
func (c *Client) Stream(ctx context.Context, msg Message) (<-chan Task, error) {
	rawParams, err := json.Marshal(struct {
		Message Message `json:"message"`
	}{Message: msg})
	if err != nil {
		return nil, fmt.Errorf("marshal stream params: %w", err)
	}
	id := atomic.AddInt64(&clientNextID, 1)
	body, err := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: id, Method: "message/stream", Params: rawParams})
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("open stream: unexpected status %d", resp.StatusCode)
	}

	events := parseSSEEvents(resp.Body)
	out := make(chan Task)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		for {
			select {
			case <-ctx.Done():
				drainSSEEvents(events)
				return
			case raw, ok := <-events:
				if !ok {
					return
				}
				var task Task
				if json.Unmarshal(raw, &task) != nil {
					continue
				}
				select {
				case out <- task:
				case <-ctx.Done():
					drainSSEEvents(events)
					return
				}
				switch task.State {
				case TaskCompleted, TaskFailed, TaskCanceled, TaskRejected:
					return
				}
			}
		}
	}()
	return out, nil
}

// drainSSEEvents keeps receiving from events, discarding every value,
// until the channel closes. parseSSEEvents' producer goroutine may be
// blocked mid-send on this channel (its buffer is small relative to how
// much a single bufio.Scanner read can pull off the wire); abandoning the
// channel here without draining would leave that goroutine parked forever,
// since closing resp.Body only unblocks a goroutine blocked on reading the
// body, not one blocked on a channel send. Draining lets the producer's
// send succeed, so it resumes scanning, exhausts the (finite) body, and
// closes the channel itself.
func drainSSEEvents(events <-chan json.RawMessage) {
	for range events {
	}
}
```

**Pitfall avoided here:** the `events` channel from `parseSSEEvents` is
only buffered 8 deep, but a single `bufio.Scanner.Scan()` call can pull
far more than that off the wire in one burst read. If the forwarding
goroutine above just `return`s on `ctx.Done()` without draining, and the
parser has already filled the buffer and is blocked on its next
`out <- ...` send, that send blocks forever — closing `resp.Body` only
unblocks a goroutine blocked reading the body, not one blocked on a
channel send, so the parser goroutine leaks permanently. `drainSSEEvents`
keeps consuming (and discarding) events after cancellation so the blocked
producer's send succeeds, it resumes scanning, exhausts the body, and
exits normally.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/sse.go internal/a2a/sse_test.go internal/a2a/client.go internal/a2a/client_test.go
git commit -m "[BEHAVIORAL] Add A2A client streaming via SSE"
```

---

### Task 5: Config — A2AConfig

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `A2AConfig{Agents []A2AAgentConfig, ServeAddr string, RequireAuth bool, APIKeySource string, APIKey string}`, `A2AAgentConfig{Name, URL, APIKeySource, APIKey string}` on `Config.A2A`. Task 9's `a2a_call` tool and Task 14's `serve` subcommand read this.

- [ ] **Step 1: Write the failing test**

```go
func TestDefaultConfigA2A(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.Equal(t, "127.0.0.1:9910", cfg.A2A.ServeAddr)
	assert.True(t, cfg.A2A.RequireAuth)
	assert.Empty(t, cfg.A2A.Agents)
}

func TestLoadA2AAgents(t *testing.T) {
	t.Parallel()

	tomlContent := `
[[a2a.agents]]
name = "research-bot"
url = "https://example.com"
api_key_source = "env"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.Len(t, cfg.A2A.Agents, 1)
	assert.Equal(t, "research-bot", cfg.A2A.Agents[0].Name)
	assert.Equal(t, "https://example.com", cfg.A2A.Agents[0].URL)
	assert.Equal(t, "env", cfg.A2A.Agents[0].APIKeySource)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run 'TestDefaultConfigA2A|TestLoadA2AAgents' -v`
Expected: FAIL — `cfg.A2A` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/config/config.go`, inside the `Config` struct's field list (alongside `MCP MCPConfig`):

```go
	A2A A2AConfig `toml:"a2a"`
```

Add new types near `MCPConfig`/`MCPServerConfig`:

```go
// A2AConfig holds settings for A2A (Agent2Agent) protocol support:
// which remote agents this instance knows about, and how it serves
// itself as an agent when `rubichan a2a serve` runs.
type A2AConfig struct {
	Agents       []A2AAgentConfig `toml:"agents"`
	ServeAddr    string           `toml:"serve_addr"`
	RequireAuth  bool             `toml:"require_auth"`
	APIKeySource string           `toml:"api_key_source"`
	APIKey       string           `toml:"api_key"`
}

// A2AAgentConfig describes one remote A2A agent this instance can call.
type A2AAgentConfig struct {
	Name         string `toml:"name"`
	URL          string `toml:"url"`
	APIKeySource string `toml:"api_key_source"`
	APIKey       string `toml:"api_key"`
}
```

In `DefaultConfig()`, add to the returned `&Config{...}` literal:

```go
		A2A: A2AConfig{
			ServeAddr:   "127.0.0.1:9910",
			RequireAuth: true,
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "[BEHAVIORAL] Add A2A config section"
```

---

### Task 6: Store — a2a_tasks table and CRUD methods

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `(*Store) CreateA2ATask(role, id, contextID, state, data string) error`, `(*Store) GetA2ATask(role, id string) (data string, state string, err error)`, `(*Store) UpdateA2ATask(role, id, state, data string) error`. Task 7's `TaskStore` adapter is the only caller — `data` is a caller-supplied JSON blob (the full marshaled `a2a.Task`), matching the existing `session_snapshots.messages` JSON-in-TEXT-column precedent. `internal/store` does not import `internal/a2a`; it only stores/returns opaque strings.

- [ ] **Step 1: Write the failing test**

```go
func TestA2ATaskCRUD(t *testing.T) {
	store, err := NewStore(":memory:")
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.CreateA2ATask("client", "task-1", "ctx-1", "submitted", `{"id":"task-1"}`))

	data, state, err := store.GetA2ATask("client", "task-1")
	require.NoError(t, err)
	assert.Equal(t, "submitted", state)
	assert.JSONEq(t, `{"id":"task-1"}`, data)

	require.NoError(t, store.UpdateA2ATask("client", "task-1", "completed", `{"id":"task-1","state":"completed"}`))

	data, state, err = store.GetA2ATask("client", "task-1")
	require.NoError(t, err)
	assert.Equal(t, "completed", state)
	assert.JSONEq(t, `{"id":"task-1","state":"completed"}`, data)
}

func TestA2ATaskGetNotFound(t *testing.T) {
	store, err := NewStore(":memory:")
	require.NoError(t, err)
	defer store.Close()

	_, _, err = store.GetA2ATask("client", "missing")
	assert.Error(t, err)
}

func TestA2ATaskRoleIsolation(t *testing.T) {
	store, err := NewStore(":memory:")
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.CreateA2ATask("server", "task-1", "ctx-1", "working", `{}`))
	require.NoError(t, store.CreateA2ATask("client", "task-1", "ctx-1", "submitted", `{}`))

	_, state, err := store.GetA2ATask("server", "task-1")
	require.NoError(t, err)
	assert.Equal(t, "working", state)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/... -run TestA2ATask -v`
Expected: FAIL — `CreateA2ATask` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to the `stmts` slice inside `createTables` in `internal/store/store.go`:

```go
		`CREATE TABLE IF NOT EXISTS a2a_tasks (
			role       TEXT NOT NULL,
			id         TEXT NOT NULL,
			context_id TEXT NOT NULL,
			state      TEXT NOT NULL,
			data       TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (role, id)
		)`,
```

Add new methods at the end of `internal/store/store.go`:

```go
// CreateA2ATask records a new A2A task. role distinguishes tasks this
// instance is executing ("server") from tasks it's waiting on as a
// caller ("client"), so both sides can share the table without their
// task IDs colliding. data is the caller's full JSON-marshaled task
// snapshot, stored opaquely.
func (s *Store) CreateA2ATask(role, id, contextID, state, data string) error {
	_, err := s.db.Exec(
		`INSERT INTO a2a_tasks (role, id, context_id, state, data) VALUES (?, ?, ?, ?, ?)`,
		role, id, contextID, state, data,
	)
	if err != nil {
		return fmt.Errorf("create a2a task: %w", err)
	}
	return nil
}

// GetA2ATask returns the stored data blob and state for a task.
func (s *Store) GetA2ATask(role, id string) (data string, state string, err error) {
	err = s.db.QueryRow(
		`SELECT data, state FROM a2a_tasks WHERE role = ? AND id = ?`, role, id,
	).Scan(&data, &state)
	if err != nil {
		return "", "", fmt.Errorf("get a2a task: %w", err)
	}
	return data, state, nil
}

// UpdateA2ATask overwrites a task's state and data blob.
func (s *Store) UpdateA2ATask(role, id, state, data string) error {
	_, err := s.db.Exec(
		`UPDATE a2a_tasks SET state = ?, data = ?, updated_at = datetime('now') WHERE role = ? AND id = ?`,
		state, data, role, id,
	)
	if err != nil {
		return fmt.Errorf("update a2a task: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "[BEHAVIORAL] Add a2a_tasks table and CRUD to Store"
```

---

### Task 7: TaskStore interface and Store-backed adapter

**Files:**
- Create: `internal/a2a/taskstore.go`
- Test: `internal/a2a/taskstore_test.go`

**Interfaces:**
- Consumes: `Task` (Task 1), `(*store.Store) CreateA2ATask/GetA2ATask/UpdateA2ATask` (Task 6).
- Produces: `TaskStore` interface (`Create`, `Get`, `Update`), `NewStoreTaskStore(s *store.Store, role string) TaskStore`. Task 9's `a2a_call` tool constructs one with role `"client"`; Task 11's server constructs one with role `"server"`.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreTaskStoreCreateGetUpdate(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ts := NewStoreTaskStore(s, "client")
	ctx := context.Background()

	task := Task{ID: "task-1", ContextID: "ctx-1", State: TaskSubmitted}
	require.NoError(t, ts.Create(ctx, task))

	got, err := ts.Get(ctx, "task-1")
	require.NoError(t, err)
	assert.Equal(t, task, got)

	task.State = TaskCompleted
	require.NoError(t, ts.Update(ctx, task))

	got, err = ts.Get(ctx, "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, got.State)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestStoreTaskStore -v`
Expected: FAIL — `NewStoreTaskStore` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2a

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/store"
)

// TaskStore persists Task state so a crash mid-poll or mid-execution is
// recoverable rather than silently forgetten. Server and client sides
// each own their own TaskStore instance (see NewStoreTaskStore's role
// parameter).
type TaskStore interface {
	Create(ctx context.Context, t Task) error
	Get(ctx context.Context, id string) (Task, error)
	Update(ctx context.Context, t Task) error
}

// storeTaskStore implements TaskStore over the shared internal/store.Store
// database, rather than opening a second SQLite connection. role is
// "server" or "client", keeping the two sides' task IDs from colliding
// in the shared a2a_tasks table.
type storeTaskStore struct {
	store *store.Store
	role  string
}

// NewStoreTaskStore creates a TaskStore backed by s, scoped to role.
func NewStoreTaskStore(s *store.Store, role string) TaskStore {
	return &storeTaskStore{store: s, role: role}
}

func (t *storeTaskStore) Create(_ context.Context, task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	return t.store.CreateA2ATask(t.role, task.ID, task.ContextID, string(task.State), string(data))
}

func (t *storeTaskStore) Get(_ context.Context, id string) (Task, error) {
	data, _, err := t.store.GetA2ATask(t.role, id)
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return Task{}, fmt.Errorf("unmarshal task: %w", err)
	}
	return task, nil
}

func (t *storeTaskStore) Update(_ context.Context, task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	return t.store.UpdateA2ATask(t.role, task.ID, string(task.State), string(data))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/taskstore.go internal/a2a/taskstore_test.go
git commit -m "[BEHAVIORAL] Add TaskStore backed by the shared Store"
```

---

### Task 8: Tool — a2a_discover

**Files:**
- Create: `internal/tools/a2a/discover_tool.go`
- Test: `internal/tools/a2a/discover_tool_test.go`

**Interfaces:**
- Consumes: `agentsdk.Tool` interface, `agentsdk.ToolResult` (`pkg/agentsdk/tool.go`), `a2a.Client`/`a2a.NewClient`/`a2a.AgentCard` (Tasks 1-2), `config.A2AAgentConfig` (Task 5).
- Produces: `DiscoverTool` struct, `NewDiscoverTool(cfg *config.Config) *DiscoverTool`. Registered under tool name `"a2a_discover"`.

- [ ] **Step 1: Write the failing test**

```go
package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverToolByRawURL(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"remote","description":"d","skills":[{"id":"echo","name":"Echo","description":"e"}]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	tool := NewDiscoverTool(config.DefaultConfig())
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "remote")
	assert.Contains(t, result.Content, "echo")
}

func TestDiscoverToolByConfiguredName(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"research-bot","description":"d","skills":[]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "research-bot", URL: srv.URL}}
	tool := NewDiscoverTool(cfg)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": "research-bot"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "research-bot")
}

func TestDiscoverToolUnknownName(t *testing.T) {
	tool := NewDiscoverTool(config.DefaultConfig())
	input, err := json.Marshal(map[string]string{"agent": "not-configured"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/a2a/... -v`
Expected: FAIL — package/`NewDiscoverTool` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package a2atools exposes A2A (Agent2Agent) protocol operations as
// ordinary tools in rubichan's tool registry, mirroring internal/tools/mcp's
// tool_wrapper.go — the agent loop sees a2a_discover/a2a_call/a2a_dispatch
// as tools like any other, with no special-cased handling.
package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DiscoverTool fetches an A2A agent's Agent Card.
type DiscoverTool struct {
	cfg        *config.Config
	httpClient *http.Client // overridable in tests; nil means a2a.NewClient's own default
}

// NewDiscoverTool creates a DiscoverTool that resolves configured agent
// names against cfg.A2A.Agents.
func NewDiscoverTool(cfg *config.Config) *DiscoverTool {
	return &DiscoverTool{cfg: cfg}
}

func (d *DiscoverTool) Name() string { return "a2a_discover" }

func (d *DiscoverTool) Description() string {
	return "Fetch an A2A (Agent2Agent) agent's Agent Card, listing its name, " +
		"description, and skills. Pass a configured agent name or a raw URL."
}

func (d *DiscoverTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent": {
				"type": "string",
				"description": "A configured agent name (from [[a2a.agents]]) or a raw base URL"
			}
		},
		"required": ["agent"]
	}`)
}

type discoverInput struct {
	Agent string `json:"agent"`
}

// resolveAgentURL resolves ref against cfg.A2A.Agents by name, falling back
// to treating ref as a raw URL. apiKey is empty unless ref matched a
// configured entry with credentials.
func resolveAgentURL(cfg *config.Config, ref string) (url, apiKey string, err error) {
	for _, a := range cfg.A2A.Agents {
		if a.Name == ref {
			// config.OpenAICompatibleEnvVar's name-to-env-var transform
			// (uppercase, non-alnum -> underscore, "_API_KEY" suffix) isn't
			// actually OpenAI-specific — it's how this codebase derives a
			// canonical env var name from any configured entry name, so
			// "research-bot" resolves against RESEARCH_BOT_API_KEY the same
			// way an OpenAI-compatible entry named "openrouter" resolves
			// against OPENROUTER_API_KEY. Without this, an agent configured
			// with api_key_source = "env" would always fail to resolve: an
			// empty env var name makes config.ResolveAPIKey's "env" case
			// reject immediately, regardless of what's actually set in the
			// environment.
			key, kerr := config.ResolveAPIKey(a.APIKeySource, a.APIKey, config.OpenAICompatibleEnvVar(a.Name))
			if kerr != nil {
				key = "" // credential resolution failing is not fatal for discovery
			}
			return a.URL, key, nil
		}
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "mem://") {
		return ref, "", nil
	}
	return "", "", fmt.Errorf("agent %q is not a configured [[a2a.agents]] name and not a URL", ref)
}

func (d *DiscoverTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in discoverInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	url, apiKey, err := resolveAgentURL(d.cfg, in.Agent)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, d.httpClient)
	card, err := client.Discover(ctx)
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("discover failed: %v", err), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Agent: %s\nDescription: %s\nSkills:\n", card.Name, card.Description)
	for _, s := range card.Skills {
		fmt.Fprintf(&b, "- %s (%s): %s\n", s.Name, s.ID, s.Description)
	}
	return agentsdk.ToolResult{Content: b.String()}, nil
}
```

This matches `agentsdk.Tool`'s `Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)` signature exactly (`pkg/agentsdk/tool.go`), the same pattern `tools.NewNotesTool` already uses in `internal/tools/notes.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/a2a/discover_tool.go internal/tools/a2a/discover_tool_test.go
git commit -m "[BEHAVIORAL] Add a2a_discover tool"
```

---

### Task 9: Tool — a2a_call

**Files:**
- Create: `internal/tools/a2a/call_tool.go`
- Test: `internal/tools/a2a/call_tool_test.go`

**Interfaces:**
- Consumes: `a2a.Client.Send`/`GetTask` (Task 3), `a2a.TaskStore`/`NewStoreTaskStore` (Task 7), `resolveAgentURL` (Task 8), `store.Store` (Task 6).
- Produces: `CallTool` struct, `NewCallTool(cfg *config.Config, s *store.Store) *CallTool`. Registered under tool name `"a2a_call"`.

- [ ] **Step 1: Write the failing test**

```go
package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallToolSyncCompletion(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req a2a.RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		result, _ := json.Marshal(a2a.Task{
			ID: "task-1", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Name: "result", Parts: []a2a.Part{{Kind: "text", Text: "42"}}}},
		})
		resp := a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := config.DefaultConfig()
	tool := NewCallTool(cfg, s)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "what is 6*7?"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "42")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/a2a/... -run TestCallTool -v`
Expected: FAIL — `NewCallTool` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// CallTool sends a message to a remote A2A agent and returns its result,
// polling if the task doesn't complete synchronously.
type CallTool struct {
	cfg        *config.Config
	tasks      a2a.TaskStore
	httpClient *http.Client
	pollEvery  time.Duration // overridable in tests
	pollFor    time.Duration
}

// NewCallTool creates a CallTool. s backs the client-side TaskStore
// (role "client") so a crash mid-poll is resumable.
func NewCallTool(cfg *config.Config, s *store.Store) *CallTool {
	return &CallTool{
		cfg:       cfg,
		tasks:     a2a.NewStoreTaskStore(s, "client"),
		pollEvery: 500 * time.Millisecond,
		pollFor:   2 * time.Minute,
	}
}

func (c *CallTool) Name() string { return "a2a_call" }

func (c *CallTool) Description() string {
	return "Send a message to a remote A2A (Agent2Agent) agent and return its result. " +
		"Pass a configured agent name or a raw URL."
}

func (c *CallTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent": {"type": "string", "description": "A configured agent name or a raw base URL"},
			"message": {"type": "string", "description": "The message to send"}
		},
		"required": ["agent", "message"]
	}`)
}

type callInput struct {
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

func (c *CallTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in callInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	url, apiKey, err := resolveAgentURL(c.cfg, in.Agent)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, c.httpClient)
	msg := a2a.Message{Role: "user", Parts: []a2a.Part{{Kind: "text", Text: in.Message}}}

	task, err := client.Send(ctx, msg)
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("call failed: %v", err), IsError: true}, nil
	}
	_ = c.tasks.Create(ctx, *task)

	deadline := time.Now().Add(c.pollFor)
	for !isTerminal(task.State) && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return agentsdk.ToolResult{Content: "call canceled", IsError: true}, nil
		case <-time.After(c.pollEvery):
		}
		task, err = client.GetTask(ctx, task.ID)
		if err != nil {
			return agentsdk.ToolResult{Content: fmt.Sprintf("poll failed: %v", err), IsError: true}, nil
		}
		_ = c.tasks.Update(ctx, *task)
	}

	if task.State != a2a.TaskCompleted {
		return agentsdk.ToolResult{Content: fmt.Sprintf("task ended in state %q: %s", task.State, task.Error), IsError: true}, nil
	}

	var b strings.Builder
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			b.WriteString(p.Text)
		}
	}
	return agentsdk.ToolResult{Content: b.String()}, nil
}

func isTerminal(s a2a.TaskState) bool {
	switch s {
	case a2a.TaskCompleted, a2a.TaskFailed, a2a.TaskCanceled, a2a.TaskRejected:
		return true
	default:
		return false
	}
}
```

Also fix Task 8's `discover_tool.go`: replace the draft `a2atoolsResult` type and `Execute` signature with `agentsdk.ToolResult` / `(agentsdk.ToolResult, error)`, importing `"github.com/julianshen/rubichan/pkg/agentsdk"`, exactly as this task's `call_tool.go` does — the two files must compile together in the same package.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/a2a/call_tool.go internal/tools/a2a/call_tool_test.go internal/tools/a2a/discover_tool.go
git commit -m "[BEHAVIORAL] Add a2a_call tool with resumable polling"
```

---

## Phase 2: Server

### Task 10: Agent Card builder

**Files:**
- Create: `internal/a2a/agentcard.go`
- Test: `internal/a2a/agentcard_test.go`

**Interfaces:**
- Consumes: `AgentCard`/`Skill`/`Capabilities` (Task 1), `*tools.Registry` (`pkg/agentsdk.Registry`, has a way to list registered tools — see step 3 for the exact accessor).
- Produces: `BuildAgentCard(reg *tools.Registry, name, description, url string) AgentCard`. Task 14's `serve` subcommand calls this once at startup.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTool satisfies agentsdk.Tool with the minimum needed for
// BuildAgentCard, which only reads Name() and Description().
type fakeTool struct{ name, desc string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return f.desc }
func (f fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f fakeTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{}, nil
}

func TestBuildAgentCard(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeTool{name: "file", desc: "read/write files"}))
	require.NoError(t, reg.Register(fakeTool{name: "shell", desc: "run shell commands"}))

	card := BuildAgentCard(reg, "rubichan", "a coding agent", "https://example.com")

	assert.Equal(t, "rubichan", card.Name)
	assert.Equal(t, "https://example.com", card.URL)
	assert.True(t, card.Capabilities.Streaming)
	assert.Len(t, card.Skills, 2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestBuildAgentCard -v`
Expected: FAIL — `BuildAgentCard` undefined.

- [ ] **Step 3: Write minimal implementation**

First check `pkg/agentsdk/registry.go` for how registered tools are listed (e.g. a `Tools() []Tool` or `Names() []string` method) — if none exists yet, add one:

```go
// Tools returns every registered tool, in no particular order.
func (r *Registry) Tools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}
```

```go
package a2a

import "github.com/julianshen/rubichan/internal/tools"

// BuildAgentCard walks reg and emits one Skill per registered tool, so the
// card this instance advertises always reflects what it can actually do.
func BuildAgentCard(reg *tools.Registry, name, description, url string) AgentCard {
	toolList := reg.Tools()
	skills := make([]Skill, 0, len(toolList))
	for _, t := range toolList {
		skills = append(skills, Skill{
			ID:          t.Name(),
			Name:        t.Name(),
			Description: t.Description(),
		})
	}
	return AgentCard{
		Name:         name,
		Description:  description,
		URL:          url,
		Skills:       skills,
		Capabilities: Capabilities{Streaming: true},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/agentcard.go internal/a2a/agentcard_test.go pkg/agentsdk/registry.go
git commit -m "[BEHAVIORAL] Add AgentCard builder from a tool registry"
```

---

### Task 11: Server — message/send

**Files:**
- Create: `internal/a2a/server.go`
- Test: `internal/a2a/server_test.go`

**Interfaces:**
- Consumes: `Task`/`Message`/`RPCRequest`/`RPCResponse`/`RPCError` (Task 1), `TaskStore` (Task 7), `AgentCard` (Task 1/10), `agent.Agent.Turn` and `agentsdk.TurnEvent` (`internal/agent/agent.go`, `pkg/agentsdk/events.go` — pattern to follow is `internal/agent/subagent.go`'s `Spawn`: range over the event channel, accumulate `text_delta` into a `strings.Builder`, watch for `"error"`/`"done"`).
- Produces: `Server` struct, `NewServer(a *agent.Agent, tasks TaskStore, card AgentCard, requireAuth bool, apiKey string) *Server`, `(*Server) Handler() http.Handler`. Task 12 adds streaming to the same `Server`; Task 13 adds auth and the remaining two methods; Task 14 constructs and runs it.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider/anthropic"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedReplySSE builds a minimal Anthropic streaming SSE body that always
// answers with one text_delta of reply, in the exact event sequence
// internal/provider/anthropic's own tests use (see
// internal/provider/anthropic/provider_test.go's TestStreamTextResponse).
func fixedReplySSE(reply string) string {
	return fmt.Sprintf(`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`, reply)
}

// newTestAgent builds a minimal Agent against a stub Anthropic server that
// always answers with a fixed text reply, so server tests don't need a
// real LLM. Follows internal/provider/anthropic/provider_test.go's own
// pattern exactly: New(baseURL, apiKey) + SetHTTPClient, not a
// functional-options constructor.
func newTestAgent(t *testing.T, reply string) *agent.Agent {
	t.Helper()
	body := fixedReplySSE(reply)
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))

	p := anthropic.New(server.URL, "test-api-key")
	p.SetHTTPClient(server.Client())

	cfg := config.DefaultConfig()
	approve := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	return agent.New(p, tools.NewRegistry(), approve, cfg)
}

func TestServerMessageSendCompletes(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/send",
		Params: mustMarshal(t, struct {
			Message Message `json:"message"`
		}{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "what is 6*7?"}}}}),
	})

	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error)

	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskCompleted, task.State)
	assert.Contains(t, task.Artifacts[0].Parts[0].Text, "42")
}
```

Also add this small helper to `server_test.go` for now (Task 11 doesn't yet have a real `TaskStore` fake — reuse the same in-memory shape both here and in Task 12/13's tests):

```go
type memoryTaskStore struct{ tasks map[string]Task }

func newMemoryTaskStore() *memoryTaskStore { return &memoryTaskStore{tasks: map[string]Task{}} }

func (m *memoryTaskStore) Create(_ context.Context, t Task) error { m.tasks[t.ID] = t; return nil }
func (m *memoryTaskStore) Get(_ context.Context, id string) (Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("task %s not found", id)
	}
	return t, nil
}
func (m *memoryTaskStore) Update(_ context.Context, t Task) error { m.tasks[t.ID] = t; return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestServerMessageSendCompletes -v`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/julianshen/rubichan/internal/agent"
)

// Server answers A2A protocol requests by driving a real agent.Agent turn
// per task.
type Server struct {
	agent       *agent.Agent
	tasks       TaskStore
	card        AgentCard
	requireAuth bool
	apiKey      string
}

// NewServer creates a Server. When requireAuth is true, every JSON-RPC
// request (not the Agent Card fetch) must carry a matching
// "Authorization: Bearer <apiKey>" header.
func NewServer(a *agent.Agent, tasks TaskStore, card AgentCard, requireAuth bool, apiKey string) *Server {
	return &Server{agent: a, tasks: tasks, card: card, requireAuth: requireAuth, apiKey: apiKey}
}

// Handler returns the HTTP handler serving the Agent Card and the
// JSON-RPC endpoint.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", s.handleAgentCard)
	mux.HandleFunc("/", s.handleRPC)
	return mux
}

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, 0, -32700, "parse error: "+err.Error())
		return
	}

	switch req.Method {
	case "message/send":
		s.handleMessageSend(w, r, req)
	default:
		s.writeError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

type sendParams struct {
	Message Message `json:"message"`
}

func (s *Server) handleMessageSend(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params sendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	task := Task{ID: newTaskID(), ContextID: newTaskID(), State: TaskSubmitted}
	ctx := r.Context()
	_ = s.tasks.Create(ctx, task)

	task.State = TaskWorking
	_ = s.tasks.Update(ctx, task)

	task = s.runTurn(ctx, task, messageText(params.Message))
	_ = s.tasks.Update(ctx, task)

	result, _ := json.Marshal(task)
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

// runTurn drives task through a real agent turn, mirroring
// internal/agent/subagent.go's Spawn: accumulate text_delta into the
// final Artifact, watch for error/done. A panic inside Turn's own
// execution is recovered here so a single bad task can never take the
// listener down.
func (s *Server) runTurn(ctx context.Context, task Task, prompt string) (result Task) {
	defer func() {
		if r := recover(); r != nil {
			result = task
			result.State = TaskFailed
			result.Error = fmt.Sprintf("panic: %v", r)
		}
	}()

	eventCh, err := s.agent.Turn(ctx, prompt)
	if err != nil {
		task.State = TaskFailed
		task.Error = err.Error()
		return task
	}

	var out strings.Builder
	for event := range eventCh {
		switch event.Type {
		case "text_delta":
			out.WriteString(event.Text)
		case "error":
			task.State = TaskFailed
			if event.Error != nil {
				task.Error = event.Error.Error()
			}
		}
	}

	if task.State == TaskFailed {
		return task
	}

	task.State = TaskCompleted
	task.Artifacts = []Artifact{{Name: "result", Parts: []Part{{Kind: "text", Text: out.String()}}}}
	return task
}

func messageText(m Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func (s *Server) writeError(w http.ResponseWriter, id int64, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}})
}

var taskIDCounter int64

func newTaskID() string {
	taskIDCounter++
	return "task-" + strconv.FormatInt(taskIDCounter, 10)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/server.go internal/a2a/server_test.go
git commit -m "[BEHAVIORAL] Add A2A server message/send handler"
```

---

### Task 12: Server — message/stream (SSE)

**Files:**
- Modify: `internal/a2a/server.go`
- Test: `internal/a2a/server_test.go`

**Interfaces:**
- Consumes: everything from Task 11, plus `agentsdk.TurnEvent`'s streaming shape (each `text_delta` becomes one SSE push).
- Produces: `handleMessageStream` wired into `handleRPC`'s method switch alongside `message/send`.

- [ ] **Step 1: Write the failing test**

```go
func TestServerMessageStreamPushesEvents(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/stream",
		Params: mustMarshal(t, sendParams{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}}}),
	})

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	events := parseSSEEvents(resp.Body)
	var last Task
	for raw := range events {
		require.NoError(t, json.Unmarshal(raw, &last))
	}
	assert.Equal(t, TaskCompleted, last.State)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestServerMessageStreamPushesEvents -v`
Expected: FAIL — `message/stream` falls through to "method not found".

- [ ] **Step 3: Write minimal implementation**

Add `case "message/stream": s.handleMessageStream(w, r, req)` to `handleRPC`'s switch in `internal/a2a/server.go`, and add:

```go
func (s *Server) handleMessageStream(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params sendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, req.ID, -32603, "streaming unsupported by this response writer")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ctx := r.Context()
	task := Task{ID: newTaskID(), ContextID: newTaskID(), State: TaskSubmitted}
	_ = s.tasks.Create(ctx, task)
	writeSSETask(w, flusher, task)

	task.State = TaskWorking
	_ = s.tasks.Update(ctx, task)
	writeSSETask(w, flusher, task)

	task = s.runTurn(ctx, task, messageText(params.Message))
	_ = s.tasks.Update(ctx, task)
	writeSSETask(w, flusher, task)
}

func writeSSETask(w http.ResponseWriter, flusher http.Flusher, task Task) {
	data, err := json.Marshal(task)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
	flusher.Flush()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/server.go internal/a2a/server_test.go
git commit -m "[BEHAVIORAL] Add A2A server message/stream via SSE"
```

---

### Task 13: Server — auth, tasks/get, tasks/cancel

**Files:**
- Modify: `internal/a2a/server.go`
- Test: `internal/a2a/server_test.go`

**Interfaces:**
- Consumes: everything from Tasks 11-12.
- Produces: auth check inside `handleRPC` (before the method switch), `handleTasksGet`, `handleTasksCancel`.

- [ ] **Step 1: Write the failing test**

```go
func TestServerRequiresAuthWhenConfigured(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, true, "secret")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: 1, Method: "tasks/get"})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestServerAcceptsMatchingBearerToken(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, true, "secret")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	require.NoError(t, store.Create(context.Background(), Task{ID: "task-1", State: TaskWorking}))

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/get",
		Params: mustMarshal(t, struct {
			ID string `json:"id"`
		}{ID: "task-1"}),
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskWorking, task.State)
}

func TestServerTasksCancel(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	require.NoError(t, store.Create(context.Background(), Task{ID: "task-1", State: TaskWorking}))

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/cancel",
		Params: mustMarshal(t, struct {
			ID string `json:"id"`
		}{ID: "task-1"}),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	task, err := store.Get(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskCanceled, task.State)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run 'TestServerRequiresAuth|TestServerAcceptsMatchingBearerToken|TestServerTasksCancel' -v`
Expected: FAIL — no auth check yet; `tasks/get`/`tasks/cancel` return "method not found".

- [ ] **Step 3: Write minimal implementation**

Modify `handleRPC` in `internal/a2a/server.go`:

```go
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if s.requireAuth && !s.authorized(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var req RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, 0, -32700, "parse error: "+err.Error())
		return
	}

	switch req.Method {
	case "message/send":
		s.handleMessageSend(w, r, req)
	case "message/stream":
		s.handleMessageStream(w, r, req)
	case "tasks/get":
		s.handleTasksGet(w, r, req)
	case "tasks/cancel":
		s.handleTasksCancel(w, r, req)
	default:
		s.writeError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	got := r.Header.Get("Authorization")
	return got == "Bearer "+s.apiKey
}

type taskIDParams struct {
	ID string `json:"id"`
}

func (s *Server) handleTasksGet(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params taskIDParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	task, err := s.tasks.Get(r.Context(), params.ID)
	if err != nil {
		s.writeError(w, req.ID, -32000, "task not found: "+err.Error())
		return
	}
	result, _ := json.Marshal(task)
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func (s *Server) handleTasksCancel(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params taskIDParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	ctx := r.Context()
	task, err := s.tasks.Get(ctx, params.ID)
	if err != nil {
		s.writeError(w, req.ID, -32000, "task not found: "+err.Error())
		return
	}
	task.State = TaskCanceled
	_ = s.tasks.Update(ctx, task)
	result, _ := json.Marshal(task)
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}
```

Note: `handleAgentCard` must stay reachable without auth (the spec requires the card fetch itself to be exempt) — it's already routed on a separate mux pattern (`/.well-known/agent.json`) that doesn't go through `handleRPC`, so no change needed there; confirm this with a quick manual check (`grep -n "HandleFunc" internal/a2a/server.go`) before moving on.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/server.go internal/a2a/server_test.go
git commit -m "[BEHAVIORAL] Add A2A server auth, tasks/get, tasks/cancel"
```

---

### Task 14: `rubichan a2a serve` subcommand

**Files:**
- Create: `cmd/rubichan/a2a.go`
- Modify: `cmd/rubichan/main.go`
- Test: `cmd/rubichan/a2a_test.go`

**Interfaces:**
- Consumes: `a2a.NewServer`/`a2a.BuildAgentCard`/`a2a.NewStoreTaskStore` (Tasks 7, 10, 11), `agent.New` (`internal/agent/agent.go`), `config.Load`/`config.ResolveAPIKey` (`internal/config`), `provider.NewProviderWithDebug` (`internal/provider`), `store.NewStore` (`internal/store`) — mirrors `cmd/rubichan/serve.go`'s `runServe` almost exactly, substituting the A2A server for the WebSocket one.
- Produces: `a2aCmd() *cobra.Command` (parent, `Use: "a2a"`), `a2aServeCmd() *cobra.Command` (child, `Use: "serve"`), added to `rootCmd` in `main.go` via `rootCmd.AddCommand(a2aCmd())`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA2ACmdHasServeSubcommand(t *testing.T) {
	cmd := a2aCmd()
	assert.Equal(t, "a2a", cmd.Use)

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "serve" {
			found = true
		}
	}
	assert.True(t, found, "a2a command must have a serve subcommand")
}

func TestA2AServeCmdFlags(t *testing.T) {
	cmd := a2aServeCmd()
	addrFlag := cmd.Flags().Lookup("addr")
	require.NotNil(t, addrFlag)
	assert.Equal(t, "127.0.0.1:9910", addrFlag.DefValue)

	noAuthFlag := cmd.Flags().Lookup("no-auth")
	require.NotNil(t, noAuthFlag)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestA2A -v`
Expected: FAIL — `a2aCmd` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

var (
	a2aServeAddr   string
	a2aServeNoAuth bool
)

func a2aCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "a2a",
		Short: "A2A (Agent2Agent) protocol commands",
	}
	cmd.AddCommand(a2aServeCmd())
	return cmd
}

func a2aServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Expose rubichan as a callable A2A agent",
		Long: `Start an HTTP server implementing the A2A (Agent2Agent) protocol,
so other agents can discover and call rubichan.

Endpoints:
  GET  /.well-known/agent.json   Agent Card
  POST /                         JSON-RPC 2.0 (message/send, message/stream, tasks/get, tasks/cancel)`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runA2AServe()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().StringVar(&a2aServeAddr, "addr", "127.0.0.1:9910", "listen address (host:port)")
	cmd.Flags().BoolVar(&a2aServeNoAuth, "no-auth", false, "disable auth (refuses to bind on a non-loopback address unless set)")
	return cmd
}

func runA2AServe() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if a2aServeAddr != "" {
		cfg.A2A.ServeAddr = a2aServeAddr
	}
	requireAuth := cfg.A2A.RequireAuth && !a2aServeNoAuth

	host, _, err := net.SplitHostPort(cfg.A2A.ServeAddr)
	if err != nil {
		return fmt.Errorf("parse addr: %w", err)
	}
	if !requireAuth && host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("refusing to bind %s without auth; pass an api key or use --addr on loopback only", cfg.A2A.ServeAddr)
	}

	apiKey, err := config.ResolveAPIKey(cfg.A2A.APIKeySource, cfg.A2A.APIKey, "RUBICHAN_A2A_API_KEY")
	if requireAuth && err != nil {
		return fmt.Errorf("resolve a2a api key: %w", err)
	}

	llm, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	registry := tools.NewRegistry()
	if _, err := registerCoreTools(".", registry, cfg, ToolsConfig{}, tools.NewDiffTracker(), 120*time.Second); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}

	approve := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	a := agent.New(llm, registry, approve, cfg)

	cfgDir, err := configDir()
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	st, err := openStore(cfgDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	card := a2a.BuildAgentCard(registry, "rubichan", "A terminal-first AI coding agent", "http://"+cfg.A2A.ServeAddr)
	srv := a2a.NewServer(a, a2a.NewStoreTaskStore(st, "server"), card, requireAuth, apiKey)

	ln, err := net.Listen("tcp", cfg.A2A.ServeAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.A2A.ServeAddr, err)
	}

	log.Printf("rubichan a2a serve: listening on %s", ln.Addr())
	if !requireAuth {
		log.Print("rubichan a2a serve: WARNING: auth disabled")
	}

	httpSrv := &http.Server{Handler: srv.Handler()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Print("rubichan a2a serve: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
```

Add `"net/http"` and `"encoding/json"` to the import block above (`configDir` and `openStore` are both already declared, unexported, in `cmd/rubichan/main.go`, so no import is needed for them — this file is in the same `package main`). Add `rootCmd.AddCommand(a2aCmd())` to `main.go` next to the other `rootCmd.AddCommand(...)` calls (near line 425).

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./cmd/rubichan/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/rubichan/a2a.go cmd/rubichan/a2a_test.go cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Add rubichan a2a serve subcommand"
```

---

### Task 15: Integration test — Phase 1 client against Phase 2 server

**Files:**
- Create: `internal/a2a/integration_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-13 (`Client`, `Server`, `TaskStore`, in-memory `agent.Agent`).
- Produces: nothing new — proves the two sides agree on the wire format.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientAgainstOwnServer(t *testing.T) {
	a := newTestAgent(t, "42")
	tasks := newMemoryTaskStore()
	server := NewServer(a, tasks, AgentCard{Name: "rubichan", Skills: []Skill{{ID: "chat", Name: "Chat"}}}, false, "")

	srv := testutil.NewServer(t, server.Handler())
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())

	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "rubichan", card.Name)

	task, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "what is 6*7?"}}})
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, task.State)
	assert.Contains(t, task.Artifacts[0].Parts[0].Text, "42")

	streamCh, err := client.Stream(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "again"}}})
	require.NoError(t, err)
	var last Task
	for t := range streamCh {
		last = t
	}
	assert.Equal(t, TaskCompleted, last.State)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestClientAgainstOwnServer -v`
Expected: PASS immediately — this task adds no new production code, only a test proving Tasks 1-13 already agree on the wire format. `internal/testutil/http.go`'s `memoryResponseWriter.Write` broadcasts each write to blocked readers via `sync.Cond` (verified during planning), and `Flush()` being a no-op is correct because that broadcast already delivers each `Write` to `parseSSEEvents`'s `bufio.Scanner` as its own frame — no separate flush signal is needed over this transport. If this test fails, it has found a real bug in an earlier task (most likely a JSON field name mismatch between `Client`'s request marshaling and `Server`'s param unmarshaling): fix it in the file where the mismatch actually is, then return to this step.

- [ ] **Step 3: Write minimal implementation**

None — this task is the test itself (Step 1). Skip to Step 5 once Step 2 passes.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS (full package)

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/integration_test.go
git commit -m "[BEHAVIORAL] Add A2A client/server integration test"
```

---

## Phase 3: Orchestration

### Task 16: Registry

**Files:**
- Create: `internal/a2a/registry.go`
- Test: `internal/a2a/registry_test.go`

**Interfaces:**
- Consumes: `AgentCard`/`Skill` (Task 1).
- Produces: `Registry` struct, `NewRegistry() *Registry`, `(*Registry) Add(name string, card AgentCard)`, `(*Registry) Match(taskDescription string) []string`. Task 17's `a2a_dispatch` tool is the only consumer.

- [ ] **Step 1: Write the failing test**

```go
package a2a

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistryMatchByTagOverlap(t *testing.T) {
	reg := NewRegistry()
	reg.Add("weather-bot", AgentCard{
		Name:   "weather-bot",
		Skills: []Skill{{ID: "forecast", Name: "Forecast", Description: "weather forecasts", Tags: []string{"weather", "forecast"}}},
	})
	reg.Add("code-bot", AgentCard{
		Name:   "code-bot",
		Skills: []Skill{{ID: "review", Name: "Review", Description: "reviews code", Tags: []string{"code", "review"}}},
	})

	matches := reg.Match("what's the weather forecast for tomorrow")
	assert.Equal(t, []string{"weather-bot"}, matches)
}

func TestRegistryMatchNoneFound(t *testing.T) {
	reg := NewRegistry()
	reg.Add("code-bot", AgentCard{Name: "code-bot", Skills: []Skill{{ID: "review", Tags: []string{"code"}}}})

	assert.Empty(t, reg.Match("book me a flight to Tokyo"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/a2a/... -run TestRegistryMatch -v`
Expected: FAIL — `NewRegistry` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2a

import "strings"

// Registry holds known agents' Agent Cards and does simple, deterministic
// capability matching — no LLM call, so dispatch decisions stay cheap and
// reproducible.
type Registry struct {
	agents map[string]AgentCard
	order  []string // insertion order, for deterministic Match output
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]AgentCard)}
}

// Add registers card under name, overwriting any existing entry.
func (r *Registry) Add(name string, card AgentCard) {
	if _, exists := r.agents[name]; !exists {
		r.order = append(r.order, name)
	}
	r.agents[name] = card
}

// Match scores every registered agent's skill tags and descriptions
// against taskDescription by substring/word overlap, returning agent
// names best-first. An agent with no overlapping words is excluded, not
// scored zero and returned last — a caller asking for the "best match"
// should get "no match" rather than an arbitrary agent.
func (r *Registry) Match(taskDescription string) []string {
	words := wordSet(taskDescription)

	type scored struct {
		name  string
		score int
	}
	var results []scored
	for _, name := range r.order {
		card := r.agents[name]
		score := 0
		for _, skill := range card.Skills {
			for _, tag := range skill.Tags {
				if words[strings.ToLower(tag)] {
					score++
				}
			}
			for w := range wordSet(skill.Description) {
				if words[w] {
					score++
				}
			}
		}
		if score > 0 {
			results = append(results, scored{name, score})
		}
	}

	// Stable sort by score descending, preserving insertion order on ties.
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	names := make([]string, len(results))
	for i, s := range results {
		names[i] = s.name
	}
	return names
}

func wordSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		out[strings.Trim(w, ".,!?'\"")] = true
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/a2a/registry.go internal/a2a/registry_test.go
git commit -m "[BEHAVIORAL] Add A2A agent registry with tag-based matching"
```

---

### Task 17: Tool — a2a_dispatch

**Files:**
- Create: `internal/tools/a2a/dispatch_tool.go`
- Test: `internal/tools/a2a/dispatch_tool_test.go`

**Interfaces:**
- Consumes: `a2a.Registry`/`Match` (Task 16), `a2a.Client.Send` (Task 3), `resolveAgentURL` (Task 8).
- Produces: `DispatchTool` struct, `NewDispatchTool(cfg *config.Config, reg *a2a.Registry) *DispatchTool`. Registered under tool name `"a2a_dispatch"`.

- [ ] **Step 1: Write the failing test**

```go
package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatchToolCallsBestMatch(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req a2a.RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		result, _ := json.Marshal(a2a.Task{
			ID: "task-1", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Parts: []a2a.Part{{Kind: "text", Text: "sunny"}}}},
		})
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "weather-bot", URL: srv.URL}}

	reg := a2a.NewRegistry()
	reg.Add("weather-bot", a2a.AgentCard{
		Name:   "weather-bot",
		Skills: []a2a.Skill{{ID: "forecast", Tags: []string{"weather"}}},
	})

	tool := NewDispatchTool(cfg, reg)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"task_description": "what's the weather like"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "sunny")
}

func TestDispatchToolNoMatch(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := a2a.NewRegistry()
	tool := NewDispatchTool(cfg, reg)

	input, err := json.Marshal(map[string]string{"task_description": "anything"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/a2a/... -run TestDispatchTool -v`
Expected: FAIL — `NewDispatchTool` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DispatchTool picks the best-matching known agent for a task description
// (via Registry.Match's deterministic tag/word overlap, not an LLM call)
// and calls it.
type DispatchTool struct {
	cfg        *config.Config
	registry   *a2a.Registry
	httpClient *http.Client
}

// NewDispatchTool creates a DispatchTool over reg.
func NewDispatchTool(cfg *config.Config, reg *a2a.Registry) *DispatchTool {
	return &DispatchTool{cfg: cfg, registry: reg}
}

func (d *DispatchTool) Name() string { return "a2a_dispatch" }

func (d *DispatchTool) Description() string {
	return "Find the best-matching known A2A agent for a task description (by skill tags, " +
		"not an LLM call) and call it. Returns an error if no agent matches."
}

func (d *DispatchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_description": {"type": "string", "description": "What needs to be done"}
		},
		"required": ["task_description"]
	}`)
}

type dispatchInput struct {
	TaskDescription string `json:"task_description"`
}

func (d *DispatchTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in dispatchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	matches := d.registry.Match(in.TaskDescription)
	if len(matches) == 0 {
		return agentsdk.ToolResult{Content: "no known agent matches this task description", IsError: true}, nil
	}
	best := matches[0]

	url, apiKey, err := resolveAgentURL(d.cfg, best)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, d.httpClient)
	task, err := client.Send(ctx, a2a.Message{Role: "user", Parts: []a2a.Part{{Kind: "text", Text: in.TaskDescription}}})
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("dispatch to %s failed: %v", best, err), IsError: true}, nil
	}
	if task.State != a2a.TaskCompleted {
		return agentsdk.ToolResult{Content: fmt.Sprintf("%s ended in state %q: %s", best, task.State, task.Error), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] ", best)
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			b.WriteString(p.Text)
		}
	}
	return agentsdk.ToolResult{Content: b.String()}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/a2a/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/a2a/dispatch_tool.go internal/tools/a2a/dispatch_tool_test.go
git commit -m "[BEHAVIORAL] Add a2a_dispatch tool"
```

---

### Task 18: Wire A2A tools and registry into interactive/headless startup

**Files:**
- Modify: `cmd/rubichan/main.go`
- Test: `cmd/rubichan/a2a_wiring_test.go` (new file, rather than growing `main_test.go` — a separate file keeps these tests scoped to the A2A wiring they exercise)

**Interfaces:**
- Consumes: `a2atools.NewDiscoverTool`/`NewCallTool`/`NewDispatchTool` (Tasks 8, 9, 17), `a2a.NewRegistry`/`Add` (Task 16), `a2a.Client.Discover` (Task 2).
- Produces: `buildA2ARegistry(ctx context.Context, cfg *config.Config) *a2a.Registry` helper; two new entries in `wireExtendedTools`'s tool literal list (`a2a_discover`, `a2a_dispatch` — neither needs the store); two new individually-registered calls to `a2atools.NewCallTool` (one in interactive setup, one in headless setup), placed after `openStore` succeeds, matching where `internal/tools/notes.go`'s `NewNotesTool` is already registered outside `registerCoreTools` for the same reason (a late-available dependency). `a2a_call` needs `*store.Store`, which `registerCoreTools`/`wireExtendedTools` runs before the store exists in both setup paths (`registerCoreTools` is called at `main.go:1064`/`:1569`; `openStore` doesn't run until `:1122`/`:1612` — confirmed by reading `main.go` during planning, not assumed), so it cannot go in `wireExtendedTools`'s list the way `a2a_discover`/`a2a_dispatch` can.

- [ ] **Step 1: Write the failing test**

```go
func TestBuildA2ARegistryPopulatesFromConfig(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"weather-bot","skills":[{"id":"forecast","tags":["weather"]}]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "weather-bot", URL: srv.URL}}

	reg := buildA2ARegistry(context.Background(), cfg)
	assert.NotEmpty(t, reg.Match("weather"))
}

func TestBuildA2ARegistryToleratesUnreachableAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "gone", URL: "mem://does-not-exist"}}

	reg := buildA2ARegistry(context.Background(), cfg)
	assert.Empty(t, reg.Match("anything"), "an unreachable agent must not appear in the registry, and must not error out registry construction")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestBuildA2ARegistry -v`
Expected: FAIL — `buildA2ARegistry` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `cmd/rubichan/a2a.go`:

```go
// buildA2ARegistry discovers every configured [[a2a.agents]] entry and
// populates a Registry. An agent that fails to respond is logged and
// skipped, not fatal — a2a_dispatch simply won't route to it.
func buildA2ARegistry(ctx context.Context, cfg *config.Config) *a2a.Registry {
	reg := a2a.NewRegistry()
	for _, agentCfg := range cfg.A2A.Agents {
		apiKey, _ := config.ResolveAPIKey(agentCfg.APIKeySource, agentCfg.APIKey, "")
		client := a2a.NewClient(agentCfg.URL, apiKey, nil)
		card, err := client.Discover(ctx)
		if err != nil {
			log.Printf("a2a: could not discover configured agent %q: %v", agentCfg.Name, err)
			continue
		}
		reg.Add(agentCfg.Name, *card)
	}
	return reg
}
```

Modify `wireExtendedTools` in `main.go` to add `a2a_discover` and `a2a_dispatch` (both need only `cfg`, not the store) to its existing tool literal list:

```go
func wireExtendedTools(cwd string, registry *tools.Registry, cfg *config.Config, toolsCfg ToolsConfig) error {
	a2aReg := buildA2ARegistry(context.Background(), cfg)
	for _, tool := range []tools.Tool{
		httptool.NewGetTool(),
		httptool.NewPostTool(),
		httptool.NewPutTool(),
		httptool.NewPatchTool(),
		httptool.NewDeleteTool(),
		httptool.NewWebFetchTool(),
		gittools.NewStatusTool(cwd),
		gittools.NewDiffTool(cwd),
		gittools.NewLogTool(cwd),
		gittools.NewShowTool(cwd),
		gittools.NewBlameTool(cwd),
		dbtools.NewQueryTool(cwd),
		a2atools.NewDiscoverTool(cfg),
		a2atools.NewDispatchTool(cfg, a2aReg),
	} {
		if toolsCfg.ShouldEnable(tool.Name()) {
			if err := registry.Register(tool); err != nil {
				return fmt.Errorf("registering tool %s: %w", tool.Name(), err)
			}
		}
	}
	// ...rest of the existing function unchanged
```

`a2a_call` needs `*store.Store`, which doesn't exist yet at `wireExtendedTools`'s call site. Register it separately, right after `openStore` succeeds, in both setup paths — matching exactly how `internal/tools/notes.go`'s `NewNotesTool` is already registered outside `registerCoreTools`/`wireExtendedTools` for the same late-dependency reason (see `main.go:1414-1419`).

In interactive setup, immediately after the existing `s, err := openStore(cfgDir)` call at `main.go:1122`:

```go
	if toolsCfg.ShouldEnable("a2a_call") {
		if err := registry.Register(a2atools.NewCallTool(cfg, s)); err != nil {
			return fmt.Errorf("registering a2a_call tool: %w", err)
		}
	}
```

In headless setup, immediately after the existing `s, err := openStore(cfgDir)` call at `main.go:1612` (confirmed during planning: `registry` is the same variable name as interactive setup; the headless block's `ToolsConfig` variable is named `headlessToolsCfg`, per its `registerCoreTools` call at `main.go:1569`):

```go
	if headlessToolsCfg.ShouldEnable("a2a_call") {
		if err := registry.Register(a2atools.NewCallTool(cfg, s)); err != nil {
			return fmt.Errorf("registering a2a_call tool: %w", err)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./cmd/rubichan/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/rubichan/main.go cmd/rubichan/a2a.go cmd/rubichan/a2a_wiring_test.go
git commit -m "[BEHAVIORAL] Wire A2A tools and agent registry into startup"
```

---

## Final Verification

After Task 18:

```bash
go build ./...
go test ./... -cover
gofmt -l .
```

Expected: all tests pass, `gofmt -l .` prints nothing, and `internal/a2a`/`internal/tools/a2a` both show >90% coverage in the `-cover` output. Any package below that threshold needs additional tests before this branch is considered done — per CLAUDE.md, this is not optional polish.
