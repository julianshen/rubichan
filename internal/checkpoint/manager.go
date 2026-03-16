package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrNoCheckpoints is returned when Undo is called on an empty checkpoint stack.
var ErrNoCheckpoints = errors.New("no checkpoints to undo")

const defaultMemBudget = 100 * 1024 * 1024 // 100MB
const spillThreshold = 1024 * 1024         // 1MB

// Checkpoint represents a snapshot of a file before modification.
type Checkpoint struct {
	ID           string
	FilePath     string // absolute path
	Turn         int
	Timestamp    time.Time
	Operation    string      // "write" or "patch"
	OriginalData []byte      // nil if file did not exist (creation checkpoint)
	FileMode     os.FileMode // original file permissions (0 if file did not exist)
	Size         int64
	spilled   bool
	spillPath string
}

// IsSpilled reports whether the checkpoint's data has been written to disk
// rather than held in memory.
func (c Checkpoint) IsSpilled() bool { return c.spilled }

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

	// Resolve symlinks in rootDir so path traversal checks work correctly
	// on platforms where TempDir returns a symlinked path (e.g., macOS /var → /private/var).
	evalRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		evalRoot = rootDir
	}

	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", sessionID)
	if err := os.MkdirAll(spillDir, 0755); err != nil {
		return nil, err
	}

	mgr := &Manager{
		rootDir:   evalRoot,
		memBudget: memBudget,
		spillDir:  spillDir,
	}
	mgr.writeLock() // best-effort PID lock; ignore error
	return mgr, nil
}

// List returns a copy of all checkpoints in the stack (oldest first).
func (m *Manager) List() []Checkpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Checkpoint, len(m.stack))
	copy(cp, m.stack)
	return cp
}

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

	needsManifest := false

	m.mu.Lock()
	// Spill large files directly to disk
	if size > spillThreshold {
		spillPath := filepath.Join(m.spillDir, id+".bak")
		if err := os.WriteFile(spillPath, data, 0644); err != nil {
			m.mu.Unlock()
			return "", fmt.Errorf("checkpoint spill: %w", err)
		}
		cp.spilled = true
		cp.spillPath = spillPath
		cp.OriginalData = nil // don't hold in memory
		needsManifest = true
	} else {
		// Check budget and evict if needed. Break if eviction fails
		// (e.g., disk full) to avoid an infinite loop.
		for m.memUsed+size > m.memBudget && len(m.stack) > 0 {
			if !m.evictOldest() {
				break // can't evict — accept over-budget rather than loop forever
			}
			needsManifest = true
		}
		m.memUsed += size
	}

	m.stack = append(m.stack, cp)
	m.mu.Unlock()

	if needsManifest {
		m.writeManifest() // best-effort manifest update; ignore error
	}

	return id, nil
}

// resolvePath resolves a relative path to absolute under rootDir with symlink
// resolution and path traversal check.
func (m *Manager) resolvePath(relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		if !strings.HasPrefix(relPath, m.rootDir+string(filepath.Separator)) && relPath != m.rootDir {
			return "", fmt.Errorf("path traversal denied: %s escapes root", relPath)
		}
		return relPath, nil
	}

	joined := filepath.Join(m.rootDir, relPath)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("abs: %w", err)
	}

	evalPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			dir := filepath.Dir(abs)
			evalDir, dirErr := filepath.EvalSymlinks(dir)
			if dirErr != nil {
				return abs, nil
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

// Undo pops the most recent checkpoint and restores the file to its captured state.
// If the checkpoint was a creation (OriginalData == nil), the file is deleted.
// Returns the absolute path of the restored file.
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

// restore writes the checkpoint's original data back to disk (or deletes the file
// if OriginalData is nil and not spilled, indicating the file was created after the checkpoint).
func (m *Manager) restore(cp Checkpoint) error {
	var data []byte
	if cp.spilled {
		var err error
		data, err = os.ReadFile(cp.spillPath)
		if err != nil {
			return fmt.Errorf("read spill file: %w", err)
		}
		os.Remove(cp.spillPath)
	} else {
		if cp.OriginalData == nil {
			return os.Remove(cp.FilePath)
		}
		data = cp.OriginalData
	}

	mode := cp.FileMode
	if mode == 0 {
		mode = 0644
	}
	return os.WriteFile(cp.FilePath, data, mode)
}

// RewindToTurn reverts all checkpoints with turn > the given turn number,
// in reverse order (newest first). Each checkpoint is restored individually;
// intermediate checkpoints for the same file are applied in sequence, not skipped.
func (m *Manager) RewindToTurn(ctx context.Context, turn int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find cutoff: last index where Turn <= turn
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

// Cleanup removes the spill directory and all checkpoint data.
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stack = nil
	m.memUsed = 0
	return os.RemoveAll(m.spillDir)
}

// evictOldest finds the oldest in-memory checkpoint and spills it to disk.
// Returns true if a checkpoint was evicted, false if none could be evicted
// (e.g., all already spilled or all writes failed). The caller must hold m.mu.
func (m *Manager) evictOldest() bool {
	for i, cp := range m.stack {
		if !cp.spilled && cp.OriginalData != nil {
			spillPath := filepath.Join(m.spillDir, cp.ID+".bak")
			if err := os.WriteFile(spillPath, cp.OriginalData, 0644); err != nil {
				continue
			}
			m.stack[i].spilled = true
			m.stack[i].spillPath = spillPath
			m.stack[i].OriginalData = nil
			m.memUsed -= cp.Size
			return true
		}
	}
	return false
}
