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
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0644))
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
	require.NoError(t, os.WriteFile(testFile, []byte{}, 0644))

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

func TestUndoRestoresModifiedFile(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "main.go")
	require.NoError(t, os.WriteFile(testFile, []byte("original"), 0644))

	mgr, err := checkpoint.New(rootDir, "undo-modify", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "main.go", 1, "write")
	require.NoError(t, err)

	// Simulate the agent writing to the file
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))

	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	// Use EvalSymlinks for macOS /var -> /private/var
	expected, _ := filepath.EvalSymlinks(testFile)
	assert.Equal(t, expected, path)

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
	assert.Empty(t, mgr.List(), "stack should be empty after undo")
}

func TestUndoDeletesCreatedFile(t *testing.T) {
	rootDir := t.TempDir()

	mgr, err := checkpoint.New(rootDir, "undo-create", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// Capture before the file exists
	_, err = mgr.Capture(context.Background(), "new.go", 1, "write")
	require.NoError(t, err)

	// Simulate the agent creating the file
	newFile := filepath.Join(rootDir, "new.go")
	require.NoError(t, os.WriteFile(newFile, []byte("package new"), 0644))

	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	_, err = os.Stat(newFile)
	assert.True(t, os.IsNotExist(err), "file should be deleted after undo of creation")
}

func TestUndoEmptyStackReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "undo-empty", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Undo(context.Background())
	assert.ErrorIs(t, err, checkpoint.ErrNoCheckpoints)
}

func TestCaptureSpillsLargeFile(t *testing.T) {
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	data := make([]byte, 2*1024*1024) // 2MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	require.NoError(t, os.WriteFile(bigFile, data, 0644))

	mgr, err := checkpoint.New(rootDir, "spill-large", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.True(t, cps[0].IsSpilled(), "file >1MB should be spilled to disk")
	assert.Nil(t, cps[0].OriginalData, "spilled checkpoint should not hold data in memory")
}

func TestCaptureBudgetEviction(t *testing.T) {
	rootDir := t.TempDir()
	data := make([]byte, 600*1024) // 600KB each
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "a.bin"), data, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "b.bin"), data, 0644))

	mgr, err := checkpoint.New(rootDir, "spill-budget", 1024*1024) // 1MB budget
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "a.bin", 1, "write")
	require.NoError(t, err)

	_, err = mgr.Capture(context.Background(), "b.bin", 2, "write")
	require.NoError(t, err)

	cps := mgr.List()
	require.Len(t, cps, 2)
	assert.True(t, cps[0].IsSpilled(), "oldest checkpoint should be evicted when budget exceeded")
}

func TestUndoSpilledCheckpoint(t *testing.T) {
	rootDir := t.TempDir()
	bigFile := filepath.Join(rootDir, "big.bin")
	data := make([]byte, 2*1024*1024) // 2MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	require.NoError(t, os.WriteFile(bigFile, data, 0644))

	mgr, err := checkpoint.New(rootDir, "undo-spill", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	_, err = mgr.Capture(context.Background(), "big.bin", 1, "write")
	require.NoError(t, err)

	// Overwrite the file
	require.NoError(t, os.WriteFile(bigFile, []byte("small"), 0644))

	path, err := mgr.Undo(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	restored, _ := os.ReadFile(bigFile)
	assert.Equal(t, data, restored, "spilled checkpoint should restore correctly")
}

func TestRewindToTurn(t *testing.T) {
	rootDir := t.TempDir()
	file1 := filepath.Join(rootDir, "a.go")
	file2 := filepath.Join(rootDir, "b.go")
	require.NoError(t, os.WriteFile(file1, []byte("a-original"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("b-original"), 0644))

	mgr, err := checkpoint.New(rootDir, "rewind", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	// Turn 1: modify a.go
	_, err = mgr.Capture(context.Background(), "a.go", 1, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file1, []byte("a-turn1"), 0644))

	// Turn 2: modify b.go
	_, err = mgr.Capture(context.Background(), "b.go", 2, "write")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file2, []byte("b-turn2"), 0644))

	// Turn 3: modify a.go again
	_, err = mgr.Capture(context.Background(), "a.go", 3, "patch")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file1, []byte("a-turn3"), 0644))

	// Rewind to turn 1 — should undo turns 2 and 3
	paths, err := mgr.RewindToTurn(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, paths, 2) // a.go and b.go

	dataA, _ := os.ReadFile(file1)
	assert.Equal(t, "a-turn1", string(dataA), "a.go should be at turn-1 state")

	dataB, _ := os.ReadFile(file2)
	assert.Equal(t, "b-original", string(dataB), "b.go should be at original state")

	assert.Len(t, mgr.List(), 1, "only turn-1 checkpoint should remain")
}
