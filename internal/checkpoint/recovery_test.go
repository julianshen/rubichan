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

