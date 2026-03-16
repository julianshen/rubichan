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

// Checkpoint represents a snapshot of a file before modification.
type Checkpoint struct {
	ID           string
	FilePath     string      // absolute path
	Turn         int
	Timestamp    time.Time
	Operation    string      // "write" or "patch"
	OriginalData []byte      // nil if file did not exist (creation checkpoint)
	FileMode     os.FileMode // original file permissions (0 if file did not exist)
	Size      int64
	Spilled   bool
	SpillPath string
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

	return &Manager{
		rootDir:   evalRoot,
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

	if !cp.Spilled {
		m.memUsed -= cp.Size
	}

	return cp.FilePath, nil
}

// restore writes the checkpoint's original data back to disk (or deletes the file
// if OriginalData is nil, indicating the file was created after the checkpoint).
func (m *Manager) restore(cp Checkpoint) error {
	if cp.OriginalData == nil {
		return os.Remove(cp.FilePath)
	}

	data := cp.OriginalData
	if cp.Spilled {
		var err error
		data, err = os.ReadFile(cp.SpillPath)
		if err != nil {
			return fmt.Errorf("read spill file: %w", err)
		}
		os.Remove(cp.SpillPath)
	}

	mode := cp.FileMode
	if mode == 0 {
		mode = 0644
	}
	return os.WriteFile(cp.FilePath, data, mode)
}

// Cleanup removes the spill directory and all checkpoint data.
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stack = nil
	m.memUsed = 0
	return os.RemoveAll(m.spillDir)
}
