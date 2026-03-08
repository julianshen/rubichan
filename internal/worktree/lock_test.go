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
		_ = fl2.Unlock()
	}

	_ = fl1.Unlock()

	// After release, should be acquirable again.
	if err := fl2.Lock(); err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	_ = fl2.Unlock()
}

func TestFileLock_UnlockNil(t *testing.T) {
	fl := &fileLock{path: "/nonexistent"}
	if err := fl.Unlock(); err != nil {
		t.Fatalf("Unlock on nil file should succeed: %v", err)
	}
}

func TestFileLock_OpenFileError(t *testing.T) {
	// Use a path where the parent exists but the lock file path is a directory,
	// causing OpenFile to fail.
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "lockfile")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fl := &fileLock{path: lockDir}
	err := fl.Lock()
	if err == nil {
		_ = fl.Unlock()
		t.Fatal("expected error opening directory as file")
	}
}

func TestFileLock_TryLock_OpenFileError(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "lockfile")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fl := &fileLock{path: lockDir}
	err := fl.TryLock()
	if err == nil {
		_ = fl.Unlock()
		t.Fatal("expected error from TryLock with directory as file")
	}
}

func TestFileLock_MkdirAllError(t *testing.T) {
	// Place a file where the parent directory should be.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(blocker, "subdir", ".lock")

	fl := &fileLock{path: lockPath}
	err := fl.Lock()
	if err == nil {
		_ = fl.Unlock()
		t.Fatal("expected MkdirAll error")
	}
}

func TestFileLock_TryLock_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(blocker, "subdir", ".lock")

	fl := &fileLock{path: lockPath}
	err := fl.TryLock()
	if err == nil {
		_ = fl.Unlock()
		t.Fatal("expected MkdirAll error from TryLock")
	}
}
