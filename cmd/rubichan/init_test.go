package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmdGeneratesAgentMD(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644))

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir})
	err := cmd.Execute()
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENT.md")
	assert.Contains(t, string(content), "Go")
}

func TestInitCmdCreatesAgentDir(t *testing.T) {
	dir := t.TempDir()

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir})
	require.NoError(t, cmd.Execute())

	assert.DirExists(t, filepath.Join(dir, ".agent", "skills"))
	assert.DirExists(t, filepath.Join(dir, ".agent", "hooks"))
}

func TestInitCmdRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("existing"), 0o644))

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestInitCmdForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("old"), 0o644))

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir, "--force"})
	require.NoError(t, cmd.Execute())

	content, err := os.ReadFile(filepath.Join(dir, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENT.md")
}

func TestInitCmdHooksOnly(t *testing.T) {
	dir := t.TempDir()

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir, "--hooks-only"})
	require.NoError(t, cmd.Execute())

	_, err := os.Stat(filepath.Join(dir, "AGENT.md"))
	assert.True(t, os.IsNotExist(err))
}
