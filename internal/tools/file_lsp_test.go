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

// mockLSPNotifier records calls to NotifyAndCollectDiagnostics.
type mockLSPNotifier struct {
	called   bool
	filePath string
	content  []byte
	diags    []string
	err      error
}

func (m *mockLSPNotifier) NotifyAndCollectDiagnostics(_ context.Context, filePath string, content []byte) ([]string, error) {
	m.called = true
	m.filePath = filePath
	m.content = content
	return m.diags, m.err
}

func TestFileToolLSPNotifierCalledOnWrite(t *testing.T) {
	dir := t.TempDir()
	mock := &mockLSPNotifier{
		diags: []string{"  main.go:1:1: error: undefined variable"},
	}

	ft := NewFileTool(dir)
	ft.SetLSPNotifier(mock)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "main.go",
		"content":   "package main\n",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, mock.called)
	assert.Equal(t, []byte("package main\n"), mock.content)
	assert.Contains(t, result.Content, "LSP diagnostics")
	assert.Contains(t, result.Content, "undefined variable")
}

func TestFileToolLSPNotifierCalledOnPatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc old() {}\n"), 0644))

	mock := &mockLSPNotifier{
		diags: []string{"  main.go:2:6: error: missing return"},
	}

	ft := NewFileTool(dir)
	ft.SetLSPNotifier(mock)

	input, _ := json.Marshal(map[string]string{
		"operation":  "patch",
		"path":       "main.go",
		"old_string": "func old() {}",
		"new_string": "func new() int {}",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, mock.called)
	assert.Equal(t, []byte("package main\nfunc new() int {}\n"), mock.content)
	assert.Contains(t, result.Content, "LSP diagnostics")
	assert.Contains(t, result.Content, "missing return")
}

func TestFileToolLSPNotifierNilSafe(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)
	// No notifier set — should not panic.

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "test.txt",
		"content":   "hello",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.NotContains(t, result.Content, "LSP")
}

func TestFileToolLSPNotifierNoDiagnostics(t *testing.T) {
	dir := t.TempDir()
	mock := &mockLSPNotifier{diags: nil}

	ft := NewFileTool(dir)
	ft.SetLSPNotifier(mock)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "clean.go",
		"content":   "package main\n",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, mock.called)
	assert.NotContains(t, result.Content, "LSP diagnostics")
}

func TestFileToolLSPNotifierErrorIgnored(t *testing.T) {
	dir := t.TempDir()
	mock := &mockLSPNotifier{
		err:   assert.AnError,
		diags: []string{"should not appear"},
	}

	ft := NewFileTool(dir)
	ft.SetLSPNotifier(mock)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "test.go",
		"content":   "package main\n",
	})
	result, err := ft.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, mock.called)
	// When notifier returns an error, diagnostics are not appended.
	assert.NotContains(t, result.Content, "LSP diagnostics")
}
