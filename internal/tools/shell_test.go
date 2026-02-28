package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellToolExecute(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	assert.Equal(t, "shell", st.Name())
	assert.NotEmpty(t, st.Description())
	assert.NotNil(t, st.InputSchema())

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello world",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "hello world\n", result.Content)
}

func TestShellToolTimeout(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 100*time.Millisecond)

	input, _ := json.Marshal(map[string]string{
		"command": "sleep 10",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")
}

func TestShellToolExitCode(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo error output >&2; exit 1",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "error output")
}

func TestShellToolOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 30KB (30 * 1024 = 30720 bytes)
	// Use printf to generate a known large output
	input, _ := json.Marshal(map[string]string{
		"command": "dd if=/dev/zero bs=1024 count=40 2>/dev/null | tr '\\0' 'A'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Output should be truncated to maxOutputBytes
	assert.LessOrEqual(t, len(result.Content), 30*1024+100) // some slack for truncation message
	assert.True(t, strings.Contains(result.Content, "truncated"))
}

func TestShellToolLargeOutputSetsDisplayContent(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 30KB but smaller than 100KB.
	// 50KB = 50 * 1024 = 51200 bytes.
	input, _ := json.Marshal(map[string]string{
		"command": "dd if=/dev/zero bs=1024 count=50 2>/dev/null | tr '\\0' 'B'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Content should be truncated at 30KB for the LLM.
	assert.LessOrEqual(t, len(result.Content), maxOutputBytes+50)
	assert.Contains(t, result.Content, "truncated")
	// DisplayContent should have more data than Content.
	assert.NotEmpty(t, result.DisplayContent)
	assert.Greater(t, len(result.DisplayContent), len(result.Content))
}

func TestShellToolHugeOutputCapsDisplayContent(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 100KB (maxDisplayBytes).
	// 120KB = 120 * 1024 = 122880 bytes.
	input, _ := json.Marshal(map[string]string{
		"command": "dd if=/dev/zero bs=1024 count=120 2>/dev/null | tr '\\0' 'C'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Content should be truncated at 30KB.
	assert.LessOrEqual(t, len(result.Content), maxOutputBytes+50)
	assert.Contains(t, result.Content, "truncated")
	// DisplayContent should be truncated at 100KB.
	assert.NotEmpty(t, result.DisplayContent)
	assert.LessOrEqual(t, len(result.DisplayContent), maxDisplayBytes+50)
	assert.Contains(t, result.DisplayContent, "truncated")
}

func TestShellToolSmallOutputNoDisplayContent(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Small output should not set DisplayContent (no redundancy).
	assert.Empty(t, result.DisplayContent)
}

func TestShellToolInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	result, err := st.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}
