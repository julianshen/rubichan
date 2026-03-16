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

// Cleanup removes the spill directory and all checkpoint data.
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stack = nil
	m.memUsed = 0
	return os.RemoveAll(m.spillDir)
}
