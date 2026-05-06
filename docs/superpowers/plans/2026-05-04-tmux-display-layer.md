# Tmux Display Layer

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `swarm/display.go` to rubichan. A `TmuxDisplay` provides real-time observability for multi-agent teams — one tmux window per agent showing activity.

**Architecture:** A `TmuxController` interface abstracts tmux operations. `TmuxDisplay` manages a tmux session with one window per agent, formatting SDK messages with timestamps. Graceful degradation when tmux is unavailable.

**Tech Stack:** Go, existing `pkg/agentsdk` types, `internal/provider` message types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/display.go` | `DisplayMessage` type — SDK-facing display message interface |
| `internal/team/display.go` | `TmuxController` interface, `TmuxDisplay`, `agentDisplay`, message formatting |
| `internal/team/display_test.go` | Tests with mock TmuxController |
| `internal/team/coordinator.go` | Team coordinator — integrate display into SpawnTeammate/ShutdownAll |

---

## Chunk 1: SDK Display Types

### Task 1: Define DisplayMessage type

**Files:**
- Create: `pkg/agentsdk/display.go`

**Code:**

```go
package agentsdk

// DisplayMessage represents a message that can be displayed in an
// agent's tmux window. It carries the same content structure as
// provider messages but is decoupled from internal packages.
type DisplayMessage struct {
	Role    string
	Content []ContentBlock
}
```

**Test:**

```go
package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisplayMessage(t *testing.T) {
	msg := DisplayMessage{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "hello"},
		},
	}
	require.Equal(t, "assistant", msg.Role)
	require.Len(t, msg.Content, 1)
	require.Equal(t, "hello", msg.Content[0].Text)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestDisplayMessage -v
```

**Expected:** PASS.

---

## Chunk 2: TmuxController Interface and TmuxDisplay

### Task 2: Define TmuxController interface

**Files:**
- Create: `internal/team/display.go`

**Code:**

```go
package team

// TmuxController abstracts tmux operations for testing and mocking.
type TmuxController interface {
	// Available returns true if tmux is installed and accessible.
	Available() bool
	// SessionExists returns true if the named tmux session exists.
	SessionExists(name string) bool
	// KillSession terminates the named tmux session.
	KillSession(name string) error
	// CreateSession creates a new tmux session with the given name.
	CreateSession(name string) error
	// CreateWindow creates a new window in the given session and returns the pane ID.
	CreateWindow(sessionName, windowName string) (string, error)
	// SendText sends text to the given pane.
	SendText(paneID, text string) error
}
```

**Test:**

```go
package team

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mockTmuxController struct {
	available       bool
	sessions        map[string]bool
	windows         map[string]string
	sentTexts       []string
	createSessionFn func(string) error
}

func (m *mockTmuxController) Available() bool { return m.available }
func (m *mockTmuxController) SessionExists(name string) bool {
	return m.sessions[name]
}
func (m *mockTmuxController) KillSession(name string) error {
	delete(m.sessions, name)
	return nil
}
func (m *mockTmuxController) CreateSession(name string) error {
	if m.createSessionFn != nil {
		return m.createSessionFn(name)
	}
	m.sessions[name] = true
	return nil
}
func (m *mockTmuxController) CreateWindow(sessionName, windowName string) (string, error) {
	paneID := sessionName + ":" + windowName
	m.windows[windowName] = paneID
	return paneID, nil
}
func (m *mockTmuxController) SendText(paneID, text string) error {
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func TestMockTmuxController(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	require.True(t, m.Available())
	require.NoError(t, m.CreateSession("test"))
	require.True(t, m.SessionExists("test"))
	paneID, err := m.CreateWindow("test", "agent1")
	require.NoError(t, err)
	require.Equal(t, "test:agent1", paneID)
}
```

**Command:**
```bash
go test ./internal/team/... -run TestMockTmuxController -v
```

**Expected:** PASS.

---

### Task 3: Implement TmuxDisplay with Start/AddAgent/MarkDone/WriteEvent

**Files:**
- Modify: `internal/team/display.go`

**Code:**

```go
package team

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

const defaultSessionName = "rubichan-swarm"

type agentDisplay struct {
	name   string
	paneID string
	color  string
	done   bool
}

// TmuxDisplay manages the tmux session and display for the swarm.
type TmuxDisplay struct {
	mu          sync.Mutex
	tmux        TmuxController
	sessionName string
	agents      map[string]*agentDisplay
	started     bool
	disabled    bool
}

// NewTmuxDisplay creates a new TmuxDisplay with the default session name.
func NewTmuxDisplay(tmux TmuxController) *TmuxDisplay {
	return NewTmuxDisplayWithSession(tmux, defaultSessionName)
}

// NewTmuxDisplayWithSession creates a new TmuxDisplay with a custom session name.
func NewTmuxDisplayWithSession(tmux TmuxController, sessionName string) *TmuxDisplay {
	return &TmuxDisplay{
		tmux:        tmux,
		sessionName: sessionName,
		agents:      make(map[string]*agentDisplay),
	}
}

// Start initializes the tmux session for display.
func (d *TmuxDisplay) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.tmux.Available() {
		d.disabled = true
		return nil
	}

	if d.tmux.SessionExists(d.sessionName) {
		if err := d.tmux.KillSession(d.sessionName); err != nil {
			return err
		}
	}

	if err := d.tmux.CreateSession(d.sessionName); err != nil {
		return err
	}

	d.started = true
	return nil
}

// Stop stops the display without killing the session.
func (d *TmuxDisplay) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started = false
	return nil
}

// Cleanup kills the session and clears all agents.
func (d *TmuxDisplay) Cleanup() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.disabled {
		d.agents = make(map[string]*agentDisplay)
		d.started = false
		return nil
	}

	_ = d.tmux.KillSession(d.sessionName)
	d.agents = make(map[string]*agentDisplay)
	d.started = false
	return nil
}

// IsActive returns whether the display is active and enabled.
func (d *TmuxDisplay) IsActive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.started && !d.disabled
}

// sanitizeWindowName removes characters that tmux interprets as target syntax.
func sanitizeWindowName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-").Replace(name)
}

// AddAgent creates a new window for an agent and stores its display state.
func (d *TmuxDisplay) AddAgent(agentID, name, color string) (string, error) {
	name = sanitizeWindowName(name)
	d.mu.Lock()
	disabled := d.disabled
	sessionName := d.sessionName
	d.mu.Unlock()

	if disabled {
		return "", nil
	}

	paneID, err := d.tmux.CreateWindow(sessionName, name)
	if err != nil {
		return "", err
	}

	header := fmt.Sprintf("=== Agent: %s [%s] ===", name, agentID)
	if err := d.tmux.SendText(paneID, header); err != nil {
		return "", err
	}

	d.mu.Lock()
	d.agents[agentID] = &agentDisplay{
		name:   name,
		paneID: paneID,
		color:  color,
	}
	d.mu.Unlock()

	return paneID, nil
}

// MarkDone marks an agent as finished and sends a footer message.
func (d *TmuxDisplay) MarkDone(agentID string) error {
	d.mu.Lock()
	agent, exists := d.agents[agentID]
	if !exists || agent.done {
		d.mu.Unlock()
		return nil
	}
	agent.done = true
	paneID := agent.paneID
	name := agent.name
	d.mu.Unlock()

	footer := fmt.Sprintf("=== Agent %s finished ===", name)
	return d.tmux.SendText(paneID, footer)
}

// WriteEvent writes a formatted message to an agent's pane.
func (d *TmuxDisplay) WriteEvent(agentID string, msg agentsdk.DisplayMessage) error {
	d.mu.Lock()
	if !d.started || d.disabled {
		d.mu.Unlock()
		return nil
	}
	agent, exists := d.agents[agentID]
	if !exists || agent.done {
		d.mu.Unlock()
		return nil
	}
	paneID := agent.paneID
	d.mu.Unlock()

	text := formatMessage(msg)
	if text == "" {
		return nil
	}
	return d.tmux.SendText(paneID, text)
}

// formatMessage formats a DisplayMessage into a human-readable string with timestamps.
func formatMessage(msg agentsdk.DisplayMessage) string {
	if len(msg.Content) == 0 {
		return ""
	}

	timestamp := time.Now().Format("15:04:05")
	var lines []string

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			lines = append(lines, fmt.Sprintf("[%s] %s", timestamp, block.Text))
		case "tool_use":
			keys := formatInputKeys(block.Input)
			lines = append(lines, fmt.Sprintf("[%s] [tool_use] %s(%s)", timestamp, block.Name, keys))
		case "tool_result":
			tag := "[tool_result]"
			if block.IsError {
				tag = "[tool_result:error]"
			}
			content := truncate(string(block.Text), 200)
			lines = append(lines, fmt.Sprintf("[%s] %s %s", timestamp, tag, content))
		}
	}

	return strings.Join(lines, "\n")
}

// formatInputKeys extracts and formats comma-separated keys from a tool input.
func formatInputKeys(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

// truncate truncates a string to maxLen characters with "..." suffix if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

Add import at top of file:
```go
import "encoding/json"
```

**Test:**

```go
package team

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestTmuxDisplayStart(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	err := d.Start()
	require.NoError(t, err)
	require.True(t, d.IsActive())
	require.True(t, m.SessionExists("rubichan-swarm"))
}

func TestTmuxDisplayDisabledWhenTmuxUnavailable(t *testing.T) {
	m := &mockTmuxController{available: false}
	d := NewTmuxDisplay(m)
	err := d.Start()
	require.NoError(t, err)
	require.False(t, d.IsActive())
}

func TestTmuxDisplayAddAgent(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()

	paneID, err := d.AddAgent("agent-1", "explorer", "blue")
	require.NoError(t, err)
	require.Equal(t, "rubichan-swarm:explorer", paneID)
	require.Len(t, m.sentTexts, 1)
	require.Contains(t, m.sentTexts[0], "Agent: explorer")
}

func TestTmuxDisplayWriteEvent(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()
	_, _ = d.AddAgent("agent-1", "explorer", "blue")

	err := d.WriteEvent("agent-1", agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "hello world"},
		},
	})
	require.NoError(t, err)
	require.Len(t, m.sentTexts, 2) // header + message
	require.Contains(t, m.sentTexts[1], "hello world")
}

func TestTmuxDisplayMarkDone(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()
	_, _ = d.AddAgent("agent-1", "explorer", "blue")

	err := d.MarkDone("agent-1")
	require.NoError(t, err)
	require.Len(t, m.sentTexts, 2)
	require.Contains(t, m.sentTexts[1], "finished")
}

func TestSanitizeWindowName(t *testing.T) {
	require.Equal(t, "foo-bar", sanitizeWindowName("foo:bar"))
	require.Equal(t, "foo-bar", sanitizeWindowName("foo.bar"))
	require.Equal(t, "a-b-c", sanitizeWindowName("a:b.c"))
}

func TestFormatMessage(t *testing.T) {
	msg := agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "hello"},
			{Type: "tool_use", Name: "shell", Input: []byte(`{"command": "ls"}`)},
			{Type: "tool_result", Text: "result content", IsError: false},
		},
	}
	text := formatMessage(msg)
	require.Contains(t, text, "hello")
	require.Contains(t, text, "[tool_use] shell(command)")
	require.Contains(t, text, "[tool_result] result content")
}

func TestFormatMessageEmpty(t *testing.T) {
	msg := agentsdk.DisplayMessage{Role: "assistant"}
	require.Empty(t, formatMessage(msg))
}
```

**Command:**
```bash
go test ./internal/team/... -v
```

**Expected:** All tests PASS.

---

### Task 4: Implement Cleanup and graceful degradation

**Files:**
- Modify: `internal/team/display.go`

Verify `Cleanup()` is already implemented above. Add test for cleanup path:

**Test:**

```go
func TestTmuxDisplayCleanup(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()
	_, _ = d.AddAgent("agent-1", "explorer", "blue")

	err := d.Cleanup()
	require.NoError(t, err)
	require.False(t, d.IsActive())
	require.False(t, m.SessionExists("rubichan-swarm"))
}

func TestTmuxDisplayCleanupDisabled(t *testing.T) {
	m := &mockTmuxController{available: false}
	d := NewTmuxDisplay(m)
	_ = d.Start()

	err := d.Cleanup()
	require.NoError(t, err)
	require.False(t, d.IsActive())
}
```

**Command:**
```bash
go test ./internal/team/... -run TestTmuxDisplayCleanup -v
```

**Expected:** PASS.

---

## Chunk 3: Integration with Team Coordinator

### Task 5: Create coordinator.go with display integration

**Files:**
- Create: `internal/team/coordinator.go`

**Code:**

```go
package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Coordinator manages a team of agents with tmux display support.
type Coordinator struct {
	mu       sync.Mutex
	display  *TmuxDisplay
	agents   map[string]*AgentHandle
}

// AgentHandle tracks a teammate agent.
type AgentHandle struct {
	ID     string
	Name   string
	Status string
}

// NewCoordinator creates a new team coordinator.
func NewCoordinator(display *TmuxDisplay) *Coordinator {
	return &Coordinator{
		display: display,
		agents:  make(map[string]*AgentHandle),
	}
}

// Start initializes the coordinator's display.
func (c *Coordinator) Start() error {
	if c.display != nil {
		return c.display.Start()
	}
	return nil
}

// SpawnTeammate creates a new agent and adds it to the display.
func (c *Coordinator) SpawnTeammate(name string) (*AgentHandle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := uuid.New().String()
	handle := &AgentHandle{
		ID:     id,
		Name:   name,
		Status: "active",
	}
	c.agents[id] = handle

	if c.display != nil {
		_, err := c.display.AddAgent(id, name, "")
		if err != nil {
			return nil, fmt.Errorf("add agent to display: %w", err)
		}
	}

	return handle, nil
}

// ShutdownAll marks all agents as done and cleans up the display.
func (c *Coordinator) ShutdownAll() error {
	c.mu.Lock()
	agents := make(map[string]*AgentHandle, len(c.agents))
	for k, v := range c.agents {
		agents[k] = v
	}
	c.mu.Unlock()

	for id, agent := range agents {
		agent.Status = "done"
		if c.display != nil {
			_ = c.display.MarkDone(id)
		}
	}

	if c.display != nil {
		return c.display.Cleanup()
	}
	return nil
}

// WriteAgentEvent sends a display event to an agent's tmux window.
func (c *Coordinator) WriteAgentEvent(agentID string, msg agentsdk.DisplayMessage) error {
	if c.display == nil {
		return nil
	}
	return c.display.WriteEvent(agentID, msg)
}
```

**Test:**

```go
package team

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorSpawnTeammate(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	c := NewCoordinator(d)
	_ = c.Start()

	handle, err := c.SpawnTeammate("explorer")
	require.NoError(t, err)
	require.NotEmpty(t, handle.ID)
	require.Equal(t, "explorer", handle.Name)
	require.Len(t, m.windows, 1)
}

func TestCoordinatorShutdownAll(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	c := NewCoordinator(d)
	_ = c.Start()
	_, _ = c.SpawnTeammate("explorer")

	err := c.ShutdownAll()
	require.NoError(t, err)
	require.False(t, d.IsActive())
	require.Len(t, m.sentTexts, 2) // header + footer
}

func TestCoordinatorWriteAgentEvent(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	c := NewCoordinator(d)
	_ = c.Start()
	handle, _ := c.SpawnTeammate("explorer")

	err := c.WriteAgentEvent(handle.ID, agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "working..."},
		},
	})
	require.NoError(t, err)
	require.Len(t, m.sentTexts, 2) // header + message
}

func TestCoordinatorNoDisplay(t *testing.T) {
	c := NewCoordinator(nil)
	handle, err := c.SpawnTeammate("explorer")
	require.NoError(t, err)
	require.NotNil(t, handle)
	err = c.ShutdownAll()
	require.NoError(t, err)
}
```

**Command:**
```bash
go test ./internal/team/... -v
```

**Expected:** All tests PASS.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/team/...
go test -cover ./internal/team/...
golangci-lint run ./internal/team/...
gofmt -l .
```

---

## PR Description

**Title:** `[STRUCTURAL] Tmux display layer for multi-agent team observability`

**Body:**
- `TmuxController` interface abstracts tmux operations for testability
- `TmuxDisplay` manages a tmux session with one window per agent
- `formatMessage()` formats SDK messages with timestamps: `[15:04:05] text`, `[15:04:05] [tool_use] name(keys)`, `[15:04:05] [tool_result] content`
- Graceful degradation: if tmux unavailable, mark disabled and return nil errors
- `sanitizeWindowName()` replaces `:`, `.` with `-`
- `Coordinator` integrates display into `SpawnTeammate` and `ShutdownAll`
- Ports ccgo's `swarm/display.go` pattern to Go

**Commit prefix:** `[STRUCTURAL]`
