# File Checkpoints Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-file checkpoint system enabling undo/rewind of file edits within a session.

**Architecture:** CheckpointManager in `internal/checkpoint/` captures file state before writes. A toolexec middleware intercepts file tool calls. Agent exposes Undo/Rewind methods. TUI registers `/undo` and `/rewind` slash commands.

**Tech Stack:** Go stdlib (`os`, `sync`, `encoding/json`, `path/filepath`), existing toolexec pipeline pattern, existing commands registry.

**Spec:** `docs/superpowers/specs/2026-03-16-file-checkpoints-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/checkpoint/manager.go` | `checkpoint` | Manager struct, Checkpoint struct, New, Capture, Undo, RewindToTurn, List, Cleanup |
| `internal/checkpoint/manager_test.go` | `checkpoint_test` | All unit tests for manager |
| `internal/checkpoint/recovery.go` | `checkpoint` | DetectOrphaned, RecoverSession, CleanupOrphaned, manifest I/O, PID lock |
| `internal/checkpoint/recovery_test.go` | `checkpoint_test` | Recovery tests |
| `internal/toolexec/checkpoint.go` | `toolexec` | CheckpointMiddleware |
| `internal/toolexec/checkpoint_test.go` | `toolexec` | Middleware tests |
| `internal/commands/checkpoint.go` | `commands` | NewUndoCommand, NewRewindCommand |
| `internal/commands/checkpoint_test.go` | `commands` | Command tests |
| `internal/agent/agent.go` | `agent` | Add checkpointMgr field, WithCheckpointManager option, Undo/RewindToTurn/Checkpoints methods, pipeline wiring |
| `internal/agent/agent_test.go` | `agent` | Integration tests |

---

## Chunk 1: CheckpointManager Core

### Task 1: Checkpoint types and New constructor

**Files:**
- Create: `internal/checkpoint/manager.go`
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test for New()**

```go
package checkpoint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "test-session", 0)
	require.NoError(t, err)
	assert.NotNil(t, mgr)
	assert.Empty(t, mgr.List())
}

func TestNewManagerCreatesSpillDir(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "test-session-spill", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "test-session-spill")
	_, err = os.Stat(spillDir)
	assert.NoError(t, err, "spill directory should be created")
}

func TestNewManagerDefaultBudget(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "test-session-budget", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()
	// Budget defaults to 100MB when 0 is passed — tested indirectly via capture behavior
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkpoint/ -run TestNew -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write minimal implementation**

```go
package checkpoint

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultMemBudget = 100 * 1024 * 1024 // 100MB

// Checkpoint represents a snapshot of a file before modification.
type Checkpoint struct {
	ID           string
	FilePath     string      // absolute path
	Turn         int
	Timestamp    time.Time
	Operation    string      // "write" or "patch"
	OriginalData []byte      // nil if file did not exist (creation checkpoint)
	FileMode     os.FileMode // original file permissions (0 if file did not exist)
	Size         int64
	spilled      bool
	spillPath    string
}

// Manager manages a stack of file checkpoints with memory budget and disk spillover.
type Manager struct {
	mu        sync.Mutex
	stack     []Checkpoint
	rootDir   string
	memUsed   int64
	memBudget int64
	spillDir  string
}

// New creates a Manager with the given root directory and session ID.
// spillDir is derived as $TMPDIR/aiagent/checkpoints/<sessionID>/.
// memBudget defaults to 100MB if <= 0.
func New(rootDir, sessionID string, memBudget int64) (*Manager, error) {
	if memBudget <= 0 {
		memBudget = defaultMemBudget
	}

	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", sessionID)
	if err := os.MkdirAll(spillDir, 0755); err != nil {
		return nil, err
	}

	return &Manager{
		rootDir:   rootDir,
		memBudget: memBudget,
		spillDir:  spillDir,
	}, nil
}

// List returns a copy of all checkpoints in the stack (oldest first).
func (m *Manager) List() []Checkpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Checkpoint, len(m.stack))
	copy(cp, m.stack)
	return cp
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkpoint/ -run TestNew -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add checkpoint Manager type and constructor
```

---

### Task 2: Capture — existing file

**Files:**
- Modify: `internal/checkpoint/manager.go`
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestCaptureExistingFile(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "hello.go")
	os.WriteFile(testFile, []byte("package main"), 0644)

	mgr, err := checkpoint.New(rootDir, "cap-existing", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	id, err := mgr.Capture(context.Background(), "hello.go", 1, "write")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.Equal(t, testFile, cps[0].FilePath)
	assert.Equal(t, 1, cps[0].Turn)
	assert.Equal(t, "write", cps[0].Operation)
	assert.Equal(t, []byte("package main"), cps[0].OriginalData)
	assert.Equal(t, os.FileMode(0644), cps[0].FileMode)
	assert.Equal(t, int64(12), cps[0].Size)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkpoint/ -run TestCaptureExistingFile -v`
Expected: FAIL — Capture not defined

- [ ] **Step 3: Write minimal implementation**

```go
import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// Capture snapshots a file before modification.
func (m *Manager) Capture(ctx context.Context, filePath string, turn int, operation string) (string, error) {
	absPath, err := m.resolvePath(filePath)
	if err != nil {
		return "", fmt.Errorf("checkpoint resolve path: %w", err)
	}

	var data []byte
	var mode os.FileMode
	info, statErr := os.Stat(absPath)
	if statErr == nil {
		mode = info.Mode()
		data, err = os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("checkpoint read file: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("checkpoint stat file: %w", statErr)
	}
	// os.IsNotExist: data stays nil, mode stays 0

	id := uuid.New().String()
	size := int64(len(data))

	cp := Checkpoint{
		ID:           id,
		FilePath:     absPath,
		Turn:         turn,
		Timestamp:    time.Now(),
		Operation:    operation,
		OriginalData: data,
		FileMode:     mode,
		Size:         size,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.stack = append(m.stack, cp)
	m.memUsed += size

	return id, nil
}

// resolvePath resolves a relative path to absolute under rootDir with symlink
// resolution and path traversal check.
func (m *Manager) resolvePath(relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return relPath, nil // already absolute
	}

	joined := filepath.Join(m.rootDir, relPath)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("abs: %w", err)
	}

	// Resolve symlinks for existing paths
	evalPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet — check ancestor
			dir := filepath.Dir(abs)
			evalDir, dirErr := filepath.EvalSymlinks(dir)
			if dirErr != nil {
				return abs, nil // best effort
			}
			if !strings.HasPrefix(evalDir, m.rootDir+string(filepath.Separator)) && evalDir != m.rootDir {
				return "", fmt.Errorf("path traversal denied: %s escapes root", relPath)
			}
			return abs, nil
		}
		return "", err
	}

	if !strings.HasPrefix(evalPath, m.rootDir+string(filepath.Separator)) && evalPath != m.rootDir {
		return "", fmt.Errorf("path traversal denied: %s escapes root", relPath)
	}

	return evalPath, nil
}

// Cleanup removes the spill directory and all checkpoint data.
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stack = nil
	m.memUsed = 0
	return os.RemoveAll(m.spillDir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkpoint/ -run TestCaptureExistingFile -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add checkpoint Capture for existing files
```

---

### Task 3: Capture — new file (creation checkpoint)

**Files:**
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestCaptureNewFile(t *testing.T) {
	rootDir := t.TempDir()

	mgr, err := checkpoint.New(rootDir, "cap-new", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	id, err := mgr.Capture(context.Background(), "new_file.go", 1, "write")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.Nil(t, cps[0].OriginalData, "creation checkpoint should have nil OriginalData")
	assert.Equal(t, os.FileMode(0), cps[0].FileMode)
	assert.Equal(t, int64(0), cps[0].Size)
}

func TestCaptureEmptyExistingFile(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "empty.go")
	os.WriteFile(testFile, []byte{}, 0644)

	mgr, err := checkpoint.New(rootDir, "cap-empty", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	_, err = mgr.Capture(context.Background(), "empty.go", 1, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.NotNil(t, cps[0].OriginalData, "empty existing file should have non-nil []byte{}")
	assert.Len(t, cps[0].OriginalData, 0)
}
```

- [ ] **Step 2: Run tests to verify they pass** (existing Capture handles both cases)

Run: `go test ./internal/checkpoint/ -run "TestCaptureNewFile|TestCaptureEmptyExistingFile" -v`
Expected: PASS (already handled by nil/non-nil distinction)

- [ ] **Step 3: Commit**

```
[BEHAVIORAL] add creation and empty file checkpoint tests
```

---

### Task 4: Undo — restore modified file

**Files:**
- Modify: `internal/checkpoint/manager.go`
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestUndoRestoresModifiedFile(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "main.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, err := checkpoint.New(rootDir, "undo-modify", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	_, err = mgr.Capture(context.Background(), "main.go", 1, "write")
	require.NoError(t, err)

	// Simulate the agent writing to the file
	os.WriteFile(testFile, []byte("modified"), 0644)

	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, testFile, path)

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
	assert.Empty(t, mgr.List(), "stack should be empty after undo")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkpoint/ -run TestUndoRestoresModifiedFile -v`
Expected: FAIL — Undo not defined

- [ ] **Step 3: Write minimal implementation**

```go
import "errors"

// ErrNoCheckpoints is returned when undo is requested on an empty stack.
var ErrNoCheckpoints = errors.New("no checkpoints to undo")

// Undo reverts the most recent checkpoint.
func (m *Manager) Undo(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.stack) == 0 {
		return "", ErrNoCheckpoints
	}

	cp := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]

	if err := m.restore(cp); err != nil {
		return cp.FilePath, fmt.Errorf("undo restore: %w", err)
	}

	if !cp.spilled {
		m.memUsed -= cp.Size
	}

	return cp.FilePath, nil
}

// restore applies a checkpoint: deletes file for creation checkpoints,
// writes original data for modification checkpoints.
func (m *Manager) restore(cp Checkpoint) error {
	if cp.OriginalData == nil {
		// Creation checkpoint — delete the file
		return os.Remove(cp.FilePath)
	}

	data := cp.OriginalData
	if cp.spilled {
		var err error
		data, err = os.ReadFile(cp.spillPath)
		if err != nil {
			return fmt.Errorf("read spill file: %w", err)
		}
		os.Remove(cp.spillPath) // cleanup spill file
	}

	mode := cp.FileMode
	if mode == 0 {
		mode = 0644
	}
	return os.WriteFile(cp.FilePath, data, mode)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkpoint/ -run TestUndoRestoresModifiedFile -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add checkpoint Undo to restore modified files
```

---

### Task 5: Undo — delete created file

**Files:**
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestUndoDeletesCreatedFile(t *testing.T) {
	rootDir := t.TempDir()

	mgr, err := checkpoint.New(rootDir, "undo-create", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	// Capture before the file exists
	_, err = mgr.Capture(context.Background(), "new.go", 1, "write")
	require.NoError(t, err)

	// Simulate the agent creating the file
	newFile := filepath.Join(rootDir, "new.go")
	os.WriteFile(newFile, []byte("package new"), 0644)

	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(rootDir, "new.go"), path)

	_, err = os.Stat(newFile)
	assert.True(t, os.IsNotExist(err), "file should be deleted after undo of creation")
}

func TestUndoEmptyStackReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "undo-empty", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	_, err = mgr.Undo(context.Background())
	assert.ErrorIs(t, err, checkpoint.ErrNoCheckpoints)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/checkpoint/ -run "TestUndoDeletesCreatedFile|TestUndoEmptyStack" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```
[BEHAVIORAL] add undo tests for creation checkpoints and empty stack
```

---

### Task 6: RewindToTurn

**Files:**
- Modify: `internal/checkpoint/manager.go`
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestRewindToTurn(t *testing.T) {
	rootDir := t.TempDir()
	file1 := filepath.Join(rootDir, "a.go")
	file2 := filepath.Join(rootDir, "b.go")
	os.WriteFile(file1, []byte("a-original"), 0644)
	os.WriteFile(file2, []byte("b-original"), 0644)

	mgr, err := checkpoint.New(rootDir, "rewind", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	// Turn 1: modify a.go
	mgr.Capture(context.Background(), "a.go", 1, "write")
	os.WriteFile(file1, []byte("a-turn1"), 0644)

	// Turn 2: modify b.go
	mgr.Capture(context.Background(), "b.go", 2, "write")
	os.WriteFile(file2, []byte("b-turn2"), 0644)

	// Turn 3: modify a.go again
	mgr.Capture(context.Background(), "a.go", 3, "patch")
	os.WriteFile(file1, []byte("a-turn3"), 0644)

	// Rewind to turn 1 — should undo turns 2 and 3
	paths, err := mgr.RewindToTurn(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, paths, 2) // a.go and b.go

	dataA, _ := os.ReadFile(file1)
	assert.Equal(t, "a-turn1", string(dataA), "a.go should be at turn-1 state")

	dataB, _ := os.ReadFile(file2)
	assert.Equal(t, "b-original", string(dataB), "b.go should be at original state")

	assert.Len(t, mgr.List(), 1, "only turn-1 checkpoint should remain")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkpoint/ -run TestRewindToTurn -v`
Expected: FAIL — RewindToTurn not defined

- [ ] **Step 3: Write minimal implementation**

```go
// RewindToTurn reverts all checkpoints with turn > the given turn number.
func (m *Manager) RewindToTurn(ctx context.Context, turn int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find cutoff index: last checkpoint where Turn <= turn
	cutoff := -1
	for i, cp := range m.stack {
		if cp.Turn <= turn {
			cutoff = i
		}
	}

	// Pop everything after cutoff in reverse
	var paths []string
	seen := make(map[string]bool)

	for i := len(m.stack) - 1; i > cutoff; i-- {
		cp := m.stack[i]
		if err := m.restore(cp); err != nil {
			return paths, fmt.Errorf("rewind restore %s: %w", cp.FilePath, err)
		}
		if !seen[cp.FilePath] {
			paths = append(paths, cp.FilePath)
			seen[cp.FilePath] = true
		}
		if !cp.spilled {
			m.memUsed -= cp.Size
		}
	}

	m.stack = m.stack[:cutoff+1]
	return paths, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkpoint/ -run TestRewindToTurn -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add RewindToTurn for multi-checkpoint rollback
```

---

### Task 7: Memory budget and disk spillover

**Files:**
- Modify: `internal/checkpoint/manager.go`
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestCaptureSpillsLargeFile(t *testing.T) {
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	// Create a 2MB file
	data := make([]byte, 2*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(bigFile, data, 0644)

	mgr, err := checkpoint.New(rootDir, "spill-large", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.True(t, cps[0].IsSpilled(), "file >1MB should be spilled to disk")
}

func TestCaptureBudgetEviction(t *testing.T) {
	rootDir := t.TempDir()

	// Create two 600KB files, budget is 1MB
	data := make([]byte, 600*1024)
	os.WriteFile(filepath.Join(rootDir, "a.bin"), data, 0644)
	os.WriteFile(filepath.Join(rootDir, "b.bin"), data, 0644)

	mgr, err := checkpoint.New(rootDir, "spill-budget", 1024*1024) // 1MB budget
	require.NoError(t, err)
	defer mgr.Cleanup()

	_, err = mgr.Capture(context.Background(), "a.bin", 1, "write")
	require.NoError(t, err)

	_, err = mgr.Capture(context.Background(), "b.bin", 2, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 2)
	// First checkpoint should have been evicted to disk
	assert.True(t, cps[0].IsSpilled(), "oldest checkpoint should be evicted when budget exceeded")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/checkpoint/ -run "TestCaptureSpills|TestCaptureBudget" -v`
Expected: FAIL — IsSpilled not defined, spill logic not implemented

- [ ] **Step 3: Write implementation**

Add `IsSpilled()` method to Checkpoint and spill/evict logic to Capture:

```go
// IsSpilled returns true if this checkpoint's data is stored on disk.
func (c Checkpoint) IsSpilled() bool { return c.spilled }

const spillThreshold = 1024 * 1024 // 1MB
```

Update `Capture` to add spill logic after creating the checkpoint:

```go
// In Capture, before pushing to stack:
if size > spillThreshold {
    spillPath := filepath.Join(m.spillDir, id+".bak")
    if err := os.WriteFile(spillPath, data, 0644); err != nil {
        return "", fmt.Errorf("checkpoint spill: %w", err)
    }
    cp.spilled = true
    cp.spillPath = spillPath
    cp.OriginalData = nil // don't hold in memory
} else {
    // Check budget and evict if needed
    for m.memUsed+size > m.memBudget && len(m.stack) > 0 {
        m.evictOldest()
    }
    m.memUsed += size
}
```

Add eviction helper:

```go
func (m *Manager) evictOldest() {
    for i, cp := range m.stack {
        if !cp.spilled && cp.OriginalData != nil {
            spillPath := filepath.Join(m.spillDir, cp.ID+".bak")
            if err := os.WriteFile(spillPath, cp.OriginalData, 0644); err != nil {
                continue // skip on error
            }
            m.stack[i].spilled = true
            m.stack[i].spillPath = spillPath
            m.stack[i].OriginalData = nil
            m.memUsed -= cp.Size
            return
        }
    }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/checkpoint/ -run "TestCaptureSpills|TestCaptureBudget" -v`
Expected: PASS

- [ ] **Step 5: Run all checkpoint tests**

Run: `go test ./internal/checkpoint/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add memory budget and disk spillover for checkpoints
```

---

### Task 8: Undo spilled checkpoint

**Files:**
- Test: `internal/checkpoint/manager_test.go`

- [ ] **Step 1: Write test**

```go
func TestUndoSpilledCheckpoint(t *testing.T) {
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	data := make([]byte, 2*1024*1024) // 2MB — will be spilled
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(bigFile, data, 0644)

	mgr, err := checkpoint.New(rootDir, "undo-spill", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.NoError(t, err)

	// Overwrite the file
	os.WriteFile(bigFile, []byte("small"), 0644)

	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, bigFile, path)

	restored, _ := os.ReadFile(bigFile)
	assert.Equal(t, data, restored, "spilled checkpoint should restore correctly")
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/checkpoint/ -run TestUndoSpilledCheckpoint -v`
Expected: PASS (restore already handles spilled case)

- [ ] **Step 3: Commit**

```
[BEHAVIORAL] add test for undo of spilled checkpoint
```

---

## Chunk 2: Crash Recovery

### Task 9: Manifest I/O and PID lock

**Files:**
- Create: `internal/checkpoint/recovery.go`
- Test: `internal/checkpoint/recovery_test.go`

- [ ] **Step 1: Write failing test**

```go
package checkpoint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectOrphanedFindsDeadSession(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "dead-session")
	os.MkdirAll(sessionDir, 0755)

	// Write a lock file with PID 999999999 (not running)
	os.WriteFile(filepath.Join(sessionDir, "session.lock"), []byte("999999999"), 0644)

	// Write a manifest
	os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte(`{"session_id":"dead-session","root_dir":"/tmp","checkpoints":[]}`), 0644)

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.Contains(t, orphans, "dead-session")
}

func TestDetectOrphanedSkipsLiveSession(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "live-session")
	os.MkdirAll(sessionDir, 0755)

	// Write lock file with our own PID (still alive)
	pid := os.Getpid()
	os.WriteFile(filepath.Join(sessionDir, "session.lock"), []byte(fmt.Sprintf("%d", pid)), 0644)

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.NotContains(t, orphans, "live-session")
}

func TestCleanupOrphaned(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "orphan")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "session.lock"), []byte("999999999"), 0644)

	err := checkpoint.CleanupOrphaned(tmpDir)
	require.NoError(t, err)

	_, err = os.Stat(sessionDir)
	assert.True(t, os.IsNotExist(err))
}

func TestRecoverSession(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "recover-sess")
	os.MkdirAll(sessionDir, 0755)

	// Create a target file that was "modified"
	targetFile := filepath.Join(targetDir, "main.go")
	os.WriteFile(targetFile, []byte("modified"), 0644)

	// Write spill file with original content
	os.WriteFile(filepath.Join(sessionDir, "cp-001.bak"), []byte("original"), 0644)

	// Write manifest referencing the spill
	manifest := fmt.Sprintf(`{"session_id":"recover-sess","root_dir":"%s","checkpoints":[{"id":"cp-001","file_path":"%s","turn":1,"operation":"write","size":8,"spilled":true}]}`, targetDir, targetFile)
	os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte(manifest), 0644)

	restored, err := checkpoint.RecoverSession(tmpDir, "recover-sess")
	require.NoError(t, err)
	assert.Len(t, restored, 1)

	data, _ := os.ReadFile(targetFile)
	assert.Equal(t, "original", string(data))
}

func TestRecoverSessionCorruptManifest(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "corrupt-sess")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte("not json"), 0644)

	_, err := checkpoint.RecoverSession(tmpDir, "corrupt-sess")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/checkpoint/ -run "TestDetectOrphaned|TestCleanupOrphaned" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write implementation**

```go
package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

type manifest struct {
	SessionID   string           `json:"session_id"`
	RootDir     string           `json:"root_dir"`
	Checkpoints []manifestEntry  `json:"checkpoints"`
}

type manifestEntry struct {
	ID        string `json:"id"`
	FilePath  string `json:"file_path"`
	Turn      int    `json:"turn"`
	Operation string `json:"operation"`
	Size      int64  `json:"size"`
	Spilled   bool   `json:"spilled"`
}

// WriteLock creates a PID lock file in the spill directory.
func (m *Manager) WriteLock() error {
	lockPath := filepath.Join(m.spillDir, "session.lock")
	return os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// WriteManifest writes the current checkpoint metadata to manifest.json.
func (m *Manager) WriteManifest() error {
	m.mu.Lock()
	entries := make([]manifestEntry, 0, len(m.stack))
	for _, cp := range m.stack {
		if cp.spilled {
			entries = append(entries, manifestEntry{
				ID: cp.ID, FilePath: cp.FilePath, Turn: cp.Turn,
				Operation: cp.Operation, Size: cp.Size, Spilled: true,
			})
		}
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(manifest{
		SessionID:   filepath.Base(m.spillDir),
		RootDir:     m.rootDir,
		Checkpoints: entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.spillDir, "manifest.json"), data, 0644)
}

// DetectOrphaned scans baseDir for checkpoint directories whose session.lock
// PID is no longer alive.
func DetectOrphaned(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var orphans []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		lockPath := filepath.Join(baseDir, e.Name(), "session.lock")
		data, err := os.ReadFile(lockPath)
		if err != nil {
			continue // no lock file — skip
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			orphans = append(orphans, e.Name())
			continue
		}
		if !isProcessAlive(pid) {
			orphans = append(orphans, e.Name())
		}
	}
	return orphans, nil
}

// RecoverSession restores all checkpointed files from an orphaned session.
func RecoverSession(baseDir, sessionID string) ([]string, error) {
	manifestPath := filepath.Join(baseDir, sessionID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	var restored []string
	for _, entry := range m.Checkpoints {
		spillPath := filepath.Join(baseDir, sessionID, entry.ID+".bak")
		content, err := os.ReadFile(spillPath)
		if err != nil {
			continue // best effort
		}
		if err := os.WriteFile(entry.FilePath, content, 0644); err != nil {
			continue
		}
		restored = append(restored, entry.FilePath)
	}
	return restored, nil
}

// CleanupOrphaned removes all orphaned checkpoint directories.
func CleanupOrphaned(baseDir string) error {
	orphans, err := DetectOrphaned(baseDir)
	if err != nil {
		return err
	}
	for _, id := range orphans {
		os.RemoveAll(filepath.Join(baseDir, id))
	}
	return nil
}

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/checkpoint/ -run "TestDetectOrphaned|TestCleanupOrphaned" -v`
Expected: PASS

- [ ] **Step 5: Update Capture to write manifest on spill, and New to write lock**

In `New()`, add `mgr.WriteLock()` call after creating spillDir.
In `Capture()`, call `m.WriteManifest()` after spill or eviction (only when data goes to disk).

- [ ] **Step 6: Run all checkpoint tests**

Run: `go test ./internal/checkpoint/ -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```
[BEHAVIORAL] add crash recovery with PID lock and manifest
```

---

## Chunk 3: Middleware + Commands + Agent Integration

### Task 10: CheckpointMiddleware

**Files:**
- Create: `internal/toolexec/checkpoint.go`
- Test: `internal/toolexec/checkpoint_test.go`

- [ ] **Step 1: Write failing test**

```go
package toolexec_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckpointMiddlewareCaptures(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "test.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, err := checkpoint.New(rootDir, "mw-test", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	turn := 1
	mw := toolexec.CheckpointMiddleware(mgr, func() int { return turn })

	called := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		called = true
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	input, _ := json.Marshal(map[string]string{"operation": "write", "path": "test.go"})
	result := handler(context.Background(), toolexec.ToolCall{Name: "file", Input: input})

	assert.True(t, called)
	assert.Equal(t, "ok", result.Content)
	assert.Len(t, mgr.List(), 1, "should have captured a checkpoint")
}

func TestCheckpointMiddlewareSkipsRead(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "mw-read", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	mw := toolexec.CheckpointMiddleware(mgr, func() int { return 1 })
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	input, _ := json.Marshal(map[string]string{"operation": "read", "path": "test.go"})
	handler(context.Background(), toolexec.ToolCall{Name: "file", Input: input})

	assert.Empty(t, mgr.List(), "read should not capture checkpoint")
}

func TestCheckpointMiddlewareSkipsNonFile(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "mw-nonfile", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	mw := toolexec.CheckpointMiddleware(mgr, func() int { return 1 })
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	handler(context.Background(), toolexec.ToolCall{Name: "shell", Input: json.RawMessage(`{}`)})

	assert.Empty(t, mgr.List(), "non-file tool should not capture checkpoint")
}

func TestCheckpointMiddlewareNilPassthrough(t *testing.T) {
	mw := toolexec.CheckpointMiddleware(nil, nil)
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{Name: "file", Input: json.RawMessage(`{}`)})
	assert.Equal(t, "ok", result.Content)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/toolexec/ -run TestCheckpointMiddleware -v`
Expected: FAIL — CheckpointMiddleware not defined

- [ ] **Step 3: Write implementation**

```go
package toolexec

import (
	"context"
	"encoding/json"
	"log"

	"github.com/julianshen/rubichan/internal/checkpoint"
)

// CheckpointMiddleware returns a Middleware that captures file state before
// write/patch operations. If mgr is nil, the middleware passes through.
func CheckpointMiddleware(mgr *checkpoint.Manager, turnCounter func() int) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if mgr == nil || tc.Name != "file" {
				return next(ctx, tc)
			}

			var input struct {
				Operation string `json:"operation"`
				Path      string `json:"path"`
			}
			if err := json.Unmarshal(tc.Input, &input); err != nil {
				return next(ctx, tc)
			}

			if input.Operation != "write" && input.Operation != "patch" {
				return next(ctx, tc)
			}

			turn := 0
			if turnCounter != nil {
				turn = turnCounter()
			}

			if _, err := mgr.Capture(ctx, input.Path, turn, input.Operation); err != nil {
				log.Printf("checkpoint capture failed: %v", err)
			}

			return next(ctx, tc)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/toolexec/ -run TestCheckpointMiddleware -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add CheckpointMiddleware for toolexec pipeline
```

---

### Task 11: Undo and Rewind slash commands

**Files:**
- Create: `internal/commands/checkpoint.go`
- Test: `internal/commands/checkpoint_test.go`

- [ ] **Step 1: Write failing test**

```go
package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUndoCommand(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "main.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, _ := checkpoint.New(rootDir, "cmd-undo", 0)
	defer mgr.Cleanup()
	mgr.Capture(context.Background(), "main.go", 1, "write")
	os.WriteFile(testFile, []byte("modified"), 0644)

	cmd := commands.NewUndoCommand(mgr)
	assert.Equal(t, "undo", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "main.go")

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
}

func TestUndoCommandEmptyStack(t *testing.T) {
	rootDir := t.TempDir()
	mgr, _ := checkpoint.New(rootDir, "cmd-undo-empty", 0)
	defer mgr.Cleanup()

	cmd := commands.NewUndoCommand(mgr)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No checkpoints")
}

func TestRewindCommand(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "a.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, _ := checkpoint.New(rootDir, "cmd-rewind", 0)
	defer mgr.Cleanup()

	mgr.Capture(context.Background(), "a.go", 1, "write")
	os.WriteFile(testFile, []byte("turn1"), 0644)
	mgr.Capture(context.Background(), "a.go", 2, "patch")
	os.WriteFile(testFile, []byte("turn2"), 0644)

	cmd := commands.NewRewindCommand(mgr)
	assert.Equal(t, "rewind", cmd.Name())

	result, err := cmd.Execute(context.Background(), []string{"0"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Reverted")

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
}

func TestRewindCommandMissingArg(t *testing.T) {
	rootDir := t.TempDir()
	mgr, _ := checkpoint.New(rootDir, "cmd-rewind-noarg", 0)
	defer mgr.Cleanup()

	cmd := commands.NewRewindCommand(mgr)
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/commands/ -run "TestUndoCommand|TestRewindCommand" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write implementation**

```go
package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/julianshen/rubichan/internal/checkpoint"
)

// --- undo ---

type undoCommand struct {
	mgr *checkpoint.Manager
}

func NewUndoCommand(mgr *checkpoint.Manager) SlashCommand {
	return &undoCommand{mgr: mgr}
}

func (c *undoCommand) Name() string        { return "undo" }
func (c *undoCommand) Description() string { return "Undo the last file edit" }
func (c *undoCommand) Arguments() []ArgumentDef { return nil }
func (c *undoCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *undoCommand) Execute(ctx context.Context, _ []string) (Result, error) {
	if c.mgr == nil {
		return Result{Output: "Checkpoints not available."}, nil
	}
	path, err := c.mgr.Undo(ctx)
	if err != nil {
		if errors.Is(err, checkpoint.ErrNoCheckpoints) {
			return Result{Output: "No checkpoints to undo."}, nil
		}
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("Reverted %s", path)}, nil
}

// --- rewind ---

type rewindCommand struct {
	mgr *checkpoint.Manager
}

func NewRewindCommand(mgr *checkpoint.Manager) SlashCommand {
	return &rewindCommand{mgr: mgr}
}

func (c *rewindCommand) Name() string        { return "rewind" }
func (c *rewindCommand) Description() string { return "Rewind all edits after turn N" }
func (c *rewindCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{{Name: "turn", Description: "Turn number to rewind to", Required: true}}
}
func (c *rewindCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *rewindCommand) Execute(ctx context.Context, args []string) (Result, error) {
	if len(args) == 0 {
		return Result{}, fmt.Errorf("turn number is required: /rewind N")
	}
	turn, err := strconv.Atoi(args[0])
	if err != nil {
		return Result{}, fmt.Errorf("invalid turn number: %s", args[0])
	}
	if c.mgr == nil {
		return Result{Output: "Checkpoints not available."}, nil
	}
	paths, err := c.mgr.RewindToTurn(ctx, turn)
	if err != nil {
		return Result{}, err
	}
	if len(paths) == 0 {
		return Result{Output: "No checkpoints to rewind."}, nil
	}
	return Result{Output: fmt.Sprintf("Reverted %d file(s):\n  - %s", len(paths), joinPaths(paths))}, nil
}

func joinPaths(paths []string) string {
	result := ""
	for i, p := range paths {
		if i > 0 {
			result += "\n  - "
		}
		result += p
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ -run "TestUndoCommand|TestRewindCommand" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add /undo and /rewind slash commands
```

---

### Task 12: Agent integration — WithCheckpointManager + pipeline wiring

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestAgentWithCheckpointManager(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "agent-test", 0)
	require.NoError(t, err)
	defer mgr.Cleanup()

	// Use the actual agent.New signature: provider, registry, approveFunc, config, ...opts
	a := agent.New(mockProvider, mockRegistry, nil, &config.Config{}, agent.WithCheckpointManager(mgr))
	assert.NotNil(t, a)
	assert.Equal(t, mgr.List(), a.Checkpoints())
}
```

**Note:** Match the test setup to existing agent tests in `agent_test.go` for the correct mock provider, registry, and config patterns.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentWithCheckpointManager -v`
Expected: FAIL — WithCheckpointManager not defined

- [ ] **Step 3: Write implementation**

Add to `internal/agent/agent.go`:

```go
import "github.com/julianshen/rubichan/internal/checkpoint"

// Add to Agent struct:
checkpointMgr *checkpoint.Manager

// WithCheckpointManager attaches a checkpoint manager for file edit undo/rewind.
func WithCheckpointManager(mgr *checkpoint.Manager) AgentOption {
	return func(a *Agent) {
		a.checkpointMgr = mgr
	}
}

// Undo reverts the most recent file checkpoint.
func (a *Agent) Undo(ctx context.Context) (string, error) {
	if a.checkpointMgr == nil {
		return "", fmt.Errorf("checkpoint manager not configured")
	}
	return a.checkpointMgr.Undo(ctx)
}

// RewindToTurn reverts all file checkpoints after the given turn.
func (a *Agent) RewindToTurn(ctx context.Context, turn int) ([]string, error) {
	if a.checkpointMgr == nil {
		return nil, fmt.Errorf("checkpoint manager not configured")
	}
	return a.checkpointMgr.RewindToTurn(ctx, turn)
}

// Checkpoints returns the current checkpoint stack.
func (a *Agent) Checkpoints() []checkpoint.Checkpoint {
	if a.checkpointMgr == nil {
		return nil
	}
	return a.checkpointMgr.List()
}
```

In `internal/agent/agent.go`, the default pipeline is built at lines 356-372 inside `New()` when `a.pipeline == nil`. Insert the checkpoint middleware between the hook middleware and the post-hook middleware:

```go
// At line ~361, after: middlewares = append(middlewares, toolexec.HookMiddleware(hookAdapter))
// And before: middlewares = append(middlewares, toolexec.PostHookMiddleware(hookAdapter))
// Insert:
if a.checkpointMgr != nil {
    middlewares = append(middlewares, toolexec.CheckpointMiddleware(a.checkpointMgr, func() int {
        return int(a.turnNumber.Load())
    }))
}
```

This ensures the ordering matches the spec: Hook → Checkpoint → PostHook → OutputMgr → Executor.

Add turn counter method:

```go
func (a *Agent) currentTurn() int {
    // The turn count is tracked in runLoop's local variable.
    // Store it atomically for middleware access.
    return int(a.turnNumber.Load())
}
```

Add `turnNumber atomic.Int32` to Agent struct and update `runLoop` to store it:

```go
// At start of each iteration in runLoop:
a.turnNumber.Store(int32(turnCount))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentWithCheckpointManager -v`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] wire checkpoint manager into agent and pipeline
```

---

### Task 13: Final integration — run full test suite + lint

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

- [ ] **Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No files listed

- [ ] **Step 4: Check test coverage for new packages**

Run: `go test -cover ./internal/checkpoint/`
Expected: >90% coverage

- [ ] **Step 5: Commit any final fixes**

```
[STRUCTURAL] fix lint and formatting issues
```
