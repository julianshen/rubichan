package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePath_ValidRelative(t *testing.T) {
	root := t.TempDir()
	got, err := validatePath(root, "src/main.swift")
	require.NoError(t, err)
	assert.Contains(t, got, "src/main.swift")
}

func TestValidatePath_DotDotWithinRoot(t *testing.T) {
	root := t.TempDir()
	got, err := validatePath(root, "src/../other/file.go")
	require.NoError(t, err)
	assert.Contains(t, got, "other/file.go")
}

func TestValidatePath_DotDotEscapesRoot(t *testing.T) {
	root := t.TempDir()
	_, err := validatePath(root, "../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes project directory")
}

func TestValidatePath_AbsolutePathRejected(t *testing.T) {
	root := t.TempDir()
	_, err := validatePath(root, "/etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute paths are not allowed")
}

func TestValidatePath_EmptyPath(t *testing.T) {
	root := t.TempDir()
	got, err := validatePath(root, "")
	require.NoError(t, err)
	assert.Equal(t, root, got)
}

func TestValidatePath_JustDotDot(t *testing.T) {
	root := t.TempDir()
	_, err := validatePath(root, "..")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes project directory")
}
