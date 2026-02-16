// internal/runner/input_test.go
package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInputFromPromptFlag(t *testing.T) {
	text, err := ResolveInput("hello world", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", text)
}

func TestResolveInputFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	err := os.WriteFile(path, []byte("prompt from file"), 0644)
	require.NoError(t, err)

	text, err := ResolveInput("", path, nil)
	require.NoError(t, err)
	assert.Equal(t, "prompt from file", text)
}

func TestResolveInputFromStdin(t *testing.T) {
	reader := strings.NewReader("piped input")
	text, err := ResolveInput("", "", reader)
	require.NoError(t, err)
	assert.Equal(t, "piped input", text)
}

func TestResolveInputPromptTakesPrecedence(t *testing.T) {
	reader := strings.NewReader("stdin")
	text, err := ResolveInput("flag wins", "", reader)
	require.NoError(t, err)
	assert.Equal(t, "flag wins", text)
}

func TestResolveInputFileTakesPrecedenceOverStdin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	err := os.WriteFile(path, []byte("file wins"), 0644)
	require.NoError(t, err)

	reader := strings.NewReader("stdin")
	text, err := ResolveInput("", path, reader)
	require.NoError(t, err)
	assert.Equal(t, "file wins", text)
}

func TestResolveInputNoInput(t *testing.T) {
	_, err := ResolveInput("", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input")
}

func TestResolveInputFileMissing(t *testing.T) {
	_, err := ResolveInput("", "/nonexistent/path.txt", nil)
	require.Error(t, err)
}

func TestResolveInputEmptyPrompt(t *testing.T) {
	_, err := ResolveInput("   ", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input")
}
