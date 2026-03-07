package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// fileLock provides file-based locking using flock(2).
type fileLock struct {
	path string
	f    *os.File
}

// Lock acquires the file lock, blocking until available.
func (l *fileLock) Lock() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return fmt.Errorf("acquiring lock: %w", err)
	}
	l.f = f
	return nil
}

// TryLock attempts to acquire the lock without blocking.
// Returns an error if the lock is already held.
func (l *fileLock) TryLock() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("lock already held")
	}
	l.f = f
	return nil
}

// Unlock releases the file lock.
func (l *fileLock) Unlock() error {
	if l.f == nil {
		return nil
	}
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		l.f.Close()
		return fmt.Errorf("releasing lock: %w", err)
	}
	return l.f.Close()
}
