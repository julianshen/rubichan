package hooks_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHooksTOMLBasic(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "setup"
command = "go mod download"
timeout = "120s"
description = "Install dependencies"

[[hooks]]
event = "pre_tool"
command = "python3 guard.py"
match_tool = "shell"
timeout = "5s"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	require.Len(t, configs, 2)

	assert.Equal(t, "setup", configs[0].Event)
	assert.Equal(t, "go mod download", configs[0].Command)
	assert.Equal(t, 120*time.Second, configs[0].Timeout)
	assert.Equal(t, "Install dependencies", configs[0].Description)
	assert.Equal(t, ".agent/hooks.toml", configs[0].Source)

	assert.Equal(t, "pre_tool", configs[1].Event)
	assert.Equal(t, "shell", configs[1].Pattern)
	assert.Equal(t, 5*time.Second, configs[1].Timeout)
}

func TestLoadHooksTOMLMissingFile(t *testing.T) {
	dir := t.TempDir()
	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	assert.Empty(t, configs)
}

func TestLoadHooksTOMLInvalidSyntax(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte("not valid toml [[["), 0o644))

	_, err := hooks.LoadHooksTOML(dir)
	assert.Error(t, err)
}

func TestLoadHooksTOMLDefaultTimeout(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "post_tool"
command = "echo done"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, 30*time.Second, configs[0].Timeout, "should default to 30s")
}
