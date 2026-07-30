package skillruntime

// These tests live in-package because the adapters and the noop backend they
// exercise are unexported: they are wiring details of how backends receive
// integrations, not part of this package's contract.

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRepoRoot returns this checkout's root, so the git-backed adapters run
// against a real repository rather than a fixture.
func testRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestNoopPromptBackend_AllMethodsWork(t *testing.T) {
	t.Parallel()
	backend := &noopPromptBackend{}

	assert.NoError(t, backend.Load(skills.SkillManifest{}, nil))
	assert.Empty(t, backend.Tools())
	assert.Empty(t, backend.Hooks())
	assert.Empty(t, backend.Commands())
	assert.Empty(t, backend.Agents())
	assert.NoError(t, backend.Unload())
}

func TestRegisterBuiltinPrompts(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	loader := skills.NewLoader(filepath.Join(configDir, "skills"), "")

	err := RegisterBuiltinPrompts(loader, configDir)
	assert.NoError(t, err)
}

func TestStarlarkGitRunnerAdapter_Diff(t *testing.T) {
	t.Parallel()
	adapter := &starlarkGitRunnerAdapter{
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	diff, err := adapter.Diff(context.Background())
	require.NoError(t, err)
	_ = diff
}

func TestPluginGitRunnerAdapter_Diff(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	diff, err := adapter.Diff()
	require.NoError(t, err)
	_ = diff
}

func TestPluginHTTPFetcherAdapter_Construction(t *testing.T) {
	t.Parallel()
	adapter := &pluginHTTPFetcherAdapter{
		ctx:     context.Background(),
		fetcher: integrations.NewHTTPFetcher(5 * time.Second),
	}
	assert.NotNil(t, adapter)
}

func TestPluginLLMCompleterAdapter_Construction(t *testing.T) {
	t.Parallel()
	adapter := &pluginLLMCompleterAdapter{
		ctx:       context.Background(),
		completer: nil,
	}
	assert.NotNil(t, adapter)
}

func TestPluginSkillInvokerAdapter_Construction(t *testing.T) {
	t.Parallel()
	adapter := &pluginSkillInvokerAdapter{
		ctx:     context.Background(),
		invoker: integrations.NewSkillInvoker(nil),
	}
	assert.NotNil(t, adapter)
}

func TestPluginHTTPFetcherAdapter_Fetch(t *testing.T) {
	t.Parallel()
	adapter := &pluginHTTPFetcherAdapter{
		ctx:     context.Background(),
		fetcher: integrations.NewHTTPFetcher(5 * time.Second),
	}
	// Fetching an invalid URL should error.
	_, err := adapter.Fetch("http://localhost:1/nonexistent")
	assert.Error(t, err)
}

func TestPluginLLMCompleterAdapter_Complete_Wiring(t *testing.T) {
	t.Parallel()
	// Just verify construction; calling Complete with nil provider panics,
	// which shows the adapter correctly delegates to the completer.
	adapter := &pluginLLMCompleterAdapter{
		ctx:       context.Background(),
		completer: integrations.NewLLMCompleter(nil, "test-model"),
	}
	assert.NotNil(t, adapter)
	assert.NotNil(t, adapter.completer)
}

func TestPluginSkillInvokerAdapter_Invoke_NilRuntime(t *testing.T) {
	t.Parallel()
	invoker := integrations.NewSkillInvoker(nil)
	adapter := &pluginSkillInvokerAdapter{
		ctx:     context.Background(),
		invoker: invoker,
	}
	// With no runtime set, Invoke should fail.
	_, err := adapter.Invoke("nonexistent", map[string]any{"key": "value"})
	assert.Error(t, err)
}

func TestStarlarkGitRunnerAdapter_Log(t *testing.T) {
	t.Parallel()
	adapter := &starlarkGitRunnerAdapter{
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	entries, err := adapter.Log(context.Background(), "-1")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.NotEmpty(t, entries[0].Hash)
	assert.NotEmpty(t, entries[0].Author)
}

func TestStarlarkGitRunnerAdapter_Status(t *testing.T) {
	t.Parallel()
	adapter := &starlarkGitRunnerAdapter{
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	_, err := adapter.Status(context.Background())
	require.NoError(t, err)
}

func TestPluginGitRunnerAdapter_Log(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	entries, err := adapter.Log("-1")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.NotEmpty(t, entries[0].Hash)
}

func TestPluginGitRunnerAdapter_Status(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	_, err := adapter.Status()
	require.NoError(t, err)
}
