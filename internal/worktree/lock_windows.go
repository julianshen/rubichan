//go:build windows

package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fileLock provides a process-local mutex-based lock on Windows.
// Windows does not support flock(2); this provides basic safety within
// a single process. Cross-process locking on Windows is not yet supported.
type fileLock struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

// Lock acquires the lock (process-local on Windows).
func (l *fileLock) Lock() error {
	l.mu.Lock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		l.mu.Unlock()
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		l.mu.Unlock()
		return fmt.Errorf("opening lock file: %w", err)
	}
	l.f = f
	return nil
}

// TryLock attempts to acquire the lock without blocking.
func (l *fileLock) TryLock() error {
	if !l.mu.TryLock() {
		return fmt.Errorf("lock already held")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		l.mu.Unlock()
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		l.mu.Unlock()
		return fmt.Errorf("opening lock file: %w", err)
	}
	l.f = f
	return nil
}

// Unlock releases the lock.
func (l *fileLock) Unlock() error {
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	l.mu.Unlock()
	return err
}
