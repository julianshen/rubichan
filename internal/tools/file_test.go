package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileToolReadFile(t *testing.T) {
	dir := t.TempDir()
	content := "hello, world\nsecond line"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644))

	ft := NewFileTool(dir)
	assert.Equal(t, "file", ft.Name())
	assert.NotEmpty(t, ft.Description())
	assert.NotNil(t, ft.InputSchema())

	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "test.txt",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, content, result.Content)
}

func TestFileToolWriteFile(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "subdir/output.txt",
		"content":   "written content",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "subdir/output.txt")

	// Verify file was actually written
	data, err := os.ReadFile(filepath.Join(dir, "subdir", "output.txt"))
	require.NoError(t, err)
	assert.Equal(t, "written content", string(data))
}

func TestFileToolPatch(t *testing.T) {
	dir := t.TempDir()
	original := "line one\nline two\nline three"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "patch.txt"), []byte(original), 0644))

	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation":  "patch",
		"path":       "patch.txt",
		"old_string": "line two",
		"new_string": "line TWO replaced",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Verify the patch was applied
	data, err := os.ReadFile(filepath.Join(dir, "patch.txt"))
	require.NoError(t, err)
	assert.Equal(t, "line one\nline TWO replaced\nline three", string(data))
}

func TestFileToolPatchOldStringNotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "patch.txt"), []byte("some content"), 0644))

	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation":  "patch",
		"path":       "patch.txt",
		"old_string": "nonexistent text",
		"new_string": "replacement",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "old_string not found")
}

func TestFileToolPathTraversal(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	tests := []struct {
		name string
		path string
	}{
		{"dot-dot", "../etc/passwd"},
		{"absolute", "/etc/passwd"},
		{"nested-dot-dot", "subdir/../../etc/passwd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, _ := json.Marshal(map[string]string{
				"operation": "read",
				"path":      tc.path,
			})
			result, err := ft.Execute(context.Background(), input)
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Content, "path traversal")
		})
	}
}

func TestFileToolSymlinkTraversal(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("top secret"), 0644))

	// Create a symlink inside rootDir pointing outside it
	symlink := filepath.Join(dir, "escape")
	require.NoError(t, os.Symlink(outsideDir, symlink))

	ft := NewFileTool(dir)

	// Reading through the symlink should be denied
	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "escape/secret.txt",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path traversal")
}

func TestFileToolSymlinkTraversalFileLink(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("top secret"), 0644))

	// Create a file symlink inside rootDir pointing to a file outside it
	symlink := filepath.Join(dir, "linked.txt")
	require.NoError(t, os.Symlink(secretFile, symlink))

	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "linked.txt",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path traversal")
}

func TestFileToolReadMissing(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "nonexistent.txt",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "no such file")
}

func TestFileToolUnknownOperation(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "delete",
		"path":      "test.txt",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown operation")
}

func TestFileToolInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	result, err := ft.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}

func TestFileToolPatchEmptyOldString(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation":  "patch",
		"path":       "test.txt",
		"old_string": "",
		"new_string": "replacement",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "old_string must not be empty")
}

func TestFileToolPatchNonexistentFile(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation":  "patch",
		"path":       "missing.txt",
		"old_string": "foo",
		"new_string": "bar",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "no such file")
}

func TestFileToolWriteToNewDir(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "a/b/c/deep.txt",
		"content":   "nested write",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	data, err := os.ReadFile(filepath.Join(dir, "a", "b", "c", "deep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested write", string(data))
}

func TestFileToolWriteToNonexistentNewFile(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	// Write to a file that doesn't exist yet (path resolution uses resolveNearestAncestor)
	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "brand_new.txt",
		"content":   "created",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	data, err := os.ReadFile(filepath.Join(dir, "brand_new.txt"))
	require.NoError(t, err)
	assert.Equal(t, "created", string(data))
}

func TestFileToolSymlinkWriteTraversal(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a symlink dir inside rootDir pointing outside
	symlink := filepath.Join(dir, "escape")
	require.NoError(t, os.Symlink(outsideDir, symlink))

	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "escape/evil.txt",
		"content":   "should be denied",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path traversal")
}
