# Session Forking — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-17 · **Status:** Approved
> **Milestone:** 6, Phase 4
> **Parent:** [Spec Amendments](2026-03-16-spec-amendments-design.md), Amendment 3
> **FRs:** FR-7.9, FR-7.10, FR-7.11, FR-1.12, FR-1.13

---

## Overview

Add the ability to fork sessions — creating a new independent session by copying the conversation history from an existing one. The original session is preserved. Exposed via Cobra CLI subcommands (`rubichan session list/fork/delete`), TUI slash commands (`/sessions`, `/fork`), and a `--fork` flag combined with `--resume`.

## Existing Infrastructure

The codebase already provides:
- **`Store`** (`internal/store/store.go`) — SQLite persistence with `sessions`, `messages`, `session_snapshots`, `tool_result_blobs` tables
- **`Store.ListSessions(limit)`** — returns `[]Session` ordered by `updated_at DESC`
- **`Store.CreateSession/GetSession/DeleteSession`** — full CRUD
- **`Store.GetMessages/AppendMessage`** — message persistence with auto-sequencing
- **`Store.GetSnapshot/SaveSnapshot`** — compacted conversation state
- **`WithResumeSession(id)`** — agent option for loading an existing session
- **`--resume` flag** — CLI flag in `cmd/rubichan/main.go`

## Components

### 1. Schema Migration — `forked_from` column

Add a `forked_from` column to the `sessions` table to track fork relationships:

```sql
ALTER TABLE sessions ADD COLUMN forked_from TEXT DEFAULT ''
```

Applied in `createTables()` (`internal/store/store.go`) via an idempotent `ALTER TABLE ... ADD COLUMN` guarded by `PRAGMA table_info(sessions)` to check if the column already exists. The `Session` struct gains a `ForkedFrom string` field.

**Required query updates:** `CreateSession`, `GetSession`, and `ListSessions` must be updated to include `forked_from` in their INSERT/SELECT statements so the `ForkedFrom` field is persisted and populated correctly.

### 2. Store.ForkSession (`internal/store/store.go`)

```go
// ForkSession creates a new session by deep-copying all data from the source.
// Copies: session metadata (with new ID, ForkedFrom set), messages, snapshot, blobs.
func (s *Store) ForkSession(sourceID, newID string) error
```

**Algorithm:**
1. `GetSession(sourceID)` — read metadata. Error if not found.
2. Create new session with `newID`, same `Model`/`WorkingDir`/`SystemPrompt`, `ForkedFrom = sourceID`.
3. Copy messages via bulk INSERT with explicit `seq` values from source (not `AppendMessage` loop, which does O(n) `MAX(seq)` subqueries). Use: `INSERT INTO messages (session_id, seq, role, content) SELECT ?, seq, role, content FROM messages WHERE session_id = ?`.
4. `GetSnapshot(sourceID)` → if exists, `SaveSnapshot(newID, msgs, tokenCount)`.
5. Copy blobs: `SELECT id, tool_name, content, byte_size FROM tool_result_blobs WHERE session_id = ?` → insert each with `newID` as session_id, preserving the original blob `id` (so message references remain valid).

**Transaction:** The entire operation runs in a single SQLite transaction for atomicity.

### 3. Cobra Subcommands (`cmd/rubichan/session.go`)

**`rubichan session list`**

Lists recent sessions for the current working directory.

```
ID          Title                  Model           Updated          Turns
abc-123     Fix auth bug           claude-sonnet   2 hours ago      12
def-456     (forked from abc-123)  claude-sonnet   1 day ago        15
```

- Opens store read-only, calls `ListSessions(20)`
- Filters by current working directory
- Shows `ForkedFrom` in title when set
- `--all` flag to show all directories

**`rubichan session fork <session-id>`**

Forks a session and prints the new ID.

```
Forked session abc-123 → xyz-999
Resume with: rubichan --resume xyz-999
```

- Opens store, generates new UUID, calls `ForkSession(sourceID, newID)`

**`rubichan session delete <session-id>`**

Deletes a session (CASCADE removes messages, snapshots, blobs).

```
Deleted session abc-123
```

- Opens store, calls `DeleteSession(id)`
- No confirmation needed (sessions are recoverable from git history if needed)

### 4. Agent.ForkSession (`internal/agent/agent.go`)

```go
// ForkSession creates a fork of the current session and switches to it.
// Returns the new session ID.
func (a *Agent) ForkSession(ctx context.Context) (string, error)
```

- Generates new UUID
- Calls `a.store.ForkSession(a.sessionID, newID)`
- Updates `a.sessionID = newID`
- Subsequent messages persist to the fork

### 5. TUI Slash Commands (`internal/commands/session.go`)

**`/sessions`** — Lists recent sessions. Takes a `func() ([]store.Session, error)` callback.

**`/fork`** — Forks the current session. Takes a `func(ctx) (string, error)` callback that calls `agent.ForkSession()`. Output:

```
Forked current session → xyz-999
New conversation continues on the fork.
```

### 6. `--fork` CLI Flag (`cmd/rubichan/main.go`)

A boolean flag combined with `--resume`:

```bash
rubichan --resume abc-123 --fork    # fork abc-123, continue on fork
```

**Note:** `--continue` (resume most recent session) does not exist in the codebase yet. Only `--resume <id>` is implemented. Adding `--continue` is deferred — users can use `rubichan session list` to find the most recent session ID, then `--resume <id> --fork`.

When `--fork` is set:
1. Resolve the source session ID from `--resume`
2. Generate new UUID, call `Store.ForkSession(sourceID, newID)`
3. Pass `newID` to `WithResumeSession()` instead of `sourceID`

### 7. Command Registration

Register `/sessions` and `/fork` in TUI `NewModel()` alongside existing commands. The `/fork` callback uses `agent.ForkSession()`. The `/sessions` callback uses `store.ListSessions()` filtered by working directory.

Register Cobra `session` command in `main.go` via `rootCmd.AddCommand(sessionCmd())`.

## Scope Exclusions

- **No LLM-generated session summaries** — title is sufficient
- **No `session show` subcommand** — use `--resume` to inspect
- **No session search/filter** beyond working directory
- **No checkpoint copying across forks** — per Amendment 2 design, forks start with empty checkpoint stack

## File Summary

| File | Package | Change |
|------|---------|--------|
| `internal/store/store.go` | `store` | Add `ForkedFrom` field, `forked_from` migration, `ForkSession()` |
| `internal/store/store_test.go` | `store` | Fork tests: copy messages, snapshot, blobs, metadata |
| `cmd/rubichan/session.go` | `main` | Cobra `session list/fork/delete` subcommands |
| `internal/commands/session.go` | `commands` | `/sessions` and `/fork` slash commands |
| `internal/commands/session_test.go` | `commands` | Slash command tests |
| `internal/agent/agent.go` | `agent` | Add `ForkSession()` method |
| `internal/agent/agent_test.go` | `agent` | Fork method test |
| `cmd/rubichan/main.go` | `main` | `--fork` flag, command registration |

## Dependencies

- No new external dependencies.
- `internal/commands/session.go` imports `internal/store` for the `Session` type (used in callback return).
- All other dependencies already exist.
