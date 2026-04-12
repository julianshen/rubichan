package checkpoint_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- restore() edge cases ---

func TestUndoRestoreSpilledFileWithMissingSpillFile(t *testing.T) {
	// Capture a large file so it spills, then delete the spill file before undo.
	// This exercises the restore() error path for spilled files.
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	data := make([]byte, 2*1024*1024) // 2MB — exceeds spillThreshold
	require.NoError(t, os.WriteFile(bigFile, data, 0644))

	mgr, err := checkpoint.New(rootDir, "restore-missing-spill", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 1)
	require.True(t, cps[0].IsSpilled())

	// Delete the spill file to force a restore error
	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "restore-missing-spill")
	entries, err := os.ReadDir(spillDir)
	require.NoError(t, err)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".bak" {
			require.NoError(t, os.Remove(filepath.Join(spillDir, e.Name())))
		}
	}

	// Undo should fail and re-push the checkpoint
	_, err = mgr.Undo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undo restore")

	// The checkpoint should still be on the stack (re-pushed on failure)
	assert.Len(t, mgr.List(), 1, "checkpoint should be re-pushed on undo failure")
}

func TestUndoCreationCheckpointAlreadyDeleted(t *testing.T) {
	// Test restore() for creation checkpoint where file is already gone (idempotent).
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "restore-idempotent", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// Capture before file exists
	_, err = mgr.Capture(context.Background(), "ephemeral.go", 1, "write")
	require.NoError(t, err)

	// File never actually created (or deleted already) — undo should succeed
	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, path)
}

func TestUndoRestoreWriteFailure(t *testing.T) {
	// Test restore() write failure for an in-memory checkpoint.
	// Create a file, capture it, then remove the parent directory
	// so write fails because the path doesn't exist.
	rootDir := t.TempDir()
	subDir := filepath.Join(rootDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	testFile := filepath.Join(subDir, "file.go")
	require.NoError(t, os.WriteFile(testFile, []byte("original"), 0644))

	mgr, err := checkpoint.New(rootDir, "restore-write-fail", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), testFile, 1, "write")
	require.NoError(t, err)

	// Remove the entire parent directory so restore cannot write
	require.NoError(t, os.RemoveAll(subDir))

	// Undo should fail (cannot write to non-existent directory)
	_, err = mgr.Undo(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undo restore")

	// Re-pushed on failure
	assert.Len(t, mgr.List(), 1)
}

// --- RecoverSession() error paths ---

func TestRecoverSessionMissingManifest(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "no-manifest")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	// No manifest.json at all
	_, err := checkpoint.RecoverSession(tmpDir, "no-manifest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

func TestRecoverSessionPathTraversal(t *testing.T) {
	// Manifest with a file path that escapes root_dir should be rejected.
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "traversal-sess")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	// Write spill file
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "cp-001.bak"), []byte("data"), 0644))

	manifest := `{"session_id":"traversal-sess","root_dir":"/safe/root","checkpoints":[{"id":"cp-001","file_path":"/etc/passwd","turn":1,"operation":"write","size":4,"spilled":true}]}`
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte(manifest), 0644))

	restored, err := checkpoint.RecoverSession(tmpDir, "traversal-sess")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "partial recovery")
	assert.Contains(t, err.Error(), "escapes root")
	assert.Empty(t, restored)
}

func TestRecoverSessionWriteFailure(t *testing.T) {
	// Test when the target file path cannot be written to.
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "write-fail-sess")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	// Use a non-existent directory as the target path
	targetFile := filepath.Join(tmpDir, "nonexistent-dir", "file.go")

	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "cp-001.bak"), []byte("data"), 0644))

	manifest, _ := json.Marshal(map[string]interface{}{
		"session_id": "write-fail-sess",
		"root_dir":   tmpDir,
		"checkpoints": []map[string]interface{}{
			{
				"id":        "cp-001",
				"file_path": targetFile,
				"turn":      1,
				"operation": "write",
				"size":      4,
				"spilled":   true,
			},
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "manifest.json"), manifest, 0644))

	restored, err := checkpoint.RecoverSession(tmpDir, "write-fail-sess")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "partial recovery")
	assert.Empty(t, restored)
}

func TestRecoverSessionEmptyRootDir(t *testing.T) {
	// When root_dir is empty in the manifest, path traversal check is skipped.
	tmpDir := t.TempDir()
	targetDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "empty-root")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	targetFile := filepath.Join(targetDir, "main.go")
	require.NoError(t, os.WriteFile(targetFile, []byte("modified"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "cp-001.bak"), []byte("original"), 0644))

	manifest := `{"session_id":"empty-root","root_dir":"","checkpoints":[{"id":"cp-001","file_path":"` + targetFile + `","turn":1,"operation":"write","size":8,"spilled":true}]}`
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte(manifest), 0644))

	restored, err := checkpoint.RecoverSession(tmpDir, "empty-root")
	require.NoError(t, err)
	assert.Len(t, restored, 1)

	data, _ := os.ReadFile(targetFile)
	assert.Equal(t, "original", string(data))
}

// --- evictOldest() edge cases ---

func TestEvictOldestAllAlreadySpilledOrCreation(t *testing.T) {
	// When all checkpoints are already spilled or creation (nil data),
	// eviction should return false and the new capture should still succeed
	// (budget exceeded but accepted).
	rootDir := t.TempDir()

	// Create a small budget: 500 bytes
	mgr, err := checkpoint.New(rootDir, "evict-all-spilled", 500)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// Capture a file that doesn't exist (creation checkpoint — nil data, size 0)
	_, err = mgr.Capture(context.Background(), "nonexistent1.go", 1, "write")
	require.NoError(t, err)

	// Now capture a file that uses 400 bytes (under 500)
	smallFile := filepath.Join(rootDir, "small.go")
	smallData := make([]byte, 400)
	require.NoError(t, os.WriteFile(smallFile, smallData, 0644))
	_, err = mgr.Capture(context.Background(), "small.go", 2, "write")
	require.NoError(t, err)

	// Capture another 400 bytes — this will exceed budget and try to evict.
	// The first checkpoint is creation (nil data), so eviction should spill the second.
	smallFile2 := filepath.Join(rootDir, "small2.go")
	require.NoError(t, os.WriteFile(smallFile2, smallData, 0644))
	_, err = mgr.Capture(context.Background(), "small2.go", 3, "write")
	require.NoError(t, err)

	cps := mgr.List()
	assert.Len(t, cps, 3)
	// The second checkpoint should have been evicted (spilled)
	assert.True(t, cps[1].IsSpilled(), "second checkpoint should be evicted/spilled")
}

func TestEvictOldestSpillWriteFailure(t *testing.T) {
	// When the spill directory becomes unwritable, eviction fails gracefully.
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "evict-fail", 500)
	require.NoError(t, err)
	defer func() {
		// Restore permissions for cleanup
		spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "evict-fail")
		os.Chmod(spillDir, 0755)
		_ = mgr.Cleanup()
	}()

	// Capture a 400-byte file
	smallFile := filepath.Join(rootDir, "small.go")
	smallData := make([]byte, 400)
	require.NoError(t, os.WriteFile(smallFile, smallData, 0644))
	_, err = mgr.Capture(context.Background(), "small.go", 1, "write")
	require.NoError(t, err)

	// Make spill dir read-only so eviction fails
	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "evict-fail")
	require.NoError(t, os.Chmod(spillDir, 0555))

	// Capture another 400 bytes — exceeds budget, eviction fails, accepts over-budget
	smallFile2 := filepath.Join(rootDir, "small2.go")
	require.NoError(t, os.WriteFile(smallFile2, smallData, 0644))
	_, err = mgr.Capture(context.Background(), "small2.go", 2, "write")
	require.NoError(t, err)

	cps := mgr.List()
	assert.Len(t, cps, 2)
	// First checkpoint should NOT be spilled (eviction failed)
	assert.False(t, cps[0].IsSpilled(), "eviction should fail — checkpoint stays in-memory")
}

// --- RewindToTurn edge cases ---

func TestRewindToTurnAllCheckpoints(t *testing.T) {
	// Rewind to turn -1 (before all checkpoints exist)
	rootDir := t.TempDir()
	file1 := filepath.Join(rootDir, "a.go")
	require.NoError(t, os.WriteFile(file1, []byte("original"), 0644))

	mgr, err := checkpoint.New(rootDir, "rewind-all", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "a.go", 1, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file1, []byte("modified"), 0644))

	paths, err := mgr.RewindToTurn(context.Background(), -1)
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Empty(t, mgr.List())

	data, _ := os.ReadFile(file1)
	assert.Equal(t, "original", string(data))
}

func TestRewindToTurnNothingToRewind(t *testing.T) {
	// Rewind to a turn >= all checkpoint turns — nothing should happen.
	rootDir := t.TempDir()
	file1 := filepath.Join(rootDir, "a.go")
	require.NoError(t, os.WriteFile(file1, []byte("original"), 0644))

	mgr, err := checkpoint.New(rootDir, "rewind-nothing", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "a.go", 1, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file1, []byte("modified"), 0644))

	paths, err := mgr.RewindToTurn(context.Background(), 5)
	require.NoError(t, err)
	assert.Empty(t, paths)
	assert.Len(t, mgr.List(), 1, "all checkpoints should remain")

	// File should NOT be restored
	data, _ := os.ReadFile(file1)
	assert.Equal(t, "modified", string(data))
}

func TestRewindToTurnRestoreFailureTruncatesStack(t *testing.T) {
	// When a restore fails during rewind, the stack should be truncated
	// to reflect which checkpoints were actually restored.
	rootDir := t.TempDir()
	subDir := filepath.Join(rootDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	file1 := filepath.Join(subDir, "a.go")
	file2 := filepath.Join(rootDir, "b.go")
	require.NoError(t, os.WriteFile(file1, []byte("a-orig"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("b-orig"), 0644))

	mgr, err := checkpoint.New(rootDir, "rewind-fail", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// Turn 1: a.go
	_, err = mgr.Capture(context.Background(), file1, 1, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file1, []byte("a-modified"), 0644))

	// Turn 2: b.go
	_, err = mgr.Capture(context.Background(), file2, 2, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file2, []byte("b-modified"), 0644))

	// Remove subDir so restoring a.go fails (can't write to non-existent dir)
	require.NoError(t, os.RemoveAll(subDir))

	// Rewind to turn 0 — b.go restore should succeed (turn 2), a.go should fail (turn 1)
	paths, err := mgr.RewindToTurn(context.Background(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rewind restore")

	// b.go should have been restored — resolve symlinks for macOS
	resolvedFile2, _ := filepath.EvalSymlinks(file2)
	found := false
	for _, p := range paths {
		if p == file2 || p == resolvedFile2 {
			found = true
			break
		}
	}
	assert.True(t, found, "b.go should be in restored paths, got %v", paths)

	// b.go should be restored to original
	dataB, _ := os.ReadFile(file2)
	assert.Equal(t, "b-orig", string(dataB))
}

func TestRewindToTurnSpilledCheckpoint(t *testing.T) {
	// Rewind with a spilled checkpoint to cover the spilled restore path in rewindLocked.
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	bigData := make([]byte, 2*1024*1024) // 2MB — spill threshold
	for i := range bigData {
		bigData[i] = byte(i % 256)
	}
	require.NoError(t, os.WriteFile(bigFile, bigData, 0644))

	mgr, err := checkpoint.New(rootDir, "rewind-spilled", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.NoError(t, err)

	// Verify it was spilled
	cps := mgr.List()
	require.Len(t, cps, 1)
	require.True(t, cps[0].IsSpilled())

	// Overwrite the file
	require.NoError(t, os.WriteFile(bigFile, []byte("small"), 0644))

	// Rewind to turn 0 — should restore the spilled checkpoint
	paths, err := mgr.RewindToTurn(context.Background(), 0)
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Empty(t, mgr.List())

	restored, _ := os.ReadFile(bigFile)
	assert.Equal(t, bigData, restored)
}

// --- resolvePath edge cases ---

func TestCaptureStatErrorNonNotExist(t *testing.T) {
	// On some systems we can create a path that exists but stat fails.
	// This is hard to simulate portably, but we can test path resolution errors.
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "stat-error", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// A file path with null bytes should cause an error
	_, err = mgr.Capture(context.Background(), "file\x00name.go", 1, "write")
	require.Error(t, err)
}

// --- New() edge cases ---

func TestNewWithSymlinkedRootDir(t *testing.T) {
	// Test that New resolves symlinks in rootDir
	realDir := t.TempDir()
	linkDir := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realDir, linkDir))

	mgr, err := checkpoint.New(linkDir, "symlink-root", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// Create a file in the real dir
	testFile := filepath.Join(realDir, "test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0644))

	// Capture via the link — should resolve to real dir and work
	_, err = mgr.Capture(context.Background(), testFile, 1, "write")
	require.NoError(t, err)
	assert.Len(t, mgr.List(), 1)
}

// --- Capture spill error ---

func TestCaptureSpillErrorReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	bigData := make([]byte, 2*1024*1024) // 2MB
	require.NoError(t, os.WriteFile(bigFile, bigData, 0644))

	mgr, err := checkpoint.New(rootDir, "spill-error", 0)
	require.NoError(t, err)
	defer func() {
		spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "spill-error")
		os.Chmod(spillDir, 0755)
		_ = mgr.Cleanup()
	}()

	// Make spill dir read-only so spill write fails
	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "spill-error")
	require.NoError(t, os.Chmod(spillDir, 0555))

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint spill")
}

func TestProcessAliveZeroPID(t *testing.T) {
	// PID 0 should return false.
	// This is tested via DetectOrphaned with a "0" lock file.
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "zero-pid")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "session.lock"), []byte("0"), 0644))

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.Contains(t, orphans, "zero-pid", "PID 0 should be treated as dead/orphaned")
}
