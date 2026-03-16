# File Checkpoints — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-16 · **Status:** Approved
> **Milestone:** 6, Phase 1
> **Parent:** [Spec Amendments](2026-03-16-spec-amendments-design.md), Amendment 2
> **FRs:** FR-7.5, FR-7.6, FR-7.7, FR-7.8, FR-1.11

---

## Overview

Before every file write or patch operation, the agent captures a **checkpoint** — a snapshot of the file's current contents. Checkpoints enable per-file undo and per-turn rewind, independent of git. The system uses a stack-based model with memory budget management and disk spillover for large files.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Agent Core                                                  │
│  ┌──────────┐  ┌───────────────────┐  ┌──────────────────┐  │
│  │Agent Loop │  │CheckpointManager  │  │  TUI Commands    │  │
│  │           │──│  (stack, budget,  │◀─│  /undo, /rewind  │  │
│  │           │  │   spill, recover) │  │                  │  │
│  └─────┬─────┘  └───────────────────┘  └──────────────────┘  │
│        │               ▲                                      │
├────────┼───────────────┼──────────────────────────────────────┤
│  Tool Execution Pipeline                                      │
│                        │                                      │
│  Hook → Classifier → RuleEngine → ShellSafety →              │
│    CheckpointMiddleware → PostHook → OutputMgr → Executor    │
│        │                                                      │
│        │ intercepts file write/patch                          │
│        └─ calls mgr.Capture() before execution               │
└───────────────────────────────────────────────────────────────┘
```

## Components

### 1. CheckpointManager (`internal/checkpoint/manager.go`)

The core checkpoint logic. Manages an in-memory stack of file snapshots with disk spillover.

```go
package checkpoint

type Manager struct {
    mu        sync.Mutex
    stack     []Checkpoint
    rootDir   string   // working directory for resolving relative paths
    memUsed   int64    // current in-memory byte usage
    memBudget int64    // max in-memory bytes (default 100MB)
    spillDir  string   // $TMPDIR/aiagent/checkpoints/<session-id>/
}

type Checkpoint struct {
    ID           string
    FilePath     string    // absolute path
    Turn         int
    Timestamp    time.Time
    Operation    string      // "write" or "patch" (matches FileTool operations)
    OriginalData []byte      // nil if file did not exist (creation checkpoint)
    FileMode     os.FileMode // original file permissions (0 if file did not exist)
    Size         int64
    spilled      bool        // true if evicted to disk
    spillPath    string      // path on disk if spilled
}
```

**Public API:**

```go
// New creates a Manager with the given root directory and session ID.
// spillDir is derived as $TMPDIR/aiagent/checkpoints/<sessionID>/.
// memBudget defaults to 100MB if <= 0.
func New(rootDir, sessionID string, memBudget int64) (*Manager, error)

// Capture snapshots a file before modification.
// For new files (os.ErrNotExist), records a creation checkpoint with nil OriginalData.
// Files >1MB are spilled directly to disk. If memBudget is exceeded, oldest
// in-memory checkpoints are evicted to disk.
// Returns the checkpoint ID.
func (m *Manager) Capture(ctx context.Context, filePath string, turn int, operation string) (string, error)

// Undo reverts the most recent checkpoint: restores file contents (or deletes
// if it was a creation checkpoint). Returns the affected file path.
func (m *Manager) Undo(ctx context.Context) (string, error)

// RewindToTurn reverts all checkpoints with turn > the given turn number,
// in reverse order (newest first). Returns all affected file paths.
func (m *Manager) RewindToTurn(ctx context.Context, turn int) ([]string, error)

// List returns a copy of all checkpoints in the stack (oldest first).
func (m *Manager) List() []Checkpoint

// Cleanup removes the spill directory and all checkpoint data.
func (m *Manager) Cleanup() error
```

**Capture algorithm:**

1. Resolve `filePath` to absolute path under `rootDir` using `filepath.EvalSymlinks` for symlink resolution, then verify the resolved path is under `rootDir` (path traversal check). This mirrors the resolution logic in `FileTool.resolvePath` — consider extracting a shared `pathutil.Resolve(rootDir, relPath)` to avoid divergence.
2. `os.Stat` the file. If it exists, record `FileMode` from `FileInfo.Mode()`. If `os.ErrNotExist`, record `OriginalData = nil`, `FileMode = 0`.
3. If file exists, read contents via `os.ReadFile`. Note: an existing empty file produces `[]byte{}` (non-nil, zero-length), which is distinct from `nil` (file did not exist). Implementations must use `OriginalData == nil`, not `len(OriginalData) == 0`, to detect creation checkpoints.
4. Generate checkpoint ID (UUID v4 or similar).
5. If `size > 1MB`: write contents to `spillDir/<id>.bak`, set `spilled = true`, don't count against `memUsed`.
6. If `memUsed + size > memBudget`: evict oldest in-memory checkpoints to disk until budget fits.
7. Push `Checkpoint` onto stack.
8. If a spill occurred in step 5 or 6, update `manifest.json` in `spillDir`. The manifest is only written when checkpoint data is on disk — purely in-memory checkpoints don't need disk persistence since crash recovery only restores from spill files.

**Undo algorithm:**

1. Pop most recent checkpoint from stack.
2. If `OriginalData == nil` (creation checkpoint): delete the file via `os.Remove`.
3. Otherwise: if checkpoint was spilled, read `OriginalData` from `spillPath`, then delete spill file. Write `OriginalData` back to `FilePath` via `os.WriteFile(path, data, checkpoint.FileMode)`, preserving the original file permissions.
5. Update `memUsed`.
6. Return affected file path.

**RewindToTurn algorithm:**

1. Find the stack index of the last checkpoint where `Turn <= turn`.
2. Pop all checkpoints after that index, in reverse order (newest first).
3. For each: apply undo logic individually (restore or delete). Each checkpoint restores to the state captured by *that specific checkpoint*, not to some "original" state. This means if a file was modified multiple times (e.g., written at turn 4, patched at turn 5), rewinding to turn 3 applies two sequential undos: first restoring the turn-4 state, then restoring the pre-turn-4 state. Intermediate checkpoints are never skipped.
4. Return all affected paths (deduplicated, preserving order of first appearance).

**Error handling:**

- `Capture` failure is **non-fatal** — logged, but does not block the file operation. The checkpoint is a safety net, not a gate.
- `Undo`/`RewindToTurn` failure is **fatal to the undo operation** — returns error to caller. The file system may be in a partially-reverted state; the error message includes which files were successfully reverted.
- `ErrNoCheckpoints` returned when stack is empty and undo is requested.

### 2. CheckpointMiddleware (`internal/toolexec/checkpoint.go`)

A pipeline middleware that intercepts file tool calls and captures checkpoints before execution.

```go
// CheckpointMiddleware returns a Middleware that captures file state before
// write/patch operations. If mgr is nil, the middleware passes through.
// turnCounter is called to get the current turn number.
func CheckpointMiddleware(mgr *checkpoint.Manager, turnCounter func() int) Middleware
```

**Behavior:**

1. If `tc.Name != "file"` → pass through to next handler.
2. Parse `{"operation": ..., "path": ...}` from `tc.Input`. On parse error → pass through (let the tool handle bad input).
3. If `operation` is `"read"` → pass through.
4. Call `mgr.Capture(ctx, path, turnCounter(), operation)`. Log on error, don't block.
5. Call `next(ctx, tc)` and return its result.

**Pipeline position:**

```
HookMiddleware → ClassifierMiddleware → RuleEngineMiddleware →
ShellSafetyMiddleware → CheckpointMiddleware → PostHookMiddleware →
OutputManagerMiddleware → RegistryExecutor
```

Rationale: checkpoint runs after hooks (which can cancel the call) and after safety checks (which can block dangerous operations), but before the actual execution. This avoids capturing checkpoints for calls that will be cancelled.

### 3. Agent Integration (`internal/agent/agent.go`)

**New option:**

```go
func WithCheckpointManager(mgr *checkpoint.Manager) Option
```

**New methods on Agent:**

```go
// Undo reverts the most recent file checkpoint.
func (a *Agent) Undo(ctx context.Context) (string, error)

// RewindToTurn reverts all file checkpoints after the given turn.
func (a *Agent) RewindToTurn(ctx context.Context, turn int) ([]string, error)

// Checkpoints returns the current checkpoint stack.
func (a *Agent) Checkpoints() []checkpoint.Checkpoint
```

**Event emission:**

After successful `Undo()` or `RewindToTurn()`, the agent emits session events using the existing `session.NewCheckpointRestoredEvent(id, reason)`. During `Capture()`, the middleware does NOT emit events (too noisy — captures happen on every file write).

**Turn counter:**

The agent exposes a `func() int` that returns the current turn number. This is passed to `CheckpointMiddleware` via closure during pipeline construction.

### 4. Crash Recovery (`internal/checkpoint/recovery.go`)

**Spill directory structure:**

```
$TMPDIR/aiagent/checkpoints/
  <session-id>/
    session.lock         # PID lock file (created by New(), removed by Cleanup())
    manifest.json        # checkpoint metadata (written only when spilling to disk)
    <checkpoint-id>.bak  # spilled file contents
    <checkpoint-id>.bak
```

**manifest.json format:**

```json
{
  "session_id": "abc-123",
  "root_dir": "/Users/dev/myproject",
  "checkpoints": [
    {
      "id": "cp-001",
      "file_path": "/Users/dev/myproject/main.go",
      "turn": 3,
      "operation": "write",
      "size": 2048,
      "spilled": true
    }
  ]
}
```

**Public API:**

```go
// DetectOrphaned scans $TMPDIR/aiagent/checkpoints/ for directories
// whose session.lock PID is no longer alive. Returns orphaned session IDs.
func DetectOrphaned(tmpDir string) ([]string, error)

// RecoverSession reads a manifest and restores all checkpointed files
// to their pre-edit state. Returns the list of restored file paths.
func RecoverSession(tmpDir, sessionID string) ([]string, error)

// CleanupOrphaned removes all orphaned checkpoint directories.
func CleanupOrphaned(tmpDir string) error
```

**Recovery flow:**

1. On session start, TUI calls `DetectOrphaned()`.
2. If orphaned sessions found, prompt user: "Found unrecovered checkpoints from a previous session. Recover? [y/n]"
3. On "yes": call `RecoverSession()` for each, display restored paths.
4. On "no" or after recovery: call `CleanupOrphaned()`.

Recovery is best-effort. Corrupt manifests are logged and cleaned up silently.

### 5. TUI Commands (`internal/tui/`)

**`/undo`** — Reverts the most recent file edit.

```
> /undo
Reverted main.go (write, turn 5)
```

**`/rewind N`** — Reverts all file edits after turn N.

```
> /rewind 3
Reverted 3 files:
  - main.go (turn 5, write)
  - handler.go (turn 4, patch)
  - handler.go (turn 4, patch)
```

Both are registered as slash commands in the existing command registry. They call `Agent.Undo()` / `Agent.RewindToTurn()` and display the result.

### 6. Configuration

Add to `~/.config/aiagent/config.toml`:

```toml
[agent]
checkpoint_memory_budget = 104857600  # 100MB in bytes, default
```

No other configuration needed. Checkpoints are always enabled — the cost is negligible for normal usage.

## Scope Exclusions

- **No SQLite persistence** — checkpoints are session-scoped, in-memory + spill dir only.
- **No headless mode support** — headless runs are non-interactive; undo is not meaningful.
- **No compression** — marginal gain at 100MB budget; adds complexity.
- **No checkpoint browsing UI** — `/undo` and `/rewind N` are sufficient.
- **No cross-session persistence** — crash recovery is the only disk use case.

## File Summary

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/checkpoint/manager.go` | `checkpoint` | Stack, capture, undo, rewind, memory budget, spill |
| `internal/checkpoint/manager_test.go` | `checkpoint` | Unit tests for all manager operations |
| `internal/checkpoint/recovery.go` | `checkpoint` | Orphan detection, session recovery, cleanup |
| `internal/checkpoint/recovery_test.go` | `checkpoint` | Recovery tests with corrupt/valid manifests |
| `internal/toolexec/checkpoint.go` | `toolexec` | Middleware intercepting file writes |
| `internal/toolexec/checkpoint_test.go` | `toolexec` | Middleware tests (pass-through, capture, error) |
| `internal/agent/agent.go` | `agent` | Add checkpoint option, Undo/Rewind methods |
| `internal/agent/agent_test.go` | `agent` | Integration tests for undo/rewind |
| `internal/tui/commands.go` | `tui` | `/undo`, `/rewind` slash commands |

## Dependencies

- No new external dependencies.
- `internal/checkpoint` depends only on `os`, `sync`, `encoding/json`, `time`, `path/filepath`.
- `internal/toolexec` already depends on `internal/tools` — adding `internal/checkpoint` is a new edge but follows the same direction.
- `internal/agent` already depends on both `internal/tools` and `internal/toolexec` — adding `internal/checkpoint` is clean.
