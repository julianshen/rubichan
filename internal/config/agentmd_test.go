package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAgentMD_FileExists(t *testing.T) {
	dir := t.TempDir()
	content := "## Project Rules\n\nUse TDD always.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o644))

	result, err := LoadAgentMD(dir)
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestLoadAgentMD_FileMissing(t *testing.T) {
	dir := t.TempDir()

	result, err := LoadAgentMD(dir)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestLoadAgentMD_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(""), 0o644))

	result, err := LoadAgentMD(dir)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestLoadIdentityMD_FileExists(t *testing.T) {
	dir := t.TempDir()
	content := "# Identity\nRuby\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte(content), 0o644))

	result, err := LoadIdentityMD(dir)
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestLoadIdentityMD_FileMissing(t *testing.T) {
	dir := t.TempDir()

	result, err := LoadIdentityMD(dir)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestLoadSoulMD_FileExists(t *testing.T) {
	dir := t.TempDir()
	content := "# Soul\nBe useful.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(content), 0o644))

	result, err := LoadSoulMD(dir)
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestLoadSoulMD_FileMissing(t *testing.T) {
	dir := t.TempDir()

	result, err := LoadSoulMD(dir)
	require.NoError(t, err)
	assert.Empty(t, result)
}
