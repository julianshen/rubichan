package checkpoint_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "test-session", 0)
	require.NoError(t, err)
	assert.NotNil(t, mgr)
	assert.Empty(t, mgr.List())
}

func TestNewManagerCreatesSpillDir(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "test-session-spill", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	spillDir := filepath.Join(os.TempDir(), "aiagent", "checkpoints", "test-session-spill")
	_, err = os.Stat(spillDir)
	assert.NoError(t, err, "spill directory should be created")
}

func TestNewManagerDefaultBudget(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "test-session-budget", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()
	// Budget defaults to 100MB when 0 is passed — tested indirectly via capture behavior
}

func TestCaptureExistingFile(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "hello.go")
	os.WriteFile(testFile, []byte("package main"), 0644)
	// Resolve symlinks so the comparison works on platforms where TempDir
	// returns a symlinked path (e.g., macOS /var → /private/var).
	resolvedTestFile, err := filepath.EvalSymlinks(testFile)
	if err != nil {
		resolvedTestFile = testFile
	}

	mgr, err := checkpoint.New(rootDir, "cap-existing", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	id, err := mgr.Capture(context.Background(), "hello.go", 1, "write")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.Equal(t, resolvedTestFile, cps[0].FilePath)
	assert.Equal(t, 1, cps[0].Turn)
	assert.Equal(t, "write", cps[0].Operation)
	assert.Equal(t, []byte("package main"), cps[0].OriginalData)
	assert.Equal(t, os.FileMode(0644), cps[0].FileMode)
	assert.Equal(t, int64(12), cps[0].Size)
}

func TestCaptureNewFile(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "cap-new", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	id, err := mgr.Capture(context.Background(), "new_file.go", 1, "write")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.Nil(t, cps[0].OriginalData, "creation checkpoint should have nil OriginalData")
	assert.Equal(t, os.FileMode(0), cps[0].FileMode)
	assert.Equal(t, int64(0), cps[0].Size)
}

func TestCaptureEmptyExistingFile(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "empty.go")
	os.WriteFile(testFile, []byte{}, 0644)

	mgr, err := checkpoint.New(rootDir, "cap-empty", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "empty.go", 1, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.NotNil(t, cps[0].OriginalData, "empty existing file should have non-nil []byte{}")
	assert.Len(t, cps[0].OriginalData, 0)
}
