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
