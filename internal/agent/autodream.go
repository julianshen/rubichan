package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

const defaultMinHours = 24
const defaultMinSessions = 5

// AutoDreamConfig controls when consolidation runs.
type AutoDreamConfig struct {
	MinHours    int
	MinSessions int
}

// DefaultAutoDreamConfig returns the default configuration.
func DefaultAutoDreamConfig() AutoDreamConfig {
	return AutoDreamConfig{
		MinHours:    defaultMinHours,
		MinSessions: defaultMinSessions,
	}
}

// ConsolidationLock is a file-based lock with PID tracking and rollback support.
type ConsolidationLock struct {
	memoryDir string
}

// NewConsolidationLock creates a new lock in the given memory directory.
func NewConsolidationLock(memoryDir string) *ConsolidationLock {
	return &ConsolidationLock{memoryDir: memoryDir}
}

// MemoryDir returns the memory directory path.
func (l *ConsolidationLock) MemoryDir() string {
	return l.memoryDir
}

func (l *ConsolidationLock) lockPath() string {
	return filepath.Join(l.memoryDir, ".consolidate-lock")
}

// ReadLastConsolidatedAt returns the last consolidation time from the lock file.
func (l *ConsolidationLock) ReadLastConsolidatedAt() (time.Time, error) {
	info, err := os.Stat(l.lockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("stat consolidation lock: %w", err)
	}
	return info.ModTime(), nil
}

// TryAcquire attempts to acquire the lock. Returns prior mtime if the lock existed.
func (l *ConsolidationLock) TryAcquire() (*time.Time, error) {
	if err := os.MkdirAll(l.memoryDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir memory dir: %w", err)
	}

	var priorMtime *time.Time
	info, err := os.Stat(l.lockPath())
	if err == nil {
		mt := info.ModTime()
		priorMtime = &mt
	}

	pid := os.Getpid()
	if err := os.WriteFile(l.lockPath(), []byte(fmt.Sprintf("%d", pid)), 0o644); err != nil {
		return nil, fmt.Errorf("write lock: %w", err)
	}

	return priorMtime, nil
}

// Rollback restores the lock file to its prior state.
func (l *ConsolidationLock) Rollback(priorMtime *time.Time) error {
	if priorMtime == nil {
		return os.Remove(l.lockPath())
	}
	pid := os.Getpid()
	if err := os.WriteFile(l.lockPath(), []byte(fmt.Sprintf("%d", pid)), 0o644); err != nil {
		return fmt.Errorf("rollback write: %w", err)
	}
	return os.Chtimes(l.lockPath(), *priorMtime, *priorMtime)
}

// RecordConsolidation updates the lock file to mark consolidation complete.
func (l *ConsolidationLock) RecordConsolidation() error {
	if err := os.MkdirAll(l.memoryDir, 0o755); err != nil {
		return fmt.Errorf("mkdir memory dir: %w", err)
	}
	pid := os.Getpid()
	return os.WriteFile(l.lockPath(), []byte(fmt.Sprintf("%d", pid)), 0o644)
}

// SessionInfo holds metadata about a session for consolidation gating.
type SessionInfo struct {
	SessionID string
	MTime     time.Time
}

// AutoDreamService performs periodic cross-session memory consolidation.
type AutoDreamService struct {
	mu        sync.Mutex
	cfg       AutoDreamConfig
	running   bool
	stopCh    chan struct{}
	memoryDir string
}

// NewAutoDreamService creates a new auto-dream service.
func NewAutoDreamService(memoryDir string, cfg AutoDreamConfig) *AutoDreamService {
	return &AutoDreamService{
		cfg:       cfg,
		memoryDir: memoryDir,
		stopCh:    make(chan struct{}),
	}
}

// IsGateOpen returns true if the consolidation gate is open (config > 0).
func (s *AutoDreamService) IsGateOpen() bool {
	return s.cfg.MinHours > 0 && s.cfg.MinSessions > 0
}

// ShouldRun checks if consolidation should run based on time since last run
// and number of recent sessions.
func (s *AutoDreamService) ShouldRun(sessions []SessionInfo, lastConsolidated time.Time, currentSessionID string) bool {
	hoursSince := time.Since(lastConsolidated).Hours()
	if hoursSince < float64(s.cfg.MinHours) {
		return false
	}

	recentCount := 0
	for _, sess := range sessions {
		if sess.MTime.After(lastConsolidated) && sess.SessionID != currentSessionID {
			recentCount++
		}
	}

	return recentCount >= s.cfg.MinSessions
}

// ExecuteDream runs the consolidation pass.
func (s *AutoDreamService) ExecuteDream(ctx context.Context, params agentsdk.DreamParams, callModel func(ctx context.Context, prompt string) (string, error)) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("dream already in progress")
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	lock := NewConsolidationLock(s.memoryDir)
	priorMtime, err := lock.TryAcquire()
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}

	prompt := BuildConsolidationPrompt(params.MemoryRoot, params.TranscriptDir, params.Extra)
	_, err = callModel(ctx, prompt)
	if err != nil {
		_ = lock.Rollback(priorMtime)
		return fmt.Errorf("dream model call failed: %w", err)
	}

	return lock.RecordConsolidation()
}

// BuildConsolidationPrompt builds the 4-phase consolidation prompt.
func BuildConsolidationPrompt(memoryRoot, transcriptDir, extra string) string {
	base := `# Dream: Memory Consolidation

You are performing a dream — a reflective pass over your memory files. Synthesize what you've learned recently into durable, well-organized memories so that future sessions can orient quickly.

Memory directory: ` + "`" + memoryRoot + "`" + `
Session transcripts: ` + "`" + transcriptDir + "`" + ` (large JSONL files — grep narrowly, don't read whole files)

---

## Phase 1 — Orient

- ` + "`" + `ls` + "`" + ` the memory directory to see what already exists
- Read ` + "`" + `MEMORY.md` + "`" + ` to understand the current index
- Skim existing topic files so you improve them rather than creating duplicates

## Phase 2 — Gather recent signal

Look for new information worth persisting. Sources in rough priority order:

1. **Daily logs** if present — these are the append-only stream
2. **Existing memories that drifted** — facts that contradict something you see in the codebase now
3. **Transcript search** — grep the JSONL transcripts for narrow terms

## Phase 3 — Consolidate

For each thing worth remembering, write or update a memory file at the top level of the memory directory.

Focus on:
- Merging new signal into existing topic files rather than creating near-duplicates
- Converting relative dates to absolute dates
- Deleting contradicted facts

## Phase 4 — Prune and index

Update ` + "`" + `MEMORY.md` + "`" + ` so it stays under 200 lines AND under ~25KB.

Return a brief summary of what you consolidated, updated, or pruned.`

	if extra != "" {
		base += "\n\n## Additional context\n\n" + extra
	}
	return base
}

// ListSessionsTouchedSince scans session files for recent activity.
func ListSessionsTouchedSince(dir string, since time.Time) ([]SessionInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []SessionInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(since) {
			name := entry.Name()
			ext := filepath.Ext(name)
			sessionID := name[:len(name)-len(ext)]
			results = append(results, SessionInfo{
				SessionID: sessionID,
				MTime:     info.ModTime(),
			})
		}
	}
	return results, nil
}
