# Session Forking Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add session forking — copy a session's conversation history into a new independent session, with CLI subcommands, TUI slash commands, and a `--fork` flag.

**Architecture:** Extend `Store` with `ForkSession()` and `forked_from` column. Add Cobra `session` subcommand with `list/fork/delete`. Add TUI `/sessions`/`/fork` slash commands. Add `Agent.ForkSession()`. Wire `--fork` flag in `main.go`.

**Tech Stack:** Go stdlib, SQLite (existing `internal/store`), Cobra (existing), `internal/commands` slash command pattern.

**Spec:** `docs/superpowers/specs/2026-03-17-session-forking-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/store/store.go` | `store` | Add `ForkedFrom` to Session, schema migration, `ForkSession()`, update queries |
| `internal/store/store_test.go` | `store` | Fork tests |
| `internal/agent/agent.go` | `agent` | Add `ForkSession()` method |
| `internal/agent/agent_test.go` | `agent` | Fork method test |
| `internal/commands/session.go` | `commands` | `/sessions` and `/fork` slash commands |
| `internal/commands/session_test.go` | `commands` | Slash command tests |
| `cmd/rubichan/session.go` | `main` | Cobra `session list/fork/delete` subcommands |
| `cmd/rubichan/main.go` | `main` | `--fork` flag, command registration |

---

## Chunk 1: Store Layer

### Task 1: Add ForkedFrom field and schema migration

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestSessionForkedFromField(t *testing.T) {
	s := setupTestStore(t)

	// Create a session with ForkedFrom
	err := s.CreateSession(store.Session{
		ID: "child-1", Model: "test", ForkedFrom: "parent-1",
	})
	require.NoError(t, err)

	sess, err := s.GetSession("child-1")
	require.NoError(t, err)
	assert.Equal(t, "parent-1", sess.ForkedFrom)
}

func TestListSessionsIncludesForkedFrom(t *testing.T) {
	s := setupTestStore(t)

	s.CreateSession(store.Session{ID: "s1", Model: "test"})
	s.CreateSession(store.Session{ID: "s2", Model: "test", ForkedFrom: "s1"})

	sessions, err := s.ListSessions(10)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	// Find the forked session
	var forked *store.Session
	for i := range sessions {
		if sessions[i].ID == "s2" {
			forked = &sessions[i]
		}
	}
	require.NotNil(t, forked)
	assert.Equal(t, "s1", forked.ForkedFrom)
}
```

Note: `setupTestStore(t)` is a helper that creates a temp DB — check existing test patterns in `store_test.go` for the actual helper name.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "TestSessionForkedFrom|TestListSessionsIncludes" -v`
Expected: FAIL — ForkedFrom field doesn't exist

- [ ] **Step 3: Write implementation**

Add to `Session` struct:
```go
ForkedFrom string
```

Add schema migration in `createTables()` after the existing statements loop:
```go
// Migration: add forked_from column if not present
var count int
_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name='forked_from'`).Scan(&count)
if count == 0 {
    if _, err := db.Exec(`ALTER TABLE sessions ADD COLUMN forked_from TEXT NOT NULL DEFAULT ''`); err != nil {
        return fmt.Errorf("migrate forked_from: %w", err)
    }
}
```

Update `CreateSession` INSERT to include `forked_from`:
```go
`INSERT INTO sessions (id, title, model, working_dir, system_prompt, token_count, forked_from, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
sess.ID, sess.Title, sess.Model, sess.WorkingDir, sess.SystemPrompt, sess.TokenCount, sess.ForkedFrom,
```

Update `GetSession` SELECT to include `forked_from`:
```go
`SELECT id, title, model, working_dir, system_prompt, created_at, updated_at, token_count, forked_from
 FROM sessions WHERE id = ?`
// Add &sess.ForkedFrom to Scan
```

Update `ListSessions` SELECT to include `forked_from`:
```go
`SELECT id, title, model, working_dir, system_prompt, created_at, updated_at, token_count, forked_from
 FROM sessions ORDER BY updated_at DESC LIMIT ?`
// Add &sess.ForkedFrom to Scan
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run "TestSessionForkedFrom|TestListSessionsIncludes" -v`
Expected: PASS

- [ ] **Step 5: Run all store tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS (existing tests should not break — ForkedFrom defaults to "")

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add ForkedFrom field and schema migration for session forking
```

---

### Task 2: Store.ForkSession

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestForkSession(t *testing.T) {
	s := setupTestStore(t)

	// Create source session with messages
	s.CreateSession(store.Session{ID: "source", Model: "claude", WorkingDir: "/proj", SystemPrompt: "prompt"})
	s.AppendMessage("source", "user", []provider.ContentBlock{{Type: "text", Text: "hello"}})
	s.AppendMessage("source", "assistant", []provider.ContentBlock{{Type: "text", Text: "world"}})

	// Fork
	err := s.ForkSession("source", "fork-1")
	require.NoError(t, err)

	// Verify fork metadata
	fork, err := s.GetSession("fork-1")
	require.NoError(t, err)
	assert.Equal(t, "claude", fork.Model)
	assert.Equal(t, "/proj", fork.WorkingDir)
	assert.Equal(t, "prompt", fork.SystemPrompt)
	assert.Equal(t, "source", fork.ForkedFrom)

	// Verify messages copied
	msgs, err := s.GetMessages("fork-1")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)

	// Verify source unchanged
	srcMsgs, _ := s.GetMessages("source")
	assert.Len(t, srcMsgs, 2)
}

func TestForkSessionCopiesSnapshot(t *testing.T) {
	s := setupTestStore(t)

	s.CreateSession(store.Session{ID: "src", Model: "test"})
	s.SaveSnapshot("src", []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "snap"}}}}, 100)

	err := s.ForkSession("src", "fork-snap")
	require.NoError(t, err)

	snap, err := s.GetSnapshot("fork-snap")
	require.NoError(t, err)
	require.NotNil(t, snap)
	require.Len(t, snap, 1)
	assert.Equal(t, "user", snap[0].Role)
}

func TestForkSessionNotFound(t *testing.T) {
	s := setupTestStore(t)
	err := s.ForkSession("nonexistent", "fork-x")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run "TestForkSession" -v`
Expected: FAIL — ForkSession not defined

- [ ] **Step 3: Write implementation**

```go
// ForkSession creates a new session by deep-copying all data from the source.
func (s *Store) ForkSession(sourceID, newID string) error {
	src, err := s.GetSession(sourceID)
	if err != nil {
		return fmt.Errorf("fork: get source: %w", err)
	}
	if src == nil {
		return fmt.Errorf("fork: source session %q not found", sourceID)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("fork: begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Create new session
	_, err = tx.Exec(
		`INSERT INTO sessions (id, title, model, working_dir, system_prompt, token_count, forked_from, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		newID, src.Title, src.Model, src.WorkingDir, src.SystemPrompt, src.TokenCount, sourceID,
	)
	if err != nil {
		return fmt.Errorf("fork: create session: %w", err)
	}

	// 2. Bulk copy messages
	_, err = tx.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 SELECT ?, seq, role, content, created_at FROM messages WHERE session_id = ? ORDER BY seq`,
		newID, sourceID,
	)
	if err != nil {
		return fmt.Errorf("fork: copy messages: %w", err)
	}

	// 3. Copy snapshot if exists
	_, err = tx.Exec(
		`INSERT OR IGNORE INTO session_snapshots (session_id, messages, token_count, created_at)
		 SELECT ?, messages, token_count, created_at FROM session_snapshots WHERE session_id = ?`,
		newID, sourceID,
	)
	if err != nil {
		return fmt.Errorf("fork: copy snapshot: %w", err)
	}

	// 4. Copy blobs (preserve original blob ID so message references stay valid)
	_, err = tx.Exec(
		`INSERT OR IGNORE INTO tool_result_blobs (id, session_id, tool_name, content, byte_size, created_at)
		 SELECT id, ?, tool_name, content, byte_size, created_at FROM tool_result_blobs WHERE session_id = ?`,
		newID, sourceID,
	)
	if err != nil {
		return fmt.Errorf("fork: copy blobs: %w", err)
	}

	return tx.Commit()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run "TestForkSession" -v`
Expected: PASS

- [ ] **Step 5: Run all store tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add Store.ForkSession with transactional deep copy
```

---

## Chunk 2: Agent + Commands

### Task 3: Agent.ForkSession method

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestAgentForkSession(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "ok"}, {Type: "stop"},
	}}

	t.Run("no store returns error", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)
		_, err := a.ForkSession(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "store not configured")
	})
}
```

Match existing agent test patterns. The store-backed test is harder to set up (needs real SQLite), so the no-store error case is the primary unit test. Integration testing happens via the store tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentForkSession -v`
Expected: FAIL — ForkSession not defined

- [ ] **Step 3: Write implementation**

Add to `internal/agent/agent.go`:

```go
// ForkSession creates a fork of the current session and switches to it.
// Returns the new session ID.
func (a *Agent) ForkSession(ctx context.Context) (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("fork session: store not configured")
	}
	newID := uuid.New().String()
	if err := a.store.ForkSession(a.sessionID, newID); err != nil {
		return "", fmt.Errorf("fork session: %w", err)
	}
	a.sessionID = newID
	return newID, nil
}

// SessionID returns the current session ID.
func (a *Agent) SessionID() string {
	return a.sessionID
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentForkSession -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add Agent.ForkSession and SessionID methods
```

---

### Task 4: /sessions and /fork slash commands

**Files:**
- Create: `internal/commands/session.go`
- Create: `internal/commands/session_test.go`

- [ ] **Step 1: Write failing tests**

```go
package commands_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsCommand(t *testing.T) {
	sessions := []store.Session{
		{ID: "abc-123", Title: "Fix auth", Model: "claude", UpdatedAt: time.Now()},
		{ID: "def-456", Title: "Refactor", Model: "opus", UpdatedAt: time.Now(), ForkedFrom: "abc-123"},
	}

	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return sessions, nil
	})
	assert.Equal(t, "sessions", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "abc-123")
	assert.Contains(t, result.Output, "Fix auth")
	assert.Contains(t, result.Output, "forked from abc-123")
}

func TestSessionsCommandEmpty(t *testing.T) {
	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return nil, nil
	})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No sessions")
}

func TestSessionsCommandNilCallback(t *testing.T) {
	cmd := commands.NewSessionsCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}

func TestForkCommand(t *testing.T) {
	cmd := commands.NewForkCommand(func(ctx context.Context) (string, error) {
		return "new-session-id", nil
	})
	assert.Equal(t, "fork", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "new-session-id")
	assert.Contains(t, result.Output, "Forked")
}

func TestForkCommandNilCallback(t *testing.T) {
	cmd := commands.NewForkCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}

func TestForkCommandError(t *testing.T) {
	cmd := commands.NewForkCommand(func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("no store")
	})
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/commands/ -run "TestSessionsCommand|TestForkCommand" -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Write implementation**

```go
package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/store"
)

// --- sessions ---

type sessionsCommand struct {
	listSessions func() ([]store.Session, error)
}

func NewSessionsCommand(listSessions func() ([]store.Session, error)) SlashCommand {
	return &sessionsCommand{listSessions: listSessions}
}

func (c *sessionsCommand) Name() string                                       { return "sessions" }
func (c *sessionsCommand) Description() string                                { return "List recent sessions" }
func (c *sessionsCommand) Arguments() []ArgumentDef                           { return nil }
func (c *sessionsCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *sessionsCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.listSessions == nil {
		return Result{Output: "Session listing not available."}, nil
	}

	sessions, err := c.listSessions()
	if err != nil {
		return Result{}, err
	}
	if len(sessions) == 0 {
		return Result{Output: "No sessions found."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Recent sessions:\n")
	for _, s := range sessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		if s.ForkedFrom != "" {
			title += fmt.Sprintf(" (forked from %s)", s.ForkedFrom[:min(len(s.ForkedFrom), 8)])
		}
		fmt.Fprintf(&sb, "  %s  %-30s  %s  %s\n",
			s.ID[:min(len(s.ID), 8)], title, s.Model, s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return Result{Output: sb.String()}, nil
}

// --- fork ---

type forkCommand struct {
	doFork func(ctx context.Context) (string, error)
}

func NewForkCommand(doFork func(ctx context.Context) (string, error)) SlashCommand {
	return &forkCommand{doFork: doFork}
}

func (c *forkCommand) Name() string                                       { return "fork" }
func (c *forkCommand) Description() string                                { return "Fork the current session" }
func (c *forkCommand) Arguments() []ArgumentDef                           { return nil }
func (c *forkCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *forkCommand) Execute(ctx context.Context, _ []string) (Result, error) {
	if c.doFork == nil {
		return Result{Output: "Session forking not available."}, nil
	}

	newID, err := c.doFork(ctx)
	if err != nil {
		return Result{}, err
	}

	return Result{Output: fmt.Sprintf("Forked current session → %s\nNew conversation continues on the fork.", newID)}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ -run "TestSessionsCommand|TestForkCommand" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add /sessions and /fork slash commands
```

---

## Chunk 3: Cobra Commands + CLI Wiring

### Task 5: Cobra session subcommands

**Files:**
- Create: `cmd/rubichan/session.go`

- [ ] **Step 1: Write implementation**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/store"
)

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage sessions",
	}
	cmd.AddCommand(sessionListCmd())
	cmd.AddCommand(sessionForkCmd())
	cmd.AddCommand(sessionDeleteCmd())
	return cmd
}

func sessionListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			sessions, err := s.ListSessions(20)
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()
			for _, sess := range sessions {
				if !all && sess.WorkingDir != cwd {
					continue
				}
				title := sess.Title
				if title == "" {
					title = "(untitled)"
				}
				if sess.ForkedFrom != "" {
					title += fmt.Sprintf(" (forked from %s)", truncID(sess.ForkedFrom))
				}
				ago := time.Since(sess.UpdatedAt).Truncate(time.Minute)
				fmt.Printf("%-10s  %-35s  %-15s  %s ago\n", truncID(sess.ID), title, sess.Model, ago)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show sessions from all directories")
	return cmd
}

func sessionForkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fork <session-id>",
		Short: "Fork a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			sourceID := args[0]
			newID := uuid.New().String()
			if err := s.ForkSession(sourceID, newID); err != nil {
				return err
			}
			fmt.Printf("Forked session %s → %s\nResume with: rubichan --resume %s\n", truncID(sourceID), truncID(newID), newID)
			return nil
		},
	}
}

func sessionDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.DeleteSession(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted session %s\n", args[0])
			return nil
		},
	}
}

func openStore() (*store.Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".config", "rubichan", "store.db")
	return store.Open(dbPath)
}

func truncID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
```

- [ ] **Step 2: Register in main.go**

In `cmd/rubichan/main.go`, at line ~457 (where other commands are added):

```go
rootCmd.AddCommand(sessionCmd())
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: BUILD OK

- [ ] **Step 4: Commit**

```
[BEHAVIORAL] add Cobra session list/fork/delete subcommands
```

---

### Task 6: --fork flag and TUI command registration

**Files:**
- Modify: `cmd/rubichan/main.go`

- [ ] **Step 1: Add --fork flag**

Add near the `--resume` flag declaration (line ~440):

```go
var forkFlag bool
// In flag registration:
rootCmd.PersistentFlags().BoolVar(&forkFlag, "fork", false, "fork the resumed session instead of continuing it")
```

- [ ] **Step 2: Wire --fork into interactive mode**

In the interactive setup (near lines 1225-1226 where `WithResumeSession` is used):

```go
if forkFlag && resumeFlag != "" {
    s, err := openStore()
    if err != nil {
        return fmt.Errorf("fork: %w", err)
    }
    defer s.Close()
    newID := uuid.New().String()
    if err := s.ForkSession(resumeFlag, newID); err != nil {
        return fmt.Errorf("fork: %w", err)
    }
    log.Printf("Forked session %s → %s", resumeFlag, newID)
    resumeFlag = newID // resume the fork instead of the original
}
if resumeFlag != "" {
    opts = append(opts, agent.WithResumeSession(resumeFlag))
}
```

- [ ] **Step 3: Register /sessions and /fork TUI commands**

In `NewModel()` default command registration (near line 197 in `internal/tui/model.go`):

```go
_ = cmdRegistry.Register(commands.NewSessionsCommand(func() ([]store.Session, error) {
    if m.agent == nil {
        return nil, nil
    }
    // Use store to list sessions — need store access
    // For now, return nil if no store
    return nil, fmt.Errorf("session listing requires store")
}))
_ = cmdRegistry.Register(commands.NewForkCommand(func(ctx context.Context) (string, error) {
    if m.agent != nil {
        return m.agent.ForkSession(ctx)
    }
    return "", fmt.Errorf("no agent")
}))
```

Note: The `/sessions` command needs store access. The TUI model has access to the agent but not directly to the store. For now, the `/sessions` command can be registered with a callback that uses the agent's store (if exposed) or deferred to the Cobra `session list` command. The `/fork` command works immediately via `agent.ForkSession()`.

- [ ] **Step 4: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: BUILD OK

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add --fork flag and register TUI session commands
```

---

### Task 7: Final integration — tests + lint + coverage

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Check formatting**

Run: `gofmt -l .`
Expected: No files

- [ ] **Step 3: Check store coverage**

Run: `go test -cover ./internal/store/`
Expected: Reasonable coverage

- [ ] **Step 4: Commit any fixes**

```
[STRUCTURAL] fix lint and formatting
```
