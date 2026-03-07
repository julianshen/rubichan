package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileLock_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")

	fl := &fileLock{path: lockPath}
	if err := fl.Lock(); err != nil {
		t.Fatalf("Lock() = %v", err)
	}

	// Lock file should exist.
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	if err := fl.Unlock(); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
}

func TestFileLock_BlocksConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")

	fl1 := &fileLock{path: lockPath}
	if err := fl1.Lock(); err != nil {
		t.Fatal(err)
	}

	// Second lock attempt should fail (non-blocking TryLock).
	fl2 := &fileLock{path: lockPath}
	if err := fl2.TryLock(); err == nil {
		t.Error("TryLock should fail when lock is held")
		fl2.Unlock()
	}

	fl1.Unlock()

	// After release, should be acquirable again.
	if err := fl2.Lock(); err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	fl2.Unlock()
}

func TestFileLock_UnlockNil(t *testing.T) {
	fl := &fileLock{path: "/nonexistent"}
	if err := fl.Unlock(); err != nil {
		t.Fatalf("Unlock on nil file should succeed: %v", err)
	}
}
