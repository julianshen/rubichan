package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOMLHooksRegisterAndFire(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "setup"
command = "echo toml-setup"
timeout = "5s"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	require.Len(t, configs, 1)

	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner(configs, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnSetup,
		Ctx:   context.Background(),
		Data:  map[string]any{"mode": "init"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestTOMLAndAgentMDHooksMerge(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "session_start"
command = "echo from-toml"
timeout = "5s"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	tomlConfigs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)

	agentMDConfigs := []hooks.UserHookConfig{
		{Event: "session_start", Command: "echo from-agentmd", Timeout: 5 * time.Second, Source: "AGENT.md"},
	}

	merged := append(agentMDConfigs, tomlConfigs...)

	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner(merged, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnConversationStart,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	_ = result
}
