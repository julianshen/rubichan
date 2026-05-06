# Coordinator + TeamRegistry

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `team/coordinator.go` and `team/team.go` to rubichan. A `Coordinator` manages team lifecycle: spawn teammates, send messages, shutdown. `TeamRegistry` tracks teammates by ID and name.

**Architecture:** `TeamConfig` holds team settings. `TeamRegistry` is a thread-safe dual-map (by ID and by name). `TeammateID` auto-generates IDs with color assignment. `Coordinator` orchestrates spawn, send, broadcast, and shutdown via a `Mailbox` and a `Spawner` interface.

**Tech Stack:** Go, standard library (`crypto/sha256`, `fmt`, `os`, `path/filepath`, `sync`, `sync/atomic`), existing `Mailbox` from `internal/team/mailbox.go`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/team/team.go` | `TeamConfig`, `TeamRegistry`, `TeammateID`, color assignment |
| `internal/team/coordinator.go` | `Coordinator`, `SpawnRequest`, `Spawner` interface, `Display` interface |
| `internal/team/coordinator_test.go` | Tests for spawn, send, broadcast, shutdown |
| `pkg/agentsdk/team.go` | `SpawnRequest`, `TeammateID` types (extend existing) |

---

## Chunk 1: SDK Types

### Task 1: Add SpawnRequest and TeammateID to agentsdk

**Files:**
- Modify: `pkg/agentsdk/team.go`

**Code:**

```go
package agentsdk

// SpawnRequest describes a teammate to spawn.
type SpawnRequest struct {
	AgentName string
	AgentType string
	Prompt    string
	Tools     []string
	Model     string
}

// TeammateID identifies a teammate in a team.
type TeammateID struct {
	AgentID   string
	AgentName string
	Color     string
}
```

**Test:**

```go
func TestSpawnRequestDefaults(t *testing.T) {
	req := SpawnRequest{AgentName: "explore", Prompt: "list files"}
	require.Equal(t, "explore", req.AgentName)
	require.Equal(t, "list files", req.Prompt)
	require.Empty(t, req.Tools)
}

func TestTeammateID(t *testing.T) {
	id := TeammateID{AgentID: "tm-1-explore", AgentName: "explore", Color: "\033[34m"}
	require.Equal(t, "tm-1-explore", id.AgentID)
	require.Equal(t, "explore", id.AgentName)
	require.Equal(t, "\033[34m", id.Color)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestSpawnRequest -v
go test ./pkg/agentsdk/... -run TestTeammateID -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add pkg/agentsdk/team.go pkg/agentsdk/team_test.go
git commit -m "[STRUCTURAL] Add SpawnRequest and TeammateID to agentsdk"
```

---

## Chunk 2: TeamRegistry and TeamConfig

### Task 2: Implement TeamConfig

**Files:**
- Create: `internal/team/team.go`

**Code:**

```go
package team

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TeamConfig holds team-wide settings.
type TeamConfig struct {
	TeamName     string
	WorkspaceDir string
	MaxTeammates int
}

// NewTeamConfig creates a TeamConfig with defaults.
func NewTeamConfig(teamName, workspaceDir string) TeamConfig {
	return TeamConfig{
		TeamName:     teamName,
		WorkspaceDir: workspaceDir,
		MaxTeammates: 10,
	}
}

// TeammatesDir returns the directory for teammate state.
func (c TeamConfig) TeammatesDir() string {
	return filepath.Join(c.WorkspaceDir, ".claude", "teams", c.TeamName)
}

// InboxesDir returns the directory for mailboxes.
func (c TeamConfig) InboxesDir() string {
	return filepath.Join(c.TeammatesDir(), "inboxes")
}

// EnsureDirs creates the team directories.
func (c TeamConfig) EnsureDirs() error {
	if err := os.MkdirAll(c.TeammatesDir(), 0o755); err != nil {
		return fmt.Errorf("create teammates dir: %w", err)
	}
	if err := os.MkdirAll(c.InboxesDir(), 0o755); err != nil {
		return fmt.Errorf("create inboxes dir: %w", err)
	}
	return nil
}

var teammateColorPalette = []string{
	"\033[34m", // blue
	"\033[32m", // green
	"\033[33m", // yellow
	"\033[35m", // magenta
	"\033[36m", // cyan
	"\033[31m", // red
}

// AssignColor deterministically assigns a color to a name.
func AssignColor(name string) string {
	h := sha256.Sum256([]byte(name))
	idx := 0
	for _, b := range h {
		idx = int(b) % len(teammateColorPalette)
	}
	return teammateColorPalette[idx]
}

var nextTeammateSeq uint64

// NewTeammateID creates a new TeammateID with auto-generated ID and color.
func NewTeammateID(agentName string) agentsdk.TeammateID {
	seq := atomic.AddUint64(&nextTeammateSeq, 1)
	agentID := fmt.Sprintf("tm-%d-%s", seq, agentName)
	return agentsdk.TeammateID{
		AgentID:   agentID,
		AgentName: agentName,
		Color:     AssignColor(agentName),
	}
}

// String returns a human-readable representation.
func (id agentsdk.TeammateID) String() string {
	return fmt.Sprintf("%s (%s)", id.AgentName, id.AgentID)
}
```

**Test:**

```go
func TestTeamConfigDefaults(t *testing.T) {
	cfg := NewTeamConfig("alpha", "/tmp/ws")
	require.Equal(t, "alpha", cfg.TeamName)
	require.Equal(t, "/tmp/ws", cfg.WorkspaceDir)
	require.Equal(t, 10, cfg.MaxTeammates)
}

func TestTeamConfigDirs(t *testing.T) {
	cfg := NewTeamConfig("alpha", "/tmp/ws")
	require.Contains(t, cfg.TeammatesDir(), ".claude/teams/alpha")
	require.Contains(t, cfg.InboxesDir(), "inboxes")
}

func TestTeamConfigEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	require.NoError(t, cfg.EnsureDirs())
	require.DirExists(t, cfg.TeammatesDir())
	require.DirExists(t, cfg.InboxesDir())
}

func TestAssignColorDeterministic(t *testing.T) {
	c1 := AssignColor("alice")
	c2 := AssignColor("alice")
	require.Equal(t, c1, c2)
	require.NotEmpty(t, c1)
}

func TestNewTeammateID(t *testing.T) {
	id1 := NewTeammateID("explore")
	id2 := NewTeammateID("explore")
	require.NotEqual(t, id1.AgentID, id2.AgentID)
	require.Equal(t, "explore", id1.AgentName)
	require.NotEmpty(t, id1.Color)
}
```

**Command:**
```bash
go test ./internal/team/... -run TestTeamConfig -v
go test ./internal/team/... -run TestAssignColor -v
go test ./internal/team/... -run TestNewTeammateID -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add internal/team/team.go internal/team/team_test.go
git commit -m "[STRUCTURAL] Add TeamConfig, TeammateID, and color assignment"
```

---

### Task 3: Implement TeamRegistry

**Files:**
- Modify: `internal/team/team.go`

**Code:**

```go
// TeamRegistry tracks teammates by ID and name, thread-safe.
type TeamRegistry struct {
	teamName string
	mu       sync.RWMutex
	byID     map[string]agentsdk.TeammateID
	byName   map[string]agentsdk.TeammateID
}

// NewTeamRegistry creates a new registry.
func NewTeamRegistry(teamName string) *TeamRegistry {
	return &TeamRegistry{
		teamName: teamName,
		byID:     make(map[string]agentsdk.TeammateID),
		byName:   make(map[string]agentsdk.TeammateID),
	}
}

// TeamName returns the team name.
func (r *TeamRegistry) TeamName() string { return r.teamName }

// Register adds a teammate to the registry.
func (r *TeamRegistry) Register(id agentsdk.TeammateID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[id.AgentID] = id
	r.byName[id.AgentName] = id
}

// Get looks up a teammate by ID.
func (r *TeamRegistry) Get(agentID string) (agentsdk.TeammateID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byID[agentID]
	return id, ok
}

// GetByName looks up a teammate by name.
func (r *TeamRegistry) GetByName(name string) (agentsdk.TeammateID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byName[name]
	return id, ok
}

// Remove deletes a teammate from the registry.
func (r *TeamRegistry) Remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byID[agentID]; ok {
		delete(r.byName, id.AgentName)
		delete(r.byID, agentID)
	}
}

// List returns all registered teammates.
func (r *TeamRegistry) List() []agentsdk.TeammateID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]agentsdk.TeammateID, 0, len(r.byID))
	for _, id := range r.byID {
		result = append(result, id)
	}
	return result
}

// IsTeammate checks if an agent ID is in the registry.
func (r *TeamRegistry) IsTeammate(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byID[agentID]
	return ok
}
```

**Test:**

```go
func TestTeamRegistryRegisterAndGet(t *testing.T) {
	r := NewTeamRegistry("alpha")
	id := NewTeammateID("explore")
	r.Register(id)

	got, ok := r.Get(id.AgentID)
	require.True(t, ok)
	require.Equal(t, id, got)

	got, ok = r.GetByName("explore")
	require.True(t, ok)
	require.Equal(t, id, got)
}

func TestTeamRegistryRemove(t *testing.T) {
	r := NewTeamRegistry("alpha")
	id := NewTeammateID("explore")
	r.Register(id)
	r.Remove(id.AgentID)

	_, ok := r.Get(id.AgentID)
	require.False(t, ok)
	_, ok = r.GetByName("explore")
	require.False(t, ok)
}

func TestTeamRegistryList(t *testing.T) {
	r := NewTeamRegistry("alpha")
	r.Register(NewTeammateID("a"))
	r.Register(NewTeammateID("b"))

	list := r.List()
	require.Len(t, list, 2)
}

func TestTeamRegistryIsTeammate(t *testing.T) {
	r := NewTeamRegistry("alpha")
	id := NewTeammateID("explore")
	r.Register(id)
	require.True(t, r.IsTeammate(id.AgentID))
	require.False(t, r.IsTeammate("tm-99-other"))
}
```

**Command:**
```bash
go test ./internal/team/... -run TestTeamRegistry -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add internal/team/team.go internal/team/team_test.go
git commit -m "[STRUCTURAL] Add TeamRegistry with thread-safe dual-map"
```

---

## Chunk 3: Coordinator

### Task 4: Implement Coordinator

**Files:**
- Create: `internal/team/coordinator.go`

**Code:**

```go
package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Spawner creates a new agent process for a teammate.
type Spawner interface {
	Spawn(ctx context.Context, req agentsdk.SpawnRequest) error
}

// Display is an optional UI integration for the coordinator.
type Display interface {
	AddAgent(agentID, name, color string) (string, error)
	MarkDone(agentID string) error
	Stop() error
	IsActive() bool
}

// CoordinatorOption configures a Coordinator.
type CoordinatorOption func(*Coordinator)

// WithDisplay sets the display integration.
func WithDisplay(d Display) CoordinatorOption {
	return func(c *Coordinator) { c.display = d }
}

// Coordinator manages team lifecycle: spawn, message, shutdown.
type Coordinator struct {
	cfg      TeamConfig
	registry *TeamRegistry
	mailbox  *Mailbox
	spawner  Spawner
	display  Display // nil if not configured
	mu       sync.RWMutex
}

// NewCoordinator creates a coordinator with the given config and spawner.
func NewCoordinator(cfg TeamConfig, spawner Spawner, opts ...CoordinatorOption) (*Coordinator, error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("ensure team dirs: %w", err)
	}
	c := &Coordinator{
		cfg:      cfg,
		registry: NewTeamRegistry(cfg.TeamName),
		mailbox:  NewMailbox(cfg.InboxesDir()),
		spawner:  spawner,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// SpawnTeammate creates a new teammate if under max and not duplicate.
func (c *Coordinator) SpawnTeammate(ctx context.Context, req agentsdk.SpawnRequest) (*agentsdk.TeammateID, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("spawn teammate %q: %w", req.AgentName, err)
	}

	c.mu.Lock()

	if _, exists := c.registry.GetByName(req.AgentName); exists {
		c.mu.Unlock()
		return nil, fmt.Errorf("teammate %q already exists", req.AgentName)
	}

	if len(c.registry.List()) >= c.cfg.MaxTeammates {
		c.mu.Unlock()
		return nil, fmt.Errorf("max teammates (%d) exceeded", c.cfg.MaxTeammates)
	}

	if err := c.mailbox.EnsureDir(); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("mailbox ensure: %w", err)
	}

	tid := NewTeammateID(req.AgentName)
	c.registry.Register(tid)

	if err := c.spawner.Spawn(ctx, req); err != nil {
		c.registry.Remove(tid.AgentID)
		c.mu.Unlock()
		return nil, fmt.Errorf("spawn teammate %q: %w", req.AgentName, err)
	}

	display := c.display
	c.mu.Unlock()

	// Display I/O outside lock to avoid blocking concurrent operations
	if display != nil {
		_, _ = display.AddAgent(req.AgentName, req.AgentName, "")
	}

	return &tid, nil
}

// SendMessage sends a direct message or broadcasts if to == "*".
func (c *Coordinator) SendMessage(from, to, text string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if to == "*" {
		return c.broadcast(from, text)
	}

	if from == to {
		return fmt.Errorf("cannot send message to self")
	}

	if _, ok := c.registry.GetByName(to); !ok {
		return fmt.Errorf("unknown teammate %q", to)
	}

	sender, ok := c.registry.GetByName(from)
	if !ok {
		return fmt.Errorf("unknown sender %q", from)
	}

	msg := agentsdk.MailboxMessage{
		From:  from,
		To:    to,
		Text:  text,
		Type:  agentsdk.MessageTypeText,
		Color: sender.Color,
	}
	return c.mailbox.Write(to, msg)
}

func (c *Coordinator) broadcast(from, text string) error {
	sender, ok := c.registry.GetByName(from)
	if !ok {
		return fmt.Errorf("unknown sender %q", from)
	}

	for _, tid := range c.registry.List() {
		if tid.AgentName == from {
			continue
		}
		msg := agentsdk.MailboxMessage{
			From:  from,
			To:    tid.AgentName,
			Text:  text,
			Type:  agentsdk.MessageTypeText,
			Color: sender.Color,
		}
		if err := c.mailbox.Write(tid.AgentName, msg); err != nil {
			return err
		}
	}
	return nil
}

// ShutdownTeammate sends a shutdown request to a teammate.
func (c *Coordinator) ShutdownTeammate(targetName, leaderName string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	target, ok := c.registry.GetByName(targetName)
	if !ok {
		return fmt.Errorf("unknown teammate %q", targetName)
	}

	msg := agentsdk.MailboxMessage{
		From: leaderName,
		To:   targetName,
		Type: agentsdk.MessageTypeShutdownRequest,
		Data: map[string]interface{}{"request_id": fmt.Sprintf("shutdown-%s", target.AgentID)},
	}
	return c.mailbox.Write(targetName, msg)
}

// ShutdownAll sends shutdown requests to all teammates except the leader.
func (c *Coordinator) ShutdownAll(leaderName string) error {
	c.mu.RLock()
	teammates := c.registry.List()
	c.mu.RUnlock()

	for _, tid := range teammates {
		if tid.AgentName == leaderName {
			continue
		}
		msg := agentsdk.MailboxMessage{
			From: leaderName,
			To:   tid.AgentName,
			Type: agentsdk.MessageTypeShutdownRequest,
			Data: map[string]interface{}{"request_id": fmt.Sprintf("shutdown-%s", tid.AgentID)},
		}
		if err := c.mailbox.Write(tid.AgentName, msg); err != nil {
			return err
		}
		if c.display != nil {
			_ = c.display.MarkDone(tid.AgentName)
		}
	}
	if c.display != nil {
		_ = c.display.Stop()
	}
	return nil
}

// ListTeammates returns all registered teammates.
func (c *Coordinator) ListTeammates() []agentsdk.TeammateID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.registry.List()
}

// GetTeammate looks up a teammate by agent ID.
func (c *Coordinator) GetTeammate(agentID string) (agentsdk.TeammateID, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.registry.Get(agentID)
}

// RemoveTeammate removes a teammate from the registry.
func (c *Coordinator) RemoveTeammate(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registry.Remove(agentID)
}

// Registry returns the underlying TeamRegistry.
func (c *Coordinator) Registry() *TeamRegistry {
	return c.registry
}

// Mailbox returns the underlying Mailbox.
func (c *Coordinator) Mailbox() *Mailbox {
	return c.mailbox
}

// DisplayActive returns whether the display is active.
func (c *Coordinator) DisplayActive() bool {
	if c.display == nil {
		return false
	}
	return c.display.IsActive()
}
```

**Test:**

```go
package team

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

type mockSpawner struct {
	spawned []agentsdk.SpawnRequest
	err     error
}

func (m *mockSpawner) Spawn(ctx context.Context, req agentsdk.SpawnRequest) error {
	if m.err != nil {
		return m.err
	}
	m.spawned = append(m.spawned, req)
	return nil
}

func TestCoordinatorSpawnTeammate(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, err := NewCoordinator(cfg, spawner)
	require.NoError(t, err)

	tid, err := coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "explore"})
	require.NoError(t, err)
	require.NotNil(t, tid)
	require.Equal(t, "explore", tid.AgentName)
	require.Len(t, spawner.spawned, 1)
}

func TestCoordinatorSpawnDuplicate(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, err := coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "explore"})
	require.NoError(t, err)

	_, err = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "explore"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestCoordinatorSpawnMax(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	cfg.MaxTeammates = 1
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, err := coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "a"})
	require.NoError(t, err)

	_, err = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "max teammates")
}

func TestCoordinatorSendMessage(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})

	err := coord.SendMessage("alice", "bob", "hello")
	require.NoError(t, err)

	msgs, err := coord.Mailbox().Read("bob")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "hello", msgs[0].Text)
}

func TestCoordinatorSendMessageSelf(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})

	err := coord.SendMessage("alice", "alice", "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "self")
}

func TestCoordinatorBroadcast(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "carol"})

	err := coord.SendMessage("alice", "*", "all hands")
	require.NoError(t, err)

	bobMsgs, _ := coord.Mailbox().Read("bob")
	carolMsgs, _ := coord.Mailbox().Read("carol")
	require.Len(t, bobMsgs, 1)
	require.Len(t, carolMsgs, 1)
	require.Equal(t, "all hands", bobMsgs[0].Text)
}

func TestCoordinatorShutdownTeammate(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})

	err := coord.ShutdownTeammate("bob", "alice")
	require.NoError(t, err)

	msgs, _ := coord.Mailbox().Read("bob")
	require.Len(t, msgs, 1)
	require.Equal(t, agentsdk.MessageTypeShutdownRequest, msgs[0].Type)
}

func TestCoordinatorShutdownAll(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})

	err := coord.ShutdownAll("alice")
	require.NoError(t, err)

	msgs, _ := coord.Mailbox().Read("bob")
	require.Len(t, msgs, 1)
	require.Equal(t, agentsdk.MessageTypeShutdownRequest, msgs[0].Type)
}
```

**Command:**
```bash
go test ./internal/team/... -run TestCoordinator -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add internal/team/coordinator.go internal/team/coordinator_test.go
git commit -m "[BEHAVIORAL] Implement Coordinator with spawn, send, broadcast, shutdown"
```

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

**Title:** `[STRUCTURAL] Coordinator + TeamRegistry for team lifecycle management`

**Body:**
- `TeamConfig` with team name, workspace dir, max teammates (default 10)
- `TeammateID` with auto-generated ID and deterministic color from palette
- `TeamRegistry` thread-safe dual-map by ID and name
- `Coordinator` orchestrates:
  - `SpawnTeammate` (checks max, dedup, spawns via `Spawner` interface)
  - `SendMessage` (direct + broadcast via `*`)
  - `ShutdownTeammate` / `ShutdownAll` (sends shutdown_request messages)
- Optional `Display` interface integration (nil if not configured)
- Ports ccgo's `team/coordinator.go` and `team/team.go` to rubichan

**Commit prefix:** `[STRUCTURAL]`
