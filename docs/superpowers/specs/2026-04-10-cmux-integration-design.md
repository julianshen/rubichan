# cmux Integration Design

**Date**: 2026-04-10
**Status**: DRAFT

## Goal

Integrate Rubichan with cmux's Unix socket API to provide rich sidebar feedback, native notifications, browser automation tools, and active multi-agent orchestration when running inside cmux terminals.

## Background

cmux is a native macOS terminal built on Ghostty's `libghostty` rendering engine, designed for orchestrating AI coding agents. It exposes a JSON-RPC API over Unix sockets (`/tmp/cmux.sock`) with commands for workspace management, split panes, sidebar metadata, notifications, and browser automation.

Rubichan already detects Ghostty via `TERM_PROGRAM` and supports OSC 9/9;4 escape sequences. cmux adds a richer control plane on top — this integration upgrades Rubichan's feedback and automation capabilities when cmux is the host terminal.

## Architecture

```
internal/cmux/              — JSON-RPC socket client + feature modules
  client.go                 — Dial, Call, Close, Identity
  workspace.go              — workspace.list/create/select/close
  surface.go                — surface.split/list/focus/send_text/send_key
  sidebar.go                — set-status, set-progress, log, clear-*
  notification.go           — notification.create/list/clear
  browser.go                — browser navigate/click/type/snapshot/wait
  orchestrator.go           — dispatch sub-agents, poll logs, collect results
  cmuxtest/mock.go          — MockClient for unit tests

internal/terminal/caps.go   — add CmuxSocket bool to Caps

internal/tools/
  cmux_browser.go           — 5 browser tools for the agent
  cmux_surface.go           — split + send tools for the agent
  cmux_orchestrate.go       — orchestrate tool for parallel pane execution
```

### Detection

Add `CmuxSocket bool` to `terminal.Caps`. During `DetectWithEnv`, after the Ghostty fast path:

1. Check `CMUX_WORKSPACE_ID` environment variable is set
2. Verify socket at `CMUX_SOCKET_PATH` (or default `/tmp/cmux.sock`) responds to `system.ping`

If both pass, `CmuxSocket = true`. One env check + one socket round-trip at startup.

### Priority: cmux Replaces Escape Sequences

When `CmuxSocket` is true, cmux's API replaces the equivalent escape sequences:

| Feature | Without cmux | With cmux |
|---------|-------------|-----------|
| Progress | OSC 9;4 (titlebar) | `set-progress` (sidebar + label) |
| Notifications | OSC 9 (simple text) | `notification.create` (title/subtitle/body + colored rings) |
| Status | N/A | `set-status` (icon + color in sidebar tab) |
| Logging | N/A | `log` (structured sidebar log entries) |

No doubling — cmux is the preferred channel, OSC is the fallback.

## Component Details

### 1. JSON-RPC Socket Client (`internal/cmux/client.go`)

Thin, stateless client over Unix domain sockets.

```go
type Client struct {
    conn    net.Conn
    mu      sync.Mutex
    nextID  int64
    timeout time.Duration  // default 5s
    ident   *Identity      // cached from Dial
}

type Response struct {
    ID     string          `json:"id"`
    OK     bool            `json:"ok"`
    Result json.RawMessage `json:"result"`
    Error  string          `json:"error,omitempty"`
}

type Identity struct {
    WindowID    string `json:"window_id"`
    WorkspaceID string `json:"workspace_id"`
    PaneID      string `json:"pane_id"`
    SurfaceID   string `json:"surface_id"`
}

func SocketPath() string          // CMUX_SOCKET_PATH or /tmp/cmux.sock
func Dial(socketPath string) (*Client, error)  // connect + system.identify
func (c *Client) Call(method string, params any) (*Response, error)
func (c *Client) Close() error
func (c *Client) Identity() *Identity
```

Request format: `{"id":"req-1","method":"workspace.list","params":{}}` (newline-terminated).
Thread-safe via mutex on socket writes. Auto-increments request IDs.

### 2. Sidebar Metadata (`internal/cmux/sidebar.go`)

```go
func (c *Client) SetStatus(key, value, icon, color string) error
func (c *Client) ClearStatus(key string) error
func (c *Client) SetProgress(fraction float64, label string) error
func (c *Client) ClearProgress() error
func (c *Client) Log(message, level, source string) error  // level: info|progress|success|warning|error
func (c *Client) ClearLog() error
func (c *Client) SidebarState() (*SidebarState, error)
```

### 3. Notifications (`internal/cmux/notification.go`)

```go
func (c *Client) Notify(title, subtitle, body string) error
func (c *Client) ListNotifications() ([]Notification, error)
func (c *Client) ClearNotifications() error
```

### 4. Workspace Management (`internal/cmux/workspace.go`)

```go
type Workspace struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

func (c *Client) ListWorkspaces() ([]Workspace, error)
func (c *Client) CreateWorkspace() (*Workspace, error)
func (c *Client) SelectWorkspace(id string) error
func (c *Client) CurrentWorkspace() (*Workspace, error)
func (c *Client) CloseWorkspace(id string) error
```

### 5. Surface Management (`internal/cmux/surface.go`)

```go
type Surface struct {
    ID   string `json:"id"`
    Type string `json:"type"`  // "terminal" or "browser"
}

func (c *Client) Split(direction string) (*Surface, error)  // left|right|up|down
func (c *Client) ListSurfaces() ([]Surface, error)
func (c *Client) FocusSurface(id string) error
func (c *Client) SendText(surfaceID, text string) error
func (c *Client) SendKey(surfaceID, key string) error  // enter|tab|escape|backspace|delete|up|down|left|right
```

### 6. Browser Automation (`internal/cmux/browser.go`)

```go
func (c *Client) BrowserNavigate(surfaceID, url string) error
func (c *Client) BrowserSnapshot(surfaceID string) (string, error)
func (c *Client) BrowserClick(surfaceID, ref string) error
func (c *Client) BrowserType(surfaceID, ref, text string) error
func (c *Client) BrowserWait(surfaceID, loadState string) error  // "complete"|"domcontentloaded"
```

### 7. Active Orchestrator (`internal/cmux/orchestrator.go`)

Coordinates sub-agents across split panes using log-based signaling.

```go
type Task struct {
    ID        string
    SurfaceID string
    Command   string
    Status    string     // "running", "done", "error"
    Logs      []LogEntry
}

type Orchestrator struct {
    client   *Client
    tasks    map[string]*Task
    pollRate time.Duration  // default 2s
}

func NewOrchestrator(client *Client) *Orchestrator
func (o *Orchestrator) Dispatch(direction, command string) (*Task, error)
func (o *Orchestrator) Wait(timeout time.Duration) ([]Task, error)
func (o *Orchestrator) WaitAny(timeout time.Duration) (*Task, error)
```

**Log convention** — sub-agents signal via cmux `log` entries:

| Log Entry | Meaning |
|-----------|---------|
| `[DONE] <summary>` | Task completed successfully |
| `[ERROR] <message>` | Task failed |
| `[PROGRESS] <0-100> <message>` | Interim progress update |

The orchestrator polls `sidebar-state` every 2 seconds, parses log entries by surface ID, and updates task statuses.

**Limitations:**
- Sub-agents must cooperate by writing structured log entries
- No stdout capture — cmux API doesn't expose terminal output
- Polling at 2s means up to 2s latency in detecting completion

### 8. Agent Tools

Registered conditionally — only when `CmuxSocket` is true.

**Browser tools** (`internal/tools/cmux_browser.go`):

| Tool | Parameters | Notes |
|------|-----------|-------|
| `cmux_browser_navigate` | `url` (required), `surface_id` (optional) | Auto-creates right split if no surface_id |
| `cmux_browser_snapshot` | `surface_id` | Returns DOM with element refs |
| `cmux_browser_click` | `surface_id`, `ref` | Click by element ref |
| `cmux_browser_type` | `surface_id`, `ref`, `text` | Type into element |
| `cmux_browser_wait` | `surface_id`, `load_state` | Wait for page load |

**Surface tools** (`internal/tools/cmux_surface.go`):

| Tool | Parameters | Notes |
|------|-----------|-------|
| `cmux_split` | `direction` | Returns new surface_id |
| `cmux_send` | `surface_id`, `text` or `key` | Send text/keypress to pane |

**Orchestration tool** (`internal/tools/cmux_orchestrate.go`):

| Tool | Parameters | Notes |
|------|-----------|-------|
| `cmux_orchestrate` | `tasks` (array of `{direction, command}`), `timeout` | Run commands in parallel, collect results |

Cmux tools are gated by the standard `ToolsConfig.ShouldEnable` mechanism (same as all other tools). No separate `cmux:control` permission is needed — tool-level approval through the existing approval flow is sufficient.

## Mode Integration

### Interactive Mode

```go
caps := terminal.Detect()
cmuxClient, closeCmux := dialCmux(caps) // returns (Caller, func())
defer closeCmux()
if cmuxClient != nil {
    registry.Register(tools.NewCmuxBrowserNavigate(cmuxClient))
    // ... register all cmux tools
}
model := tui.NewModel(...)
model.SetTermCaps(caps)
model.SetCmuxClient(cmuxClient)  // nil-safe
```

TUI dispatches via cmux when available:
- Approval requests: `Notify("Rubichan", "", "Needs approval to proceed")`
- Bootstrap progress: `SetProgress(fraction, phase)`
- Phase changes: `SetStatus("phase", "analyzing", "magnifyingglass", "#007aff")`
- Tool execution: `Log("Running file_write...", "progress", "tools")`

### Headless Mode

```go
if caps.CmuxSocket {
    cmuxClient, _ = cmux.Dial(cmux.SocketPath())
    cmuxClient.SetStatus("mode", "code-review", "shield", "#ff3b30")
    cmuxClient.SetProgress(0.3, "Static analysis...")
    // on completion:
    cmuxClient.Notify("Rubichan", "Code Review", "3 findings, 1 critical")
    cmuxClient.ClearProgress()
}
```

### Wiki Mode

```go
if caps.CmuxSocket {
    cmuxClient, _ = cmux.Dial(cmux.SocketPath())
    opts.ProgressFunc = func(phase string, pct int) {
        cmuxClient.SetProgress(float64(pct)/100.0, phase)
        cmuxClient.Log(phase, "progress", "wiki")
    }
    cmuxClient.Notify("Rubichan", "Wiki Generation", "12 pages generated")
}
```

## Testing Strategy

### Unit Tests — Mock Client

```go
// internal/cmux/cmuxtest/mock.go
type MockClient struct {
    Calls     []Call
    Responses map[string]any
}
```

All feature modules tested against the mock. No real socket needed.

### Socket Client Tests — Test Unix Socket

```go
func TestClientCall(t *testing.T) {
    sock := filepath.Join(t.TempDir(), "test.sock")
    ln, _ := net.Listen("unix", sock)
    go fakeServer(ln)
    c, _ := cmux.Dial(sock)
    defer c.Close()
    resp, err := c.Call("system.ping", nil)
    require.True(t, resp.OK)
}
```

### Integration Tests — Real cmux (opt-in)

Gated behind build tag or environment variable. Only runs inside cmux:

```go
//go:build cmux_integration

func TestRealCmuxPing(t *testing.T) {
    if os.Getenv("CMUX_SOCKET_PATH") == "" {
        t.Skip("not running inside cmux")
    }
    // ...
}
```

### Tool Tests

Each tool tested with mock client. Verify correct method names, parameters, error handling, and nil-client safety.

### Orchestrator Tests

Mock returns evolving `sidebar-state` responses across poll cycles:
1. First poll: no `[DONE]` logs
2. Second poll: task 1 logs `[DONE]`
3. Third poll: task 2 logs `[ERROR]`
4. Verify `Wait()` returns correct task statuses

## Sources

- [cmux API Documentation](https://cmux.com/zh-TW/docs/api)
- [cmux Review — Vibe Coding App](https://vibecoding.app/blog/cmux-review)
- [cmux Terminal Guide — Better Stack](https://betterstack.com/community/guides/ai/cmux-terminal/)
