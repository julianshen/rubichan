package config

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestLoadIdentityMD_RejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	require.NoError(t, os.WriteFile(target, []byte("secret"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "IDENTITY.md")))

	result, err := LoadIdentityMD(dir)
	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "loadOptionalMarkdown")
	assert.Contains(t, err.Error(), "IDENTITY.md")
}

func TestLoadAgentMDWithHooks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("---\nhooks:\n  - event: post_edit\n    pattern: \"*.go\"\n    command: \"gofmt -w {file}\"\n  - event: pre_shell\n    command: \"echo {command}\"\n---\n\n# Project Instructions\nUse Go.\n"), 0644)

	body, hooks, err := LoadAgentMDWithHooks(dir)
	require.NoError(t, err)
	assert.Contains(t, body, "# Project Instructions")
	assert.NotContains(t, body, "hooks:")
	require.Len(t, hooks, 2)
	assert.Equal(t, "post_edit", hooks[0].Event)
	assert.Equal(t, "*.go", hooks[0].Pattern)
	assert.Equal(t, "gofmt -w {file}", hooks[0].Command)
}

func TestLoadAgentMDWithHooksNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("# Just markdown\nNo frontmatter here.\n"), 0644)

	body, hooks, err := LoadAgentMDWithHooks(dir)
	require.NoError(t, err)
	assert.Contains(t, body, "# Just markdown")
	assert.Empty(t, hooks)
}

func TestLoadAgentMDWithHooksNoFile(t *testing.T) {
	dir := t.TempDir()
	body, hooks, err := LoadAgentMDWithHooks(dir)
	require.NoError(t, err)
	assert.Empty(t, body)
	assert.Empty(t, hooks)
}

func TestLoadAgentMDStripsHookFrontmatter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("---\nhooks:\n  - event: post_edit\n    command: \"test\"\n---\n\n# Instructions\n"), 0644)

	body, err := LoadAgentMD(dir)
	require.NoError(t, err)
	assert.Contains(t, body, "# Instructions")
	assert.NotContains(t, body, "hooks:")
}
