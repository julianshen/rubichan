package skillruntime

// These tests live in-package because the adapters and the noop backend they
// exercise are unexported: they are wiring details of how backends receive
// integrations, not part of this package's contract.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRepo builds a throwaway repository with one commit. The adapters used
// to be pointed at this checkout, which made the tests fail rather than skip
// in any environment without git history — a source tarball, a minimal
// container — and made assertions like "Log returns entries" depend on the
// ambient repository rather than on anything the test set up.
func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "--quiet")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("contents\n"), 0o600))
	run("add", "file.txt")
	run("commit", "--quiet", "-m", "initial commit")
	return dir
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
		runner: integrations.NewGitRunner(testRepo(t)),
	}
	diff, err := adapter.Diff(context.Background())
	require.NoError(t, err)
	_ = diff
}

func TestPluginGitRunnerAdapter_Diff(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepo(t)),
	}
	diff, err := adapter.Diff()
	require.NoError(t, err)
	_ = diff
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
		runner: integrations.NewGitRunner(testRepo(t)),
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
		runner: integrations.NewGitRunner(testRepo(t)),
	}
	_, err := adapter.Status(context.Background())
	require.NoError(t, err)
}

func TestPluginGitRunnerAdapter_Log(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepo(t)),
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
		runner: integrations.NewGitRunner(testRepo(t)),
	}
	_, err := adapter.Status()
	require.NoError(t, err)
}

// The backend factory is the single place deciding what a skill of each kind
// can reach, so these tests pin the routing rather than the backends
// themselves: a manifest of each declared kind must produce that kind's
// backend, and an unrecognised one must be refused rather than silently
// treated as prompt-only.

func testBackendDeps(t *testing.T) backendDeps {
	t.Helper()
	return backendDeps{
		llmCompleter:       integrations.NewLLMCompleter(nil, "test-model"),
		httpFetcher:        integrations.NewHTTPFetcher(5 * time.Second),
		skillInvoker:       integrations.NewSkillInvoker(nil),
		starlarkGitAdapter: &starlarkGitRunnerAdapter{runner: integrations.NewGitRunner(t.TempDir())},
		pluginLLMAdapter:   &pluginLLMCompleterAdapter{ctx: context.Background()},
		pluginHTTPAdapter:  &pluginHTTPFetcherAdapter{ctx: context.Background()},
		pluginGitAdapter:   &pluginGitRunnerAdapter{ctx: context.Background()},
		pluginSkillAdapter: &pluginSkillInvokerAdapter{ctx: context.Background()},
	}
}

func TestBackendFactoryRoutesPromptOnlySkillsToTheNoopBackend(t *testing.T) {
	t.Parallel()

	factory := newBackendFactory(context.Background(), testBackendDeps(t))
	backend, err := factory(skills.SkillManifest{Name: "prompt-skill"}, t.TempDir())
	require.NoError(t, err)
	assert.IsType(t, &noopPromptBackend{}, backend,
		"a skill with no implementation still has to load through the normal flow")
}

func TestBackendFactoryRoutesStarlark(t *testing.T) {
	t.Parallel()

	manifest := skills.SkillManifest{Name: "star-skill"}
	manifest.Implementation.Backend = skills.BackendStarlark

	factory := newBackendFactory(context.Background(), testBackendDeps(t))
	backend, err := factory(manifest, t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, backend)
	assert.NotEqual(t, "*skillruntime.noopPromptBackend", fmt.Sprintf("%T", backend))
}

func TestBackendFactoryRoutesPlugin(t *testing.T) {
	t.Parallel()

	manifest := skills.SkillManifest{Name: "plugin-skill"}
	manifest.Implementation.Backend = skills.BackendPlugin

	factory := newBackendFactory(context.Background(), testBackendDeps(t))
	backend, err := factory(manifest, t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, backend)
}

func TestBackendFactoryRoutesProcess(t *testing.T) {
	t.Parallel()

	manifest := skills.SkillManifest{Name: "process-skill"}
	manifest.Implementation.Backend = skills.BackendProcess

	factory := newBackendFactory(context.Background(), testBackendDeps(t))
	backend, err := factory(manifest, t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, backend)
}

// TestBackendFactoryRejectsAnUnknownBackend guards the default arm. Falling
// through to the noop backend would make a misspelled backend look like a
// prompt-only skill that simply does nothing.
func TestBackendFactoryRejectsAnUnknownBackend(t *testing.T) {
	t.Parallel()

	manifest := skills.SkillManifest{Name: "odd-skill"}
	manifest.Implementation.Backend = "wasm"

	factory := newBackendFactory(context.Background(), testBackendDeps(t))
	backend, err := factory(manifest, t.TempDir())
	require.Error(t, err)
	assert.Nil(t, backend)
	assert.Contains(t, err.Error(), `backend "wasm" not implemented`)
}

// stubProvider is the smallest thing satisfying provider.LLMProvider, so the
// plugin LLM adapter can be exercised without a network call.
type stubProvider struct{ text string }

func (p *stubProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: p.text}
	ch <- provider.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func TestPluginLLMCompleterAdapterCompletesThroughTheProvider(t *testing.T) {
	t.Parallel()

	adapter := &pluginLLMCompleterAdapter{
		ctx:       context.Background(),
		completer: integrations.NewLLMCompleter(&stubProvider{text: "hello"}, "test-model"),
	}

	got, err := adapter.Complete("say hello")
	require.NoError(t, err)
	assert.Contains(t, got, "hello", "the adapter must return what the model said, not swallow it")
}

func TestNoopPromptBackendHasNoWorkflows(t *testing.T) {
	t.Parallel()
	assert.Empty(t, (&noopPromptBackend{}).Workflows())
}

// The git adapters wrap a runner that fails outside a repository. These cover
// the error arms, which otherwise only run when someone points rubichan at a
// directory git does not track.

func TestGitAdaptersPropagateFailureOutsideARepository(t *testing.T) {
	t.Parallel()

	notARepo := t.TempDir()
	star := &starlarkGitRunnerAdapter{runner: integrations.NewGitRunner(notARepo)}
	plugin := &pluginGitRunnerAdapter{ctx: context.Background(), runner: integrations.NewGitRunner(notARepo)}

	_, err := star.Log(context.Background(), "-1")
	assert.Error(t, err, "starlark Log outside a repository")
	_, err = star.Status(context.Background())
	assert.Error(t, err, "starlark Status outside a repository")
	_, err = plugin.Log("-1")
	assert.Error(t, err, "plugin Log outside a repository")
	_, err = plugin.Status()
	assert.Error(t, err, "plugin Status outside a repository")
}

func TestBackendFactoryRoutesMCP(t *testing.T) {
	t.Parallel()

	manifest := skills.SkillManifest{Name: "mcp-example"}
	manifest.Implementation.Backend = skills.BackendMCP
	manifest.Implementation.MCPTransport = "stdio"
	manifest.Implementation.MCPCommand = "true"

	factory := newBackendFactory(context.Background(), testBackendDeps(t))
	backend, err := factory(manifest, t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, backend)
}
