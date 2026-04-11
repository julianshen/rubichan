# cmux Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Rubichan with cmux's Unix socket JSON-RPC API for rich sidebar feedback, native notifications, browser automation tools, and active multi-agent orchestration.

**Architecture:** A thin JSON-RPC socket client (`internal/cmux/client.go`) with feature modules for sidebar, notifications, workspace, surface, browser, and orchestration. Detection extends the existing `terminal.Caps` struct. Agent tools are conditionally registered when running inside cmux.

**Tech Stack:** Go `net` (Unix sockets), `encoding/json`, existing `internal/tools` registry, `internal/terminal` caps detection.

---

## File Structure

```
internal/cmux/
  client.go          — JSON-RPC socket client (Dial, Call, Close, Identity)
  client_test.go     — tests with fake Unix socket server
  workspace.go       — workspace CRUD methods
  workspace_test.go
  surface.go         — split, send_text, send_key, list, focus
  surface_test.go
  sidebar.go         — set-status, set-progress, log, sidebar-state
  sidebar_test.go
  notification.go    — notification.create/list/clear
  notification_test.go
  browser.go         — navigate, snapshot, click, type, wait
  browser_test.go
  orchestrator.go    — dispatch sub-agents, poll logs, collect results
  orchestrator_test.go
  cmuxtest/
    mock.go          — MockClient for consumer tests

internal/terminal/
  caps.go            — add CmuxSocket bool (modify existing)
  caps_test.go       — add cmux detection tests (modify existing)

internal/tools/
  cmux_browser.go    — 5 browser tools
  cmux_browser_test.go
  cmux_surface.go    — split + send tools
  cmux_surface_test.go
  cmux_orchestrate.go — orchestrate tool
  cmux_orchestrate_test.go

cmd/rubichan/
  main.go            — wire cmux client into all 3 modes (modify existing)

internal/tui/
  model.go           — add SetCmuxClient, cmux dispatch (modify existing)
  update.go          — cmux notification dispatch (modify existing)
```

---

### Task 1: JSON-RPC Socket Client

**Files:**
- Create: `internal/cmux/client.go`
- Create: `internal/cmux/client_test.go`

- [ ] **Step 1: Write the failing test for Dial + system.ping**

```go
// internal/cmux/client_test.go
package cmux

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServer accepts one connection and handles JSON-RPC requests.
func fakeServer(t *testing.T, ln net.Listener, handlers map[string]any) {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		result, ok := handlers[req.Method]
		if !ok {
			enc.Encode(map[string]any{"id": req.ID, "ok": false, "error": fmt.Sprintf("unknown method: %s", req.Method)})
			continue
		}
		enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": result})
	}
}

func newTestServer(t *testing.T, handlers map[string]any) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go fakeServer(t, ln, handlers)
	return sock
}

func TestDial_Ping(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping": map[string]bool{"pong": true},
		"system.identify": map[string]string{
			"window_id":    "win-1",
			"workspace_id": "ws-1",
			"pane_id":      "pane-1",
			"surface_id":   "surf-1",
		},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	assert.Equal(t, "win-1", c.Identity().WindowID)
	assert.Equal(t, "ws-1", c.Identity().WorkspaceID)
	assert.Equal(t, "surf-1", c.Identity().SurfaceID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cmux/ -run TestDial_Ping -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement Client**

```go
// internal/cmux/client.go
package cmux

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Response is a JSON-RPC response from cmux.
type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// Identity contains the cmux context identifiers cached at Dial time.
type Identity struct {
	WindowID    string `json:"window_id"`
	WorkspaceID string `json:"workspace_id"`
	PaneID      string `json:"pane_id"`
	SurfaceID   string `json:"surface_id"`
}

// Client communicates with cmux over a Unix domain socket.
type Client struct {
	conn    net.Conn
	enc     *json.Encoder
	dec     *json.Decoder
	mu      sync.Mutex
	nextID  atomic.Int64
	timeout time.Duration
	ident   *Identity
}

// SocketPath returns the cmux socket path from the environment or default.
func SocketPath() string {
	if p := os.Getenv("CMUX_SOCKET_PATH"); p != "" {
		return p
	}
	return "/tmp/cmux.sock"
}

// Dial connects to the cmux socket and caches the identity.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("cmux dial: %w", err)
	}
	c := &Client{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		dec:     json.NewDecoder(conn),
		timeout: 5 * time.Second,
	}

	// Verify connectivity.
	if _, err := c.Call("system.ping", nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cmux ping: %w", err)
	}

	// Cache identity.
	resp, err := c.Call("system.identify", nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("cmux identify: %w", err)
	}
	var ident Identity
	if err := json.Unmarshal(resp.Result, &ident); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cmux parse identity: %w", err)
	}
	c.ident = &ident

	return c, nil
}

// Call sends a JSON-RPC request and reads the response.
func (c *Client) Call(method string, params any) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("cmux set deadline: %w", err)
	}

	id := fmt.Sprintf("req-%d", c.nextID.Add(1))
	req := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: params}

	if params == nil {
		req.Params = struct{}{}
	}

	if err := c.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("cmux send %s: %w", method, err)
	}

	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("cmux recv %s: %w", method, err)
	}

	if !resp.OK {
		return &resp, fmt.Errorf("cmux %s: %s", method, resp.Error)
	}

	return &resp, nil
}

// Identity returns the cached cmux context identifiers.
func (c *Client) Identity() *Identity {
	return c.ident
}

// Close closes the socket connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cmux/ -run TestDial_Ping -v`
Expected: PASS

- [ ] **Step 5: Write test for Call error handling**

```go
// internal/cmux/client_test.go — append

func TestCall_ErrorResponse(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Call("nonexistent.method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown method")
}

func TestSocketPath_Default(t *testing.T) {
	t.Setenv("CMUX_SOCKET_PATH", "")
	assert.Equal(t, "/tmp/cmux.sock", SocketPath())
}

func TestSocketPath_Override(t *testing.T) {
	t.Setenv("CMUX_SOCKET_PATH", "/custom/path.sock")
	assert.Equal(t, "/custom/path.sock", SocketPath())
}

func TestDial_BadSocket(t *testing.T) {
	_, err := Dial("/nonexistent/path.sock")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cmux dial")
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cmux/client.go internal/cmux/client_test.go
git commit -m "[BEHAVIORAL] Add cmux JSON-RPC socket client

Dial connects to cmux Unix socket, verifies with system.ping,
caches identity via system.identify. Thread-safe Call with timeout.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: MockClient for Consumer Tests

**Files:**
- Create: `internal/cmux/cmuxtest/mock.go`
- Create: `internal/cmux/cmuxtest/mock_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/cmux/cmuxtest/mock_test.go
package cmuxtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_RecordsCalls(t *testing.T) {
	m := NewMockClient()
	m.SetResult("system.ping", map[string]bool{"pong": true})

	resp, err := m.Call("system.ping", nil)
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Len(t, m.Calls(), 1)
	assert.Equal(t, "system.ping", m.Calls()[0].Method)
}

func TestMockClient_UnknownMethodErrors(t *testing.T) {
	m := NewMockClient()
	_, err := m.Call("unknown.method", nil)
	require.Error(t, err)
}

func TestMockClient_Identity(t *testing.T) {
	m := NewMockClient()
	assert.Equal(t, "mock-surface", m.Identity().SurfaceID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cmux/cmuxtest/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement MockClient**

The mock needs to satisfy the same API that consumer code uses. Define a `Caller` interface in the cmux package that both `Client` and `MockClient` satisfy:

```go
// internal/cmux/caller.go
package cmux

// Caller is the interface for making cmux JSON-RPC calls.
// Both Client and cmuxtest.MockClient implement this.
type Caller interface {
	Call(method string, params any) (*Response, error)
	Identity() *Identity
}
```

```go
// internal/cmux/cmuxtest/mock.go
package cmuxtest

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/cmux"
)

// Call records a single JSON-RPC call made to the mock.
type Call struct {
	Method string
	Params any
}

// MockClient is a test double for cmux.Client that records calls
// and returns canned responses.
type MockClient struct {
	mu        sync.Mutex
	calls     []Call
	results   map[string]any
	identity  *cmux.Identity
}

// NewMockClient creates a MockClient with a default identity.
func NewMockClient() *MockClient {
	return &MockClient{
		results: make(map[string]any),
		identity: &cmux.Identity{
			WindowID:    "mock-window",
			WorkspaceID: "mock-workspace",
			PaneID:      "mock-pane",
			SurfaceID:   "mock-surface",
		},
	}
}

// SetResult sets the canned result for a method.
func (m *MockClient) SetResult(method string, result any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[method] = result
}

// Call records the call and returns the canned result.
func (m *MockClient) Call(method string, params any) (*cmux.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, Call{Method: method, Params: params})

	result, ok := m.results[method]
	if !ok {
		return nil, fmt.Errorf("mock: no result for %s", method)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("mock: marshal result for %s: %w", method, err)
	}

	return &cmux.Response{
		ID:     fmt.Sprintf("mock-%d", len(m.calls)),
		OK:     true,
		Result: data,
	}, nil
}

// Identity returns the mock identity.
func (m *MockClient) Identity() *cmux.Identity {
	return m.identity
}

// Calls returns all recorded calls.
func (m *MockClient) Calls() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Call, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// Reset clears recorded calls and results.
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
	m.results = make(map[string]any)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmux/... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmux/caller.go internal/cmux/cmuxtest/mock.go internal/cmux/cmuxtest/mock_test.go
git commit -m "[BEHAVIORAL] Add cmux Caller interface and MockClient for testing

Caller interface abstracts Client for testability. MockClient records
calls and returns canned responses.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Sidebar Metadata

**Files:**
- Create: `internal/cmux/sidebar.go`
- Create: `internal/cmux/sidebar_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmux/sidebar_test.go
package cmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetStatus(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"set-status":      true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.SetStatus("phase", "analyzing", "magnifyingglass", "#007aff")
	assert.NoError(t, err)
}

func TestSetProgress(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"set-progress":    true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.SetProgress(0.5, "Building...")
	assert.NoError(t, err)
}

func TestClearProgress(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":      map[string]bool{"pong": true},
		"system.identify":  map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"clear-progress":   true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.ClearProgress()
	assert.NoError(t, err)
}

func TestLog(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"log":             true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.Log("Running tests...", "progress", "tools")
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmux/ -run TestSet -v`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement sidebar methods**

```go
// internal/cmux/sidebar.go
package cmux

// SetStatus sets a labeled status in the cmux sidebar tab.
// icon is an SF Symbol name (e.g., "hammer", "checkmark.circle").
// color is a hex string (e.g., "#ff9500").
func (c *Client) SetStatus(key, value, icon, color string) error {
	_, err := c.Call("set-status", map[string]string{
		"key":   key,
		"value": value,
		"icon":  icon,
		"color": color,
	})
	return err
}

// ClearStatus removes a status entry by key.
func (c *Client) ClearStatus(key string) error {
	_, err := c.Call("clear-status", map[string]string{"key": key})
	return err
}

// SetProgress sets the sidebar progress bar.
// fraction is 0.0–1.0. label describes the current operation.
func (c *Client) SetProgress(fraction float64, label string) error {
	_, err := c.Call("set-progress", map[string]any{
		"value": fraction,
		"label": label,
	})
	return err
}

// ClearProgress removes the sidebar progress bar.
func (c *Client) ClearProgress() error {
	_, err := c.Call("clear-progress", nil)
	return err
}

// Log appends a log entry to the sidebar.
// level: "info", "progress", "success", "warning", "error".
func (c *Client) Log(message, level, source string) error {
	_, err := c.Call("log", map[string]string{
		"message": message,
		"level":   level,
		"source":  source,
	})
	return err
}

// ClearLog clears all sidebar log entries.
func (c *Client) ClearLog() error {
	_, err := c.Call("clear-log", nil)
	return err
}

// SidebarState returns the current sidebar metadata for the workspace.
func (c *Client) SidebarState() (*SidebarState, error) {
	resp, err := c.Call("sidebar-state", nil)
	if err != nil {
		return nil, err
	}
	var state SidebarState
	if err := unmarshalResult(resp, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SidebarState represents the sidebar metadata dump.
type SidebarState struct {
	CWD       string     `json:"cwd"`
	GitBranch string     `json:"git_branch"`
	Ports     []int      `json:"ports"`
	Status    []Status   `json:"status"`
	Progress  *Progress  `json:"progress"`
	Logs      []LogEntry `json:"logs"`
}

// Status is a sidebar status entry.
type Status struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// Progress is the sidebar progress bar state.
type Progress struct {
	Value float64 `json:"value"`
	Label string  `json:"label"`
}

// LogEntry is a sidebar log item.
type LogEntry struct {
	Message string `json:"message"`
	Level   string `json:"level"`
	Source  string `json:"source"`
}
```

Also add a helper to `client.go`:

```go
// unmarshalResult unmarshals a Response's Result into dst.
func unmarshalResult(resp *Response, dst any) error {
	return json.Unmarshal(resp.Result, dst)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmux/sidebar.go internal/cmux/sidebar_test.go internal/cmux/client.go
git commit -m "[BEHAVIORAL] Add cmux sidebar metadata methods

SetStatus, SetProgress, Log with levels, ClearProgress, ClearLog,
and SidebarState for reading current sidebar metadata.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Notifications

**Files:**
- Create: `internal/cmux/notification.go`
- Create: `internal/cmux/notification_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmux/notification_test.go
package cmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotify(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":         map[string]bool{"pong": true},
		"system.identify":     map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"notification.create": map[string]string{"id": "notif-1"},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.Notify("Rubichan", "Code Review", "3 findings")
	assert.NoError(t, err)
}

func TestClearNotifications(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":         map[string]bool{"pong": true},
		"system.identify":     map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"notification.clear":  true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.ClearNotifications()
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmux/ -run TestNotify -v`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement notification methods**

```go
// internal/cmux/notification.go
package cmux

// Notification represents a cmux notification.
type Notification struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Body     string `json:"body"`
}

// Notify sends a native macOS notification via cmux.
func (c *Client) Notify(title, subtitle, body string) error {
	_, err := c.Call("notification.create", map[string]string{
		"title":    title,
		"subtitle": subtitle,
		"body":     body,
	})
	return err
}

// ListNotifications returns all active notifications.
func (c *Client) ListNotifications() ([]Notification, error) {
	resp, err := c.Call("notification.list", nil)
	if err != nil {
		return nil, err
	}
	var notifs []Notification
	if err := unmarshalResult(resp, &notifs); err != nil {
		return nil, err
	}
	return notifs, nil
}

// ClearNotifications clears all notifications.
func (c *Client) ClearNotifications() error {
	_, err := c.Call("notification.clear", nil)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmux/notification.go internal/cmux/notification_test.go
git commit -m "[BEHAVIORAL] Add cmux notification methods

Notify sends native macOS notifications with title/subtitle/body.
ListNotifications and ClearNotifications for notification management.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Workspace Management

**Files:**
- Create: `internal/cmux/workspace.go`
- Create: `internal/cmux/workspace_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmux/workspace_test.go
package cmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListWorkspaces(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":    map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"workspace.list": map[string]any{
			"workspaces": []map[string]string{
				{"id": "ws-1", "name": "project-a"},
				{"id": "ws-2", "name": "project-b"},
			},
		},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	workspaces, err := c.ListWorkspaces()
	require.NoError(t, err)
	assert.Len(t, workspaces, 2)
	assert.Equal(t, "ws-1", workspaces[0].ID)
	assert.Equal(t, "project-a", workspaces[0].Name)
}

func TestCreateWorkspace(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":      map[string]bool{"pong": true},
		"system.identify":  map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"workspace.create": map[string]string{"id": "ws-new", "name": ""},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	ws, err := c.CreateWorkspace()
	require.NoError(t, err)
	assert.Equal(t, "ws-new", ws.ID)
}

func TestSelectWorkspace(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":      map[string]bool{"pong": true},
		"system.identify":  map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"workspace.select": true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.SelectWorkspace("ws-1")
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmux/ -run TestListWorkspaces -v`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement workspace methods**

```go
// internal/cmux/workspace.go
package cmux

// Workspace represents a cmux workspace (vertical tab).
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListWorkspaces returns all open workspaces.
func (c *Client) ListWorkspaces() ([]Workspace, error) {
	resp, err := c.Call("workspace.list", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, err
	}
	return result.Workspaces, nil
}

// CreateWorkspace creates a new workspace.
func (c *Client) CreateWorkspace() (*Workspace, error) {
	resp, err := c.Call("workspace.create", nil)
	if err != nil {
		return nil, err
	}
	var ws Workspace
	if err := unmarshalResult(resp, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// SelectWorkspace switches to a specific workspace.
func (c *Client) SelectWorkspace(id string) error {
	_, err := c.Call("workspace.select", map[string]string{"workspace_id": id})
	return err
}

// CurrentWorkspace returns the active workspace.
func (c *Client) CurrentWorkspace() (*Workspace, error) {
	resp, err := c.Call("workspace.current", nil)
	if err != nil {
		return nil, err
	}
	var ws Workspace
	if err := unmarshalResult(resp, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// CloseWorkspace closes a workspace by ID.
func (c *Client) CloseWorkspace(id string) error {
	_, err := c.Call("workspace.close", map[string]string{"workspace_id": id})
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmux/workspace.go internal/cmux/workspace_test.go
git commit -m "[BEHAVIORAL] Add cmux workspace management methods

ListWorkspaces, CreateWorkspace, SelectWorkspace, CurrentWorkspace,
CloseWorkspace for managing cmux vertical tabs.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Surface Management

**Files:**
- Create: `internal/cmux/surface.go`
- Create: `internal/cmux/surface_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmux/surface_test.go
package cmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplit(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":    map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"surface.split":  map[string]string{"id": "surf-new", "type": "terminal"},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	surf, err := c.Split("right")
	require.NoError(t, err)
	assert.Equal(t, "surf-new", surf.ID)
	assert.Equal(t, "terminal", surf.Type)
}

func TestSendText(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":       map[string]bool{"pong": true},
		"system.identify":   map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"surface.send_text": true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.SendText("surf-1", "echo hello")
	assert.NoError(t, err)
}

func TestSendKey(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":      map[string]bool{"pong": true},
		"system.identify":  map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"surface.send_key": true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.SendKey("surf-1", "enter")
	assert.NoError(t, err)
}

func TestListSurfaces(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":    map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"surface.list": map[string]any{
			"surfaces": []map[string]string{
				{"id": "surf-1", "type": "terminal"},
				{"id": "surf-2", "type": "browser"},
			},
		},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	surfaces, err := c.ListSurfaces()
	require.NoError(t, err)
	assert.Len(t, surfaces, 2)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmux/ -run TestSplit -v`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement surface methods**

```go
// internal/cmux/surface.go
package cmux

// Surface represents a cmux surface (terminal or browser pane).
type Surface struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "terminal" or "browser"
}

// Split creates a new split pane in the given direction.
// direction: "left", "right", "up", "down".
func (c *Client) Split(direction string) (*Surface, error) {
	resp, err := c.Call("surface.split", map[string]string{"direction": direction})
	if err != nil {
		return nil, err
	}
	var surf Surface
	if err := unmarshalResult(resp, &surf); err != nil {
		return nil, err
	}
	return &surf, nil
}

// ListSurfaces returns all surfaces in the current workspace.
func (c *Client) ListSurfaces() ([]Surface, error) {
	resp, err := c.Call("surface.list", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Surfaces []Surface `json:"surfaces"`
	}
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, err
	}
	return result.Surfaces, nil
}

// FocusSurface switches focus to a specific surface.
func (c *Client) FocusSurface(id string) error {
	_, err := c.Call("surface.focus", map[string]string{"surface_id": id})
	return err
}

// SendText types text into the target surface.
func (c *Client) SendText(surfaceID, text string) error {
	_, err := c.Call("surface.send_text", map[string]string{
		"surface_id": surfaceID,
		"text":       text,
	})
	return err
}

// SendKey sends a key press to the target surface.
// key: "enter", "tab", "escape", "backspace", "delete", "up", "down", "left", "right".
func (c *Client) SendKey(surfaceID, key string) error {
	_, err := c.Call("surface.send_key", map[string]string{
		"surface_id": surfaceID,
		"key":        key,
	})
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmux/surface.go internal/cmux/surface_test.go
git commit -m "[BEHAVIORAL] Add cmux surface management methods

Split, ListSurfaces, FocusSurface, SendText, SendKey for managing
cmux split panes and sending input to terminal surfaces.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 7: Browser Automation

**Files:**
- Create: `internal/cmux/browser.go`
- Create: `internal/cmux/browser_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmux/browser_test.go
package cmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserNavigate(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":       map[string]bool{"pong": true},
		"system.identify":   map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"browser.navigate":  true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserNavigate("surf-1", "https://example.com")
	assert.NoError(t, err)
}

func TestBrowserSnapshot(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":       map[string]bool{"pong": true},
		"system.identify":   map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"browser.snapshot":  map[string]string{"dom": "<html>e10: button 'Submit'</html>"},
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	dom, err := c.BrowserSnapshot("surf-1")
	require.NoError(t, err)
	assert.Contains(t, dom, "e10")
}

func TestBrowserClick(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"browser.click":   true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserClick("surf-1", "e10")
	assert.NoError(t, err)
}

func TestBrowserType(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"browser.type":    true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserType("surf-1", "e14", "hello world")
	assert.NoError(t, err)
}

func TestBrowserWait(t *testing.T) {
	sock := newTestServer(t, map[string]any{
		"system.ping":     map[string]bool{"pong": true},
		"system.identify": map[string]string{"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s"},
		"browser.wait":    true,
	})
	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserWait("surf-1", "complete")
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmux/ -run TestBrowser -v`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement browser methods**

```go
// internal/cmux/browser.go
package cmux

// BrowserNavigate opens a URL in a browser surface.
func (c *Client) BrowserNavigate(surfaceID, url string) error {
	_, err := c.Call("browser.navigate", map[string]string{
		"surface_id": surfaceID,
		"url":        url,
	})
	return err
}

// BrowserSnapshot returns a text-based DOM representation with element references.
func (c *Client) BrowserSnapshot(surfaceID string) (string, error) {
	resp, err := c.Call("browser.snapshot", map[string]string{
		"surface_id": surfaceID,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		DOM string `json:"dom"`
	}
	if err := unmarshalResult(resp, &result); err != nil {
		return "", err
	}
	return result.DOM, nil
}

// BrowserClick clicks an element by its snapshot reference.
func (c *Client) BrowserClick(surfaceID, ref string) error {
	_, err := c.Call("browser.click", map[string]string{
		"surface_id": surfaceID,
		"ref":        ref,
	})
	return err
}

// BrowserType types text into an element by its snapshot reference.
func (c *Client) BrowserType(surfaceID, ref, text string) error {
	_, err := c.Call("browser.type", map[string]string{
		"surface_id": surfaceID,
		"ref":        ref,
		"text":       text,
	})
	return err
}

// BrowserWait waits for a page load state.
// loadState: "complete" or "domcontentloaded".
func (c *Client) BrowserWait(surfaceID, loadState string) error {
	_, err := c.Call("browser.wait", map[string]string{
		"surface_id": surfaceID,
		"load_state": loadState,
	})
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmux/browser.go internal/cmux/browser_test.go
git commit -m "[BEHAVIORAL] Add cmux browser automation methods

BrowserNavigate, BrowserSnapshot, BrowserClick, BrowserType,
BrowserWait for interacting with cmux browser surfaces.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 8: Orchestrator

**Files:**
- Create: `internal/cmux/orchestrator.go`
- Create: `internal/cmux/orchestrator_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmux/orchestrator_test.go
package cmux

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOrchestratorServer handles split + send_text + sidebar-state with evolving state.
func fakeOrchestratorServer(t *testing.T, ln net.Listener, logSequence [][]LogEntry) {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	pollCount := 0
	splitCount := 0

	for {
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}

		switch req.Method {
		case "system.ping":
			enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": map[string]bool{"pong": true}})
		case "system.identify":
			enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": map[string]string{
				"window_id": "w", "workspace_id": "w", "pane_id": "p", "surface_id": "s",
			}})
		case "surface.split":
			splitCount++
			enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": map[string]string{
				"id": fmt.Sprintf("surf-%d", splitCount), "type": "terminal",
			}})
		case "surface.send_text":
			enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": true})
		case "sidebar-state":
			idx := pollCount
			if idx >= len(logSequence) {
				idx = len(logSequence) - 1
			}
			pollCount++
			enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": map[string]any{
				"logs": logSequence[idx],
			}})
		default:
			enc.Encode(map[string]any{"id": req.ID, "ok": true, "result": true})
		}
	}
}

func TestOrchestrator_DispatchAndWait(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	// Poll 1: no completion. Poll 2: task 1 done. Poll 3: task 2 error.
	go fakeOrchestratorServer(t, ln, [][]LogEntry{
		{},
		{{Message: "[DONE] scan complete", Level: "success", Source: "surf-1"}},
		{
			{Message: "[DONE] scan complete", Level: "success", Source: "surf-1"},
			{Message: "[ERROR] review failed", Level: "error", Source: "surf-2"},
		},
	})

	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	orch := NewOrchestrator(c)
	orch.SetPollRate(50 * time.Millisecond) // fast polling for tests

	task1, err := orch.Dispatch("right", "run security scan")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", task1.SurfaceID)
	assert.Equal(t, "running", task1.Status)

	task2, err := orch.Dispatch("down", "run code review")
	require.NoError(t, err)
	assert.Equal(t, "surf-2", task2.SurfaceID)

	results, err := orch.Wait(5 * time.Second)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Find results by surface ID.
	var r1, r2 *Task
	for i := range results {
		switch results[i].SurfaceID {
		case "surf-1":
			r1 = &results[i]
		case "surf-2":
			r2 = &results[i]
		}
	}
	require.NotNil(t, r1)
	require.NotNil(t, r2)
	assert.Equal(t, "done", r1.Status)
	assert.Equal(t, "error", r2.Status)
}

func TestOrchestrator_Timeout(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	// Never complete — always return empty logs.
	go fakeOrchestratorServer(t, ln, [][]LogEntry{{}})

	c, err := Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	orch := NewOrchestrator(c)
	orch.SetPollRate(50 * time.Millisecond)

	_, err = orch.Dispatch("right", "hang forever")
	require.NoError(t, err)

	_, err = orch.Wait(200 * time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmux/ -run TestOrchestrator -v`
Expected: FAIL — types/methods not defined

- [ ] **Step 3: Implement orchestrator**

```go
// internal/cmux/orchestrator.go
package cmux

import (
	"fmt"
	"strings"
	"time"
)

// Task represents a dispatched sub-agent task.
type Task struct {
	ID        string
	SurfaceID string
	Command   string
	Status    string // "running", "done", "error"
	Logs      []LogEntry
}

// Orchestrator coordinates sub-agents across split panes.
type Orchestrator struct {
	client   *Client
	tasks    map[string]*Task // task ID (surface ID) → task
	pollRate time.Duration
}

// NewOrchestrator creates an Orchestrator with a default 2-second poll rate.
func NewOrchestrator(client *Client) *Orchestrator {
	return &Orchestrator{
		client:   client,
		tasks:    make(map[string]*Task),
		pollRate: 2 * time.Second,
	}
}

// SetPollRate configures the polling interval for log checks.
func (o *Orchestrator) SetPollRate(d time.Duration) {
	o.pollRate = d
}

// Dispatch creates a split pane, sends a command, and tracks it as a task.
func (o *Orchestrator) Dispatch(direction, command string) (*Task, error) {
	surf, err := o.client.Split(direction)
	if err != nil {
		return nil, fmt.Errorf("orchestrator split: %w", err)
	}

	if err := o.client.SendText(surf.ID, command); err != nil {
		return nil, fmt.Errorf("orchestrator send: %w", err)
	}
	if err := o.client.SendKey(surf.ID, "enter"); err != nil {
		return nil, fmt.Errorf("orchestrator send enter: %w", err)
	}

	task := &Task{
		ID:        surf.ID,
		SurfaceID: surf.ID,
		Command:   command,
		Status:    "running",
	}
	o.tasks[surf.ID] = task
	return task, nil
}

// Wait blocks until all tasks reach "done" or "error", or timeout.
func (o *Orchestrator) Wait(timeout time.Duration) ([]Task, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if err := o.pollLogs(); err != nil {
			return nil, err
		}

		allDone := true
		for _, task := range o.tasks {
			if task.Status == "running" {
				allDone = false
				break
			}
		}
		if allDone {
			return o.results(), nil
		}

		time.Sleep(o.pollRate)
	}

	return nil, fmt.Errorf("orchestrator: timeout waiting for %d tasks", o.runningCount())
}

// WaitAny blocks until at least one task completes, or timeout.
func (o *Orchestrator) WaitAny(timeout time.Duration) (*Task, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if err := o.pollLogs(); err != nil {
			return nil, err
		}

		for _, task := range o.tasks {
			if task.Status != "running" {
				return task, nil
			}
		}

		time.Sleep(o.pollRate)
	}

	return nil, fmt.Errorf("orchestrator: timeout waiting for any task")
}

// pollLogs fetches sidebar-state and updates task statuses from log entries.
func (o *Orchestrator) pollLogs() error {
	state, err := o.client.SidebarState()
	if err != nil {
		return fmt.Errorf("orchestrator poll: %w", err)
	}

	for _, entry := range state.Logs {
		task, ok := o.tasks[entry.Source]
		if !ok || task.Status != "running" {
			continue
		}

		task.Logs = append(task.Logs, entry)

		if strings.HasPrefix(entry.Message, "[DONE]") {
			task.Status = "done"
		} else if strings.HasPrefix(entry.Message, "[ERROR]") {
			task.Status = "error"
		}
	}

	return nil
}

func (o *Orchestrator) results() []Task {
	tasks := make([]Task, 0, len(o.tasks))
	for _, t := range o.tasks {
		tasks = append(tasks, *t)
	}
	return tasks
}

func (o *Orchestrator) runningCount() int {
	count := 0
	for _, t := range o.tasks {
		if t.Status == "running" {
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: Add missing imports to test file**

The test file needs `encoding/json`, `fmt`, `net`, and `path/filepath` imports. Add them to the import block of `orchestrator_test.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cmux/ -run TestOrchestrator -v -timeout 30s`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cmux/orchestrator.go internal/cmux/orchestrator_test.go
git commit -m "[BEHAVIORAL] Add cmux orchestrator for multi-pane coordination

Dispatch splits panes and sends commands. Wait polls sidebar-state
for [DONE]/[ERROR] log entries. Configurable poll rate for testing.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 9: CmuxSocket Detection in Caps

**Files:**
- Modify: `internal/terminal/caps.go`
- Modify: `internal/terminal/caps_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/terminal/caps_test.go — append

func TestDetectWithEnv_CmuxSocket_Detected(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "ws-123")

	// Create a fake cmux socket that responds to system.ping.
	sock := filepath.Join(t.TempDir(), "cmux.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		for {
			var req map[string]any
			if err := dec.Decode(&req); err != nil {
				return
			}
			enc.Encode(map[string]any{"id": req["id"], "ok": true, "result": map[string]bool{"pong": true}})
		}
	}()

	t.Setenv("CMUX_SOCKET_PATH", sock)
	caps := DetectWithEnv("ghostty", nil)
	assert.True(t, caps.CmuxSocket)
}

func TestDetectWithEnv_CmuxSocket_NoEnvVar(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "")
	caps := DetectWithEnv("ghostty", nil)
	assert.False(t, caps.CmuxSocket)
}

func TestDetectWithEnv_CmuxSocket_BadSocket(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "ws-123")
	t.Setenv("CMUX_SOCKET_PATH", "/nonexistent/cmux.sock")
	caps := DetectWithEnv("ghostty", nil)
	assert.False(t, caps.CmuxSocket)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/terminal/ -run TestDetectWithEnv_CmuxSocket -v`
Expected: FAIL — `CmuxSocket` field does not exist

- [ ] **Step 3: Add CmuxSocket to Caps and detection logic**

In `internal/terminal/caps.go`, add the field:

```go
type Caps struct {
	Hyperlinks      bool // OSC 8
	KittyGraphics   bool // Kitty graphics protocol
	KittyKeyboard   bool // Kitty keyboard protocol
	ProgressBar     bool // OSC 9;4 (ConEmu/Ghostty)
	Notifications   bool // OSC 9
	SyncRendering   bool // Mode 2026
	LightDarkMode   bool // OSC 11 background color query
	ClipboardAccess bool // OSC 52
	FocusEvents     bool // Mode 1004
	DarkBackground  bool // detected via OSC 11 query (defaults true)
	CmuxSocket      bool // cmux Unix socket API available
}
```

At the end of `DetectWithEnv`, before the final return, add cmux detection:

```go
func DetectWithEnv(termProgram string, prober Prober) *Caps {
	// ... existing code ...

	// Detect cmux socket availability.
	caps.CmuxSocket = detectCmuxSocket()

	return caps
}

// detectCmuxSocket checks if running inside cmux by verifying the
// CMUX_WORKSPACE_ID env var is set and the socket responds to ping.
func detectCmuxSocket() bool {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		return false
	}
	socketPath := os.Getenv("CMUX_SOCKET_PATH")
	if socketPath == "" {
		socketPath = "/tmp/cmux.sock"
	}
	return pingCmuxSocket(socketPath)
}

// pingCmuxSocket attempts a system.ping call to the cmux socket.
// Returns false on any error (connection refused, timeout, etc.).
func pingCmuxSocket(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(map[string]any{
		"id": "detect-1", "method": "system.ping", "params": struct{}{},
	}); err != nil {
		return false
	}

	var resp struct {
		OK bool `json:"ok"`
	}
	if err := dec.Decode(&resp); err != nil {
		return false
	}
	return resp.OK
}
```

Add imports: `"encoding/json"`, `"net"`, `"time"`.

Note: The `detectCmuxSocket()` call needs to be placed at two points — once in the known-terminal early return path and once at the end of the function. Insert it just before each `return caps` statement.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/terminal/ -run TestDetectWithEnv_CmuxSocket -v`
Expected: All PASS

- [ ] **Step 5: Run full terminal test suite**

Run: `go test ./internal/terminal/ -v`
Expected: All PASS — no regressions

- [ ] **Step 6: Commit**

```bash
git add internal/terminal/caps.go internal/terminal/caps_test.go
git commit -m "[BEHAVIORAL] Add CmuxSocket detection to terminal Caps

Checks CMUX_WORKSPACE_ID env var and pings the cmux socket to verify
availability. 500ms timeout for startup detection.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 10: Browser Tools

**Files:**
- Create: `internal/tools/cmux_browser.go`
- Create: `internal/tools/cmux_browser_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/cmux_browser_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmuxBrowserNavigateTool_Name(t *testing.T) {
	m := cmuxtest.NewMockClient()
	tool := NewCmuxBrowserNavigate(m)
	assert.Equal(t, "cmux_browser_navigate", tool.Name())
}

func TestCmuxBrowserNavigateTool_Execute(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("surface.split", map[string]string{"id": "surf-new", "type": "browser"})
	m.SetResult("browser.navigate", true)
	m.SetResult("browser.wait", true)

	tool := NewCmuxBrowserNavigate(m)
	input := json.RawMessage(`{"url":"https://example.com"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "surf-new")
}

func TestCmuxBrowserNavigateTool_WithSurfaceID(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("browser.navigate", true)
	m.SetResult("browser.wait", true)

	tool := NewCmuxBrowserNavigate(m)
	input := json.RawMessage(`{"url":"https://example.com","surface_id":"existing-surf"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Should NOT have called surface.split since surface_id was provided.
	for _, call := range m.Calls() {
		assert.NotEqual(t, "surface.split", call.Method)
	}
}

func TestCmuxBrowserSnapshotTool_Execute(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("browser.snapshot", map[string]string{"dom": "<html>e10: button</html>"})

	tool := NewCmuxBrowserSnapshot(m)
	input := json.RawMessage(`{"surface_id":"surf-1"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "e10")
}

func TestCmuxBrowserClickTool_Execute(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("browser.click", true)

	tool := NewCmuxBrowserClick(m)
	input := json.RawMessage(`{"surface_id":"surf-1","ref":"e10"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestCmuxBrowserTypeTool_Execute(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("browser.type", true)

	tool := NewCmuxBrowserType(m)
	input := json.RawMessage(`{"surface_id":"surf-1","ref":"e14","text":"hello"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestCmuxBrowserWaitTool_Execute(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("browser.wait", true)

	tool := NewCmuxBrowserWait(m)
	input := json.RawMessage(`{"surface_id":"surf-1","load_state":"complete"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run TestCmuxBrowser -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement browser tools**

```go
// internal/tools/cmux_browser.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/cmux"
)

// CmuxBrowserNavigateTool opens a URL in a cmux browser surface.
type CmuxBrowserNavigateTool struct {
	client cmux.Caller
}

func NewCmuxBrowserNavigate(client cmux.Caller) *CmuxBrowserNavigateTool {
	return &CmuxBrowserNavigateTool{client: client}
}

func (t *CmuxBrowserNavigateTool) Name() string { return "cmux_browser_navigate" }

func (t *CmuxBrowserNavigateTool) Description() string {
	return "Open a URL in a cmux browser pane. If no surface_id is provided, " +
		"a new right split is created automatically."
}

func (t *CmuxBrowserNavigateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL to navigate to"},
			"surface_id": {"type": "string", "description": "Browser surface ID (optional — creates new split if omitted)"}
		},
		"required": ["url"]
	}`)
}

func (t *CmuxBrowserNavigateTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		URL       string `json:"url"`
		SurfaceID string `json:"surface_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.URL == "" {
		return ToolResult{Content: "url is required", IsError: true}, nil
	}

	surfaceID := in.SurfaceID
	if surfaceID == "" {
		resp, err := t.client.Call("surface.split", map[string]string{"direction": "right"})
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("failed to create split: %s", err), IsError: true}, nil
		}
		var surf cmux.Surface
		if err := json.Unmarshal(resp.Result, &surf); err != nil {
			return ToolResult{Content: fmt.Sprintf("failed to parse surface: %s", err), IsError: true}, nil
		}
		surfaceID = surf.ID
	}

	if _, err := t.client.Call("browser.navigate", map[string]string{
		"surface_id": surfaceID,
		"url":        in.URL,
	}); err != nil {
		return ToolResult{Content: fmt.Sprintf("navigate failed: %s", err), IsError: true}, nil
	}

	if _, err := t.client.Call("browser.wait", map[string]string{
		"surface_id": surfaceID,
		"load_state": "complete",
	}); err != nil {
		return ToolResult{Content: fmt.Sprintf("wait failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Navigated to %s in surface %s", in.URL, surfaceID)}, nil
}

// CmuxBrowserSnapshotTool returns a DOM snapshot from a browser surface.
type CmuxBrowserSnapshotTool struct {
	client cmux.Caller
}

func NewCmuxBrowserSnapshot(client cmux.Caller) *CmuxBrowserSnapshotTool {
	return &CmuxBrowserSnapshotTool{client: client}
}

func (t *CmuxBrowserSnapshotTool) Name() string { return "cmux_browser_snapshot" }

func (t *CmuxBrowserSnapshotTool) Description() string {
	return "Get a text-based DOM snapshot from a cmux browser surface. " +
		"Returns element references (e.g., e10, e14) for use with click/type tools."
}

func (t *CmuxBrowserSnapshotTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {"type": "string", "description": "Browser surface ID"}
		},
		"required": ["surface_id"]
	}`)
}

func (t *CmuxBrowserSnapshotTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		SurfaceID string `json:"surface_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	resp, err := t.client.Call("browser.snapshot", map[string]string{"surface_id": in.SurfaceID})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("snapshot failed: %s", err), IsError: true}, nil
	}
	var result struct {
		DOM string `json:"dom"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return ToolResult{Content: fmt.Sprintf("parse failed: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: result.DOM}, nil
}

// CmuxBrowserClickTool clicks an element in a browser surface.
type CmuxBrowserClickTool struct {
	client cmux.Caller
}

func NewCmuxBrowserClick(client cmux.Caller) *CmuxBrowserClickTool {
	return &CmuxBrowserClickTool{client: client}
}

func (t *CmuxBrowserClickTool) Name() string { return "cmux_browser_click" }

func (t *CmuxBrowserClickTool) Description() string {
	return "Click an element in a cmux browser surface by its snapshot reference."
}

func (t *CmuxBrowserClickTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {"type": "string", "description": "Browser surface ID"},
			"ref": {"type": "string", "description": "Element reference from snapshot (e.g., e10)"}
		},
		"required": ["surface_id", "ref"]
	}`)
}

func (t *CmuxBrowserClickTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		SurfaceID string `json:"surface_id"`
		Ref       string `json:"ref"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if _, err := t.client.Call("browser.click", map[string]string{
		"surface_id": in.SurfaceID,
		"ref":        in.Ref,
	}); err != nil {
		return ToolResult{Content: fmt.Sprintf("click failed: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Clicked %s in surface %s", in.Ref, in.SurfaceID)}, nil
}

// CmuxBrowserTypeTool types text into an element in a browser surface.
type CmuxBrowserTypeTool struct {
	client cmux.Caller
}

func NewCmuxBrowserType(client cmux.Caller) *CmuxBrowserTypeTool {
	return &CmuxBrowserTypeTool{client: client}
}

func (t *CmuxBrowserTypeTool) Name() string { return "cmux_browser_type" }

func (t *CmuxBrowserTypeTool) Description() string {
	return "Type text into an element in a cmux browser surface by its snapshot reference."
}

func (t *CmuxBrowserTypeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {"type": "string", "description": "Browser surface ID"},
			"ref": {"type": "string", "description": "Element reference from snapshot (e.g., e14)"},
			"text": {"type": "string", "description": "Text to type into the element"}
		},
		"required": ["surface_id", "ref", "text"]
	}`)
}

func (t *CmuxBrowserTypeTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		SurfaceID string `json:"surface_id"`
		Ref       string `json:"ref"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if _, err := t.client.Call("browser.type", map[string]string{
		"surface_id": in.SurfaceID,
		"ref":        in.Ref,
		"text":       in.Text,
	}); err != nil {
		return ToolResult{Content: fmt.Sprintf("type failed: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Typed into %s in surface %s", in.Ref, in.SurfaceID)}, nil
}

// CmuxBrowserWaitTool waits for a page load state.
type CmuxBrowserWaitTool struct {
	client cmux.Caller
}

func NewCmuxBrowserWait(client cmux.Caller) *CmuxBrowserWaitTool {
	return &CmuxBrowserWaitTool{client: client}
}

func (t *CmuxBrowserWaitTool) Name() string { return "cmux_browser_wait" }

func (t *CmuxBrowserWaitTool) Description() string {
	return "Wait for a page load state in a cmux browser surface."
}

func (t *CmuxBrowserWaitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {"type": "string", "description": "Browser surface ID"},
			"load_state": {"type": "string", "enum": ["complete", "domcontentloaded"], "description": "Page load state to wait for"}
		},
		"required": ["surface_id", "load_state"]
	}`)
}

func (t *CmuxBrowserWaitTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		SurfaceID string `json:"surface_id"`
		LoadState string `json:"load_state"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if _, err := t.client.Call("browser.wait", map[string]string{
		"surface_id": in.SurfaceID,
		"load_state": in.LoadState,
	}); err != nil {
		return ToolResult{Content: fmt.Sprintf("wait failed: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Page loaded (%s) in surface %s", in.LoadState, in.SurfaceID)}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run TestCmuxBrowser -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/cmux_browser.go internal/tools/cmux_browser_test.go
git commit -m "[BEHAVIORAL] Add cmux browser automation tools

5 tools: navigate (auto-splits), snapshot, click, type, wait.
All use cmux.Caller interface for testability.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 11: Surface and Orchestrate Tools

**Files:**
- Create: `internal/tools/cmux_surface.go`
- Create: `internal/tools/cmux_surface_test.go`
- Create: `internal/tools/cmux_orchestrate.go`
- Create: `internal/tools/cmux_orchestrate_test.go`

- [ ] **Step 1: Write the failing tests for surface tools**

```go
// internal/tools/cmux_surface_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmuxSplitTool_Execute(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("surface.split", map[string]string{"id": "surf-new", "type": "terminal"})

	tool := NewCmuxSplit(m)
	assert.Equal(t, "cmux_split", tool.Name())

	input := json.RawMessage(`{"direction":"right"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "surf-new")
}

func TestCmuxSendTool_Text(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("surface.send_text", true)

	tool := NewCmuxSend(m)
	assert.Equal(t, "cmux_send", tool.Name())

	input := json.RawMessage(`{"surface_id":"surf-1","text":"echo hello"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestCmuxSendTool_Key(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("surface.send_key", true)

	tool := NewCmuxSend(m)
	input := json.RawMessage(`{"surface_id":"surf-1","key":"enter"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run TestCmuxS -v`
Expected: FAIL

- [ ] **Step 3: Implement surface tools**

```go
// internal/tools/cmux_surface.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/cmux"
)

// CmuxSplitTool creates a split pane in cmux.
type CmuxSplitTool struct {
	client cmux.Caller
}

func NewCmuxSplit(client cmux.Caller) *CmuxSplitTool {
	return &CmuxSplitTool{client: client}
}

func (t *CmuxSplitTool) Name() string { return "cmux_split" }

func (t *CmuxSplitTool) Description() string {
	return "Create a split pane in cmux. Returns the new surface ID."
}

func (t *CmuxSplitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"direction": {"type": "string", "enum": ["left", "right", "up", "down"], "description": "Split direction"}
		},
		"required": ["direction"]
	}`)
}

func (t *CmuxSplitTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	resp, err := t.client.Call("surface.split", map[string]string{"direction": in.Direction})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("split failed: %s", err), IsError: true}, nil
	}
	var surf cmux.Surface
	if err := json.Unmarshal(resp.Result, &surf); err != nil {
		return ToolResult{Content: fmt.Sprintf("parse failed: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Created %s split — surface_id: %s", in.Direction, surf.ID)}, nil
}

// CmuxSendTool sends text or a key press to a cmux surface.
type CmuxSendTool struct {
	client cmux.Caller
}

func NewCmuxSend(client cmux.Caller) *CmuxSendTool {
	return &CmuxSendTool{client: client}
}

func (t *CmuxSendTool) Name() string { return "cmux_send" }

func (t *CmuxSendTool) Description() string {
	return "Send text or a key press to a cmux surface. Provide either 'text' or 'key', not both."
}

func (t *CmuxSendTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {"type": "string", "description": "Target surface ID"},
			"text": {"type": "string", "description": "Text to type (mutually exclusive with key)"},
			"key": {"type": "string", "enum": ["enter", "tab", "escape", "backspace", "delete", "up", "down", "left", "right"], "description": "Key to press (mutually exclusive with text)"}
		},
		"required": ["surface_id"]
	}`)
}

func (t *CmuxSendTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		SurfaceID string `json:"surface_id"`
		Text      string `json:"text"`
		Key       string `json:"key"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Text != "" {
		if _, err := t.client.Call("surface.send_text", map[string]string{
			"surface_id": in.SurfaceID,
			"text":       in.Text,
		}); err != nil {
			return ToolResult{Content: fmt.Sprintf("send_text failed: %s", err), IsError: true}, nil
		}
		return ToolResult{Content: fmt.Sprintf("Sent text to surface %s", in.SurfaceID)}, nil
	}

	if in.Key != "" {
		if _, err := t.client.Call("surface.send_key", map[string]string{
			"surface_id": in.SurfaceID,
			"key":        in.Key,
		}); err != nil {
			return ToolResult{Content: fmt.Sprintf("send_key failed: %s", err), IsError: true}, nil
		}
		return ToolResult{Content: fmt.Sprintf("Sent key '%s' to surface %s", in.Key, in.SurfaceID)}, nil
	}

	return ToolResult{Content: "provide either 'text' or 'key'", IsError: true}, nil
}
```

- [ ] **Step 4: Run surface tool tests**

Run: `go test ./internal/tools/ -run TestCmuxS -v`
Expected: All PASS

- [ ] **Step 5: Write orchestrate tool tests**

```go
// internal/tools/cmux_orchestrate_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmuxOrchestrateTool_Name(t *testing.T) {
	m := cmuxtest.NewMockClient()
	tool := NewCmuxOrchestrate(m)
	assert.Equal(t, "cmux_orchestrate", tool.Name())
}
```

- [ ] **Step 6: Implement orchestrate tool**

```go
// internal/tools/cmux_orchestrate.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/cmux"
)

// CmuxOrchestrateTool runs commands in parallel cmux panes and collects results.
type CmuxOrchestrateTool struct {
	client cmux.Caller
}

func NewCmuxOrchestrate(client cmux.Caller) *CmuxOrchestrateTool {
	return &CmuxOrchestrateTool{client: client}
}

func (t *CmuxOrchestrateTool) Name() string { return "cmux_orchestrate" }

func (t *CmuxOrchestrateTool) Description() string {
	return "Run commands in parallel cmux panes and collect results. " +
		"Each task gets its own split pane. Results are collected via log-based signaling."
}

func (t *CmuxOrchestrateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"tasks": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"direction": {"type": "string", "enum": ["left", "right", "up", "down"]},
						"command": {"type": "string"}
					},
					"required": ["direction", "command"]
				},
				"description": "List of tasks to run in parallel panes"
			},
			"timeout": {"type": "string", "description": "Timeout duration (e.g., '5m', '30s'). Default: 5m"}
		},
		"required": ["tasks"]
	}`)
}

func (t *CmuxOrchestrateTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		Tasks []struct {
			Direction string `json:"direction"`
			Command   string `json:"command"`
		} `json:"tasks"`
		Timeout string `json:"timeout"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if len(in.Tasks) == 0 {
		return ToolResult{Content: "at least one task is required", IsError: true}, nil
	}

	timeout := 5 * time.Minute
	if in.Timeout != "" {
		d, err := time.ParseDuration(in.Timeout)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("invalid timeout: %s", err), IsError: true}, nil
		}
		timeout = d
	}

	// Build a real Client from the Caller — the orchestrator needs Client methods.
	// If the caller is a MockClient (testing), we can't use the orchestrator directly.
	// For now, dispatch tasks manually using the Caller interface.
	var dispatched []struct {
		surfaceID string
		command   string
	}

	for _, task := range in.Tasks {
		resp, err := t.client.Call("surface.split", map[string]string{"direction": task.Direction})
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("split failed: %s", err), IsError: true}, nil
		}
		var surf cmux.Surface
		if err := json.Unmarshal(resp.Result, &surf); err != nil {
			return ToolResult{Content: fmt.Sprintf("parse surface failed: %s", err), IsError: true}, nil
		}

		if _, err := t.client.Call("surface.send_text", map[string]string{
			"surface_id": surf.ID,
			"text":       task.Command,
		}); err != nil {
			return ToolResult{Content: fmt.Sprintf("send failed: %s", err), IsError: true}, nil
		}
		if _, err := t.client.Call("surface.send_key", map[string]string{
			"surface_id": surf.ID,
			"key":        "enter",
		}); err != nil {
			return ToolResult{Content: fmt.Sprintf("send enter failed: %s", err), IsError: true}, nil
		}

		dispatched = append(dispatched, struct {
			surfaceID string
			command   string
		}{surf.ID, task.Command})
	}

	// Poll for completion.
	deadline := time.Now().Add(timeout)
	completed := make(map[string]string) // surfaceID → status

	for time.Now().Before(deadline) && len(completed) < len(dispatched) {
		resp, err := t.client.Call("sidebar-state", nil)
		if err != nil {
			break
		}
		var state cmux.SidebarState
		if err := json.Unmarshal(resp.Result, &state); err != nil {
			break
		}

		for _, entry := range state.Logs {
			if _, done := completed[entry.Source]; done {
				continue
			}
			for _, d := range dispatched {
				if d.surfaceID == entry.Source {
					if strings.HasPrefix(entry.Message, "[DONE]") {
						completed[entry.Source] = "done"
					} else if strings.HasPrefix(entry.Message, "[ERROR]") {
						completed[entry.Source] = "error"
					}
				}
			}
		}

		if len(completed) < len(dispatched) {
			time.Sleep(2 * time.Second)
		}
	}

	// Format results.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Orchestrated %d tasks:\n", len(dispatched)))
	for _, d := range dispatched {
		status := completed[d.surfaceID]
		if status == "" {
			status = "timeout"
		}
		sb.WriteString(fmt.Sprintf("  [%s] surface=%s cmd=%q\n", status, d.surfaceID, d.command))
	}

	hasError := false
	for _, status := range completed {
		if status == "error" {
			hasError = true
			break
		}
	}
	if len(completed) < len(dispatched) {
		hasError = true
	}

	return ToolResult{Content: sb.String(), IsError: hasError}, nil
}
```

- [ ] **Step 7: Run all tool tests**

Run: `go test ./internal/tools/ -run TestCmux -v`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/tools/cmux_surface.go internal/tools/cmux_surface_test.go internal/tools/cmux_orchestrate.go internal/tools/cmux_orchestrate_test.go
git commit -m "[BEHAVIORAL] Add cmux surface and orchestrate tools

cmux_split, cmux_send for pane management. cmux_orchestrate dispatches
parallel commands and polls sidebar-state for log-based completion.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 12: Wire cmux into TUI Model

**Files:**
- Modify: `internal/tui/model.go:142-417`
- Modify: `internal/tui/update.go:162-181`
- Modify: `internal/tui/bootstrapprogress.go:46-82`

- [ ] **Step 1: Add cmuxClient field and SetCmuxClient to Model**

In `internal/tui/model.go`, add import for `"github.com/julianshen/rubichan/internal/cmux"` and add the field after `termCaps`:

```go
type Model struct {
	// ... existing fields ...
	termCaps          *terminal.Caps
	cmuxClient        cmux.Caller    // nil when not running in cmux
	// ... rest of fields ...
}
```

Add setter:

```go
// SetCmuxClient sets the cmux client for rich sidebar/notification dispatch.
// Pass nil when not running inside cmux.
func (m *Model) SetCmuxClient(client cmux.Caller) {
	m.cmuxClient = client
}
```

- [ ] **Step 2: Update notifyIfSupported to prefer cmux**

Replace the existing `notifyIfSupported` method:

```go
func (m *Model) notifyIfSupported(message string) {
	if m.cmuxClient != nil {
		m.cmuxClient.Call("notification.create", map[string]string{
			"title": "Rubichan",
			"body":  message,
		})
		return
	}
	if m.termCaps != nil && m.termCaps.Notifications {
		terminal.Notify(os.Stderr, message)
	}
}
```

- [ ] **Step 3: Update BootstrapProgressOverlay to use cmux**

In `internal/tui/bootstrapprogress.go`, add `cmuxClient cmux.Caller` field to `BootstrapProgressOverlay`:

```go
type BootstrapProgressOverlay struct {
	messages   []string
	phase      string
	done       bool
	error      string
	width      int
	height     int
	caps       *terminal.Caps
	cmuxClient cmux.Caller
}
```

Update `NewBootstrapProgressOverlay` to accept it:

```go
func NewBootstrapProgressOverlay(width, height int, caps *terminal.Caps, cmuxClient cmux.Caller) *BootstrapProgressOverlay {
	return &BootstrapProgressOverlay{
		messages:   []string{"🚀 Knowledge Graph Bootstrap Started"},
		width:      width,
		height:     height,
		caps:       caps,
		cmuxClient: cmuxClient,
	}
}
```

In the `Update` method, replace progress bar dispatch:

```go
// Replace:
if b.caps != nil && b.caps.ProgressBar {
	terminal.SetProgress(os.Stderr, terminal.ProgressNormal, percent)
}

// With:
if b.cmuxClient != nil {
	b.cmuxClient.Call("set-progress", map[string]any{
		"value": float64(percent) / 100.0,
		"label": msg.Message,
	})
} else if b.caps != nil && b.caps.ProgressBar {
	terminal.SetProgress(os.Stderr, terminal.ProgressNormal, percent)
}
```

Apply the same pattern for `ClearProgress` calls (error and complete branches).

- [ ] **Step 4: Find and update all callers of NewBootstrapProgressOverlay**

Search for `NewBootstrapProgressOverlay` calls and add the `cmuxClient` parameter. There should be one or two call sites in the TUI code.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tui/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/bootstrapprogress.go
git commit -m "[BEHAVIORAL] Wire cmux client into TUI model

SetCmuxClient setter. notifyIfSupported prefers cmux notifications.
Bootstrap progress uses cmux sidebar when available.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 13: Wire cmux into Mode Entrypoints

**Files:**
- Modify: `cmd/rubichan/main.go:1371-1372` (runInteractive)
- Modify: `cmd/rubichan/main.go:1900` (runHeadless)
- Modify: `cmd/rubichan/main.go:3110-3157` (runWikiHeadless)

- [ ] **Step 1: Wire cmux into runInteractive**

After `caps := terminal.Detect()` (line 1372) and before model creation (line 1681), add:

```go
var cmuxClient cmux.Caller
if caps.CmuxSocket {
	if cc, err := cmux.Dial(cmux.SocketPath()); err == nil {
		cmuxClient = cc
		defer cc.Close()
	}
}
```

After `model.SetTermCaps(caps)` (line 1682), add:

```go
model.SetCmuxClient(cmuxClient)
```

After tool registration, conditionally register cmux tools:

```go
if cmuxClient != nil {
	for _, t := range []Tool{
		NewCmuxBrowserNavigate(cmuxClient),
		NewCmuxBrowserSnapshot(cmuxClient),
		NewCmuxBrowserClick(cmuxClient),
		NewCmuxBrowserType(cmuxClient),
		NewCmuxBrowserWait(cmuxClient),
		NewCmuxSplit(cmuxClient),
		NewCmuxSend(cmuxClient),
		NewCmuxOrchestrate(cmuxClient),
	} {
		if err := registry.Register(t); err != nil {
			log.Printf("warning: registering cmux tool %q: %v", t.Name(), err)
		}
	}
}
```

Add import: `"github.com/julianshen/rubichan/internal/cmux"`.

- [ ] **Step 2: Wire cmux into runHeadless**

After `caps := terminal.Detect()` (line 1900), add:

```go
var cmuxClient cmux.Caller
if caps.CmuxSocket {
	if cc, err := cmux.Dial(cmux.SocketPath()); err == nil {
		cmuxClient = cc
		defer cc.Close()
	}
}
```

Replace the notification block at line 2278:

```go
// Before:
if caps.Notifications {
	terminal.Notify(os.Stderr, "Code review complete")
	// ...
}

// After:
if cmuxClient != nil {
	cmuxClient.Call("notification.create", map[string]string{
		"title": "Rubichan",
		"subtitle": "Code Review",
		"body": "Code review complete",
	})
} else if caps.Notifications {
	terminal.Notify(os.Stderr, "Code review complete")
	// ...
}
```

- [ ] **Step 3: Wire cmux into runWikiHeadless**

After `caps := terminal.Detect()` (line 3111), add:

```go
var cmuxClient cmux.Caller
if caps.CmuxSocket {
	if cc, err := cmux.Dial(cmux.SocketPath()); err == nil {
		cmuxClient = cc
		defer cc.Close()
	}
}
```

In the ProgressFunc closure, replace progress dispatch:

```go
ProgressFunc: func(stage string, current, total int) {
	if total > 0 {
		fmt.Fprintf(os.Stderr, "[%s] %d/%d\n", stage, current, total)
		percent := current * 100 / total
		if cmuxClient != nil {
			cmuxClient.Call("set-progress", map[string]any{
				"value": float64(percent) / 100.0,
				"label": stage,
			})
		} else if caps.ProgressBar {
			terminal.SetProgress(os.Stderr, terminal.ProgressNormal, percent)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[%s]\n", stage)
		if cmuxClient != nil {
			cmuxClient.Call("set-progress", map[string]any{
				"value": 0.0,
				"label": stage,
			})
		} else if caps.ProgressBar {
			terminal.SetProgress(os.Stderr, terminal.ProgressIndeterminate, 0)
		}
	}
},
```

Replace the ClearProgress and notification blocks:

```go
if cmuxClient != nil {
	cmuxClient.Call("clear-progress", nil)
} else if caps.ProgressBar {
	terminal.ClearProgress(os.Stderr)
}

if err != nil {
	if cmuxClient != nil {
		cmuxClient.Call("notification.create", map[string]string{
			"title": "Rubichan", "body": "Wiki generation failed",
		})
	} else if caps.Notifications {
		terminal.Notify(os.Stderr, "Wiki generation failed")
	}
	return err
}

if cmuxClient != nil {
	cmuxClient.Call("notification.create", map[string]string{
		"title": "Rubichan",
		"subtitle": "Wiki Generation",
		"body": fmt.Sprintf("Wiki complete — %d documents rendered", result.Documents),
	})
} else if caps.Notifications {
	terminal.Notify(os.Stderr, fmt.Sprintf("Wiki complete — %d documents rendered", result.Documents))
}
```

- [ ] **Step 4: Build to verify compilation**

Run: `go build ./cmd/rubichan/`
Expected: SUCCESS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... 2>&1 | tail -30`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire cmux client into all three mode entrypoints

Interactive: register cmux tools, set client on TUI model.
Headless: cmux notifications for code review completion.
Wiki: cmux sidebar progress and completion notifications.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 14: Integration Tests (opt-in)

**Files:**
- Create: `internal/cmux/integration_test.go`

- [ ] **Step 1: Write opt-in integration tests**

```go
//go:build cmux_integration

// internal/cmux/integration_test.go
package cmux

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Ping(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	assert.NotEmpty(t, c.Identity().WorkspaceID)
	assert.NotEmpty(t, c.Identity().SurfaceID)
}

func TestIntegration_Sidebar(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.SetStatus("test", "integration", "checkmark.circle", "#00ff00"))
	require.NoError(t, c.SetProgress(0.5, "Testing..."))
	require.NoError(t, c.Log("Integration test running", "info", "test"))

	state, err := c.SidebarState()
	require.NoError(t, err)
	assert.NotNil(t, state)

	require.NoError(t, c.ClearStatus("test"))
	require.NoError(t, c.ClearProgress())
	require.NoError(t, c.ClearLog())
}

func TestIntegration_Notify(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Notify("Rubichan Test", "Integration", "This is a test notification"))
	require.NoError(t, c.ClearNotifications())
}

func TestIntegration_ListWorkspaces(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	workspaces, err := c.ListWorkspaces()
	require.NoError(t, err)
	assert.NotEmpty(t, workspaces)
}
```

- [ ] **Step 2: Verify tests skip outside cmux**

Run: `go test ./internal/cmux/ -run TestIntegration -v -tags cmux_integration`
Expected: All SKIP (unless running inside cmux)

- [ ] **Step 3: Commit**

```bash
git add internal/cmux/integration_test.go
git commit -m "[BEHAVIORAL] Add opt-in cmux integration tests

Gated behind cmux_integration build tag. Tests skip when
CMUX_WORKSPACE_ID is not set.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---
