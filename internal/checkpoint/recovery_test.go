package checkpoint_test

import (
	"fmt"
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
	os.WriteFile(filepath.Join(sessionDir, "session.lock"), []byte("999999999"), 0644)
	os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte(`{"session_id":"dead-session","root_dir":"/tmp","checkpoints":[]}`), 0644)

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.Contains(t, orphans, "dead-session")
}

func TestDetectOrphanedSkipsLiveSession(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "live-session")
	os.MkdirAll(sessionDir, 0755)
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

	targetFile := filepath.Join(targetDir, "main.go")
	os.WriteFile(targetFile, []byte("modified"), 0644)

	os.WriteFile(filepath.Join(sessionDir, "cp-001.bak"), []byte("original"), 0644)

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

func TestRecoverSessionMissingSpillFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "missing-spill")
	os.MkdirAll(sessionDir, 0755)

	targetFile := filepath.Join(targetDir, "main.go")
	os.WriteFile(targetFile, []byte("modified"), 0644)

	// Manifest references a spill file that does not exist
	manifest := fmt.Sprintf(`{"session_id":"missing-spill","root_dir":"%s","checkpoints":[{"id":"ghost-001","file_path":"%s","turn":1,"operation":"write","size":8,"spilled":true}]}`, targetDir, targetFile)
	os.WriteFile(filepath.Join(sessionDir, "manifest.json"), []byte(manifest), 0644)

	restored, err := checkpoint.RecoverSession(tmpDir, "missing-spill")
	// Missing spill file reports partial recovery error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "partial recovery")
	assert.Empty(t, restored)

	// Original file should be untouched
	data, _ := os.ReadFile(targetFile)
	assert.Equal(t, "modified", string(data))
}

func TestDetectOrphanedNonExistentDir(t *testing.T) {
	orphans, err := checkpoint.DetectOrphaned("/nonexistent/path/that/cannot/exist/xyzzy")
	assert.NoError(t, err)
	assert.Nil(t, orphans)
}

func TestDetectOrphanedInvalidPID(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "bad-pid")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "session.lock"), []byte("not-a-number"), 0644)

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.Contains(t, orphans, "bad-pid")
}

func TestDetectOrphanedNoLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "no-lock")
	os.MkdirAll(sessionDir, 0755)
	// No session.lock — entry is skipped (not treated as orphaned)

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.NotContains(t, orphans, "no-lock")
}

func TestDetectOrphanedSkipsNonDirectoryEntries(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a regular file (not a directory) at the top level
	os.WriteFile(filepath.Join(tmpDir, "notadir.txt"), []byte("file"), 0644)

	orphans, err := checkpoint.DetectOrphaned(tmpDir)
	require.NoError(t, err)
	assert.NotContains(t, orphans, "notadir.txt")
}

func TestDetectOrphanedBaseDirIsFile(t *testing.T) {
	// Pass a regular file as baseDir — os.ReadDir fails with a non-IsNotExist error
	tmpDir := t.TempDir()
	notADir := filepath.Join(tmpDir, "notadir")
	os.WriteFile(notADir, []byte("file"), 0644)

	_, err := checkpoint.DetectOrphaned(notADir)
	assert.Error(t, err)
}

func TestCleanupOrphanedBaseDirIsFile(t *testing.T) {
	// When DetectOrphaned returns an error, CleanupOrphaned propagates it
	tmpDir := t.TempDir()
	notADir := filepath.Join(tmpDir, "notadir")
	os.WriteFile(notADir, []byte("file"), 0644)

	err := checkpoint.CleanupOrphaned(notADir)
	assert.Error(t, err)
}
