package goplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/skillsdk"
)

// --- Mocks ---

// mockPermissionChecker is a test double for skills.PermissionChecker.
type mockPermissionChecker struct {
	allowAll bool
}

func (m *mockPermissionChecker) CheckPermission(_ skills.Permission) error {
	if m.allowAll {
		return nil
	}
	return fmt.Errorf("permission denied")
}

func (m *mockPermissionChecker) CheckRateLimit(_ string) error { return nil }
func (m *mockPermissionChecker) ResetTurnLimits()              {}

// fakePlugin implements skillsdk.SkillPlugin for testing without real .so files.
type fakePlugin struct {
	activated   bool
	deactivated bool
	lastCtx     skillsdk.Context
}

func (fp *fakePlugin) Manifest() skillsdk.Manifest {
	return skillsdk.Manifest{
		Name:        "fake-plugin",
		Version:     "1.0.0",
		Description: "A fake plugin for testing",
		Author:      "test",
		License:     "MIT",
	}
}

func (fp *fakePlugin) Activate(ctx skillsdk.Context) error {
	fp.activated = true
	fp.lastCtx = ctx
	return nil
}

func (fp *fakePlugin) Deactivate(ctx skillsdk.Context) error {
	fp.deactivated = true
	fp.lastCtx = ctx
	return nil
}

// fakePluginWithTools registers a tool during Activate.
type fakePluginWithTools struct {
	fakePlugin
	toolRegistrar ToolRegistrar
}

func (fp *fakePluginWithTools) Activate(ctx skillsdk.Context) error {
	fp.activated = true
	fp.lastCtx = ctx
	if fp.toolRegistrar != nil {
		fp.toolRegistrar.RegisterTool(&fakeTool{
			name:        "test-tool",
			description: "A test tool from plugin",
		})
	}
	return nil
}

// fakePluginWithHooks registers a hook during Activate.
type fakePluginWithHooks struct {
	fakePlugin
	hookRegistrar HookRegistrar
}

func (fp *fakePluginWithHooks) Activate(ctx skillsdk.Context) error {
	fp.activated = true
	fp.lastCtx = ctx
	if fp.hookRegistrar != nil {
		fp.hookRegistrar.RegisterHook(skills.HookOnBeforeToolCall, func(event skills.HookEvent) (skills.HookResult, error) {
			return skills.HookResult{Modified: map[string]any{"handled": true}}, nil
		})
	}
	return nil
}

// fakeTool implements tools.Tool for testing.
type fakeTool struct {
	name        string
	description string
}

func (ft *fakeTool) Name() string                 { return ft.name }
func (ft *fakeTool) Description() string          { return ft.description }
func (ft *fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (ft *fakeTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

// mockPluginLoader implements PluginLoader for testing the happy path.
type mockPluginLoader struct {
	plugin skillsdk.SkillPlugin
	err    error
}

func (m *mockPluginLoader) Load(_ string) (skillsdk.SkillPlugin, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.plugin, nil
}

// --- Helper ---

func newTestManifest() skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Description: "A test plugin",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendPlugin,
			Entrypoint: "test.so",
		},
		Permissions: []skills.Permission{skills.PermFileRead},
	}
}

// --- Tests ---

func TestLoadPlugin(t *testing.T) {
	fp := &fakePlugin{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))

	// Load should succeed and the backend should implement SkillBackend.
	var _ skills.SkillBackend = backend
	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	// The plugin should have been activated.
	assert.True(t, fp.activated, "plugin Activate should have been called")

	// The context passed to Activate should not be nil.
	assert.NotNil(t, fp.lastCtx, "plugin should receive a non-nil Context")
}

func TestPluginTools(t *testing.T) {
	ft := &fakeTool{name: "test-tool", description: "A test tool from plugin"}
	fp := &fakePluginWithTools{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))

	// Before loading, no tools registered.
	assert.Empty(t, backend.Tools())

	// Wire the tool registrar so the fake plugin can register a tool on Activate.
	fp.toolRegistrar = backend

	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	// After loading, the plugin should have registered one tool.
	registeredTools := backend.Tools()
	require.Len(t, registeredTools, 1)
	assert.Equal(t, ft.name, registeredTools[0].Name())
	assert.Equal(t, ft.description, registeredTools[0].Description())
}

func TestPluginActivateDeactivate(t *testing.T) {
	fp := &fakePlugin{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))

	// Load activates the plugin.
	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)
	assert.True(t, fp.activated, "Activate should be called during Load")
	assert.False(t, fp.deactivated, "Deactivate should not be called during Load")

	// Unload deactivates the plugin.
	err = backend.Unload()
	require.NoError(t, err)
	assert.True(t, fp.deactivated, "Deactivate should be called during Unload")
}

func TestLoadPluginInvalidPath(t *testing.T) {
	// Use the real system plugin loader -- loading a non-existent .so
	// should produce an error without needing a real .so file.
	backend := NewGoPluginBackend(WithPluginLoader(&systemPluginLoader{}))

	manifest := newTestManifest()
	manifest.Implementation.Entrypoint = "/nonexistent/path/plugin.so"

	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(manifest, checker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load plugin")
}

func TestLoadPluginMissingSymbol(t *testing.T) {
	// A loader that returns an error simulating a missing NewSkill symbol.
	loader := &mockPluginLoader{
		err: fmt.Errorf("plugin does not export symbol \"NewSkill\""),
	}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))

	err := backend.Load(newTestManifest(), checker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NewSkill")
}

func TestPluginHooks(t *testing.T) {
	fp := &fakePluginWithHooks{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))

	// Before loading, no hooks.
	assert.Empty(t, backend.Hooks())

	// Wire the hook registrar.
	fp.hookRegistrar = backend

	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	// After loading, should have one hook registered.
	hooks := backend.Hooks()
	require.Contains(t, hooks, skills.HookOnBeforeToolCall)

	// Invoke the hook and verify it works.
	result, err := hooks[skills.HookOnBeforeToolCall](skills.HookEvent{
		Phase:     skills.HookOnBeforeToolCall,
		SkillName: "test-plugin",
		Data:      map[string]any{},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"handled": true}, result.Modified)
}

func TestPluginContextBridgesPermissions(t *testing.T) {
	fp := &fakePlugin{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: false} // deny all

	backend := NewGoPluginBackend(WithPluginLoader(loader))
	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	// The context passed to the plugin should enforce permissions.
	ctx := fp.lastCtx
	require.NotNil(t, ctx)

	// ReadFile should fail because permission is denied.
	_, err = ctx.ReadFile("/some/file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestPluginContextAllowsWithPermission(t *testing.T) {
	fp := &fakePlugin{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	// Create a temp file to read.
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.txt"
	require.NoError(t, writeTestFile(tmpFile, "hello"))

	// Set skillDir to tmpDir so sandboxed file operations work.
	backend := NewGoPluginBackend(WithPluginLoader(loader), WithSkillDir(tmpDir))

	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	ctx := fp.lastCtx
	require.NotNil(t, ctx)

	// ReadFile should succeed with permissions allowed.
	content, err := ctx.ReadFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "hello", content)
}

func TestUnloadWithoutLoad(t *testing.T) {
	backend := NewGoPluginBackend()
	// Unloading without loading should not panic and return no error.
	err := backend.Unload()
	require.NoError(t, err)
}

func TestPluginContextProjectRoot(t *testing.T) {
	fp := &fakePlugin{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader), WithSkillDir("/some/skill/dir"))
	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	ctx := fp.lastCtx
	require.NotNil(t, ctx)
	assert.Equal(t, "/some/skill/dir", ctx.ProjectRoot())
}

func TestLoadPluginEmptyEntrypoint(t *testing.T) {
	fp := &fakePlugin{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))

	manifest := newTestManifest()
	manifest.Implementation.Entrypoint = ""

	err := backend.Load(manifest, checker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entrypoint is required")
}

func TestPluginActivateError(t *testing.T) {
	// Plugin that fails on Activate.
	fp := &fakePluginActivateError{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))
	err := backend.Load(newTestManifest(), checker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "activate plugin")
	assert.Contains(t, err.Error(), "activation failed")
}

func TestPluginDeactivateError(t *testing.T) {
	// Plugin that fails on Deactivate.
	fp := &fakePluginDeactivateError{}
	loader := &mockPluginLoader{plugin: fp}
	checker := &mockPermissionChecker{allowAll: true}

	backend := NewGoPluginBackend(WithPluginLoader(loader))
	err := backend.Load(newTestManifest(), checker)
	require.NoError(t, err)

	err = backend.Unload()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deactivate plugin")
}

// --- pluginContext method tests ---

func TestPluginContextWriteFile(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, tmpDir)

		path := tmpDir + "/out.txt"
		err := ctx.WriteFile(path, "hello write")
		require.NoError(t, err)

		content, err := ctx.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "hello write", content)
	})

	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		err := ctx.WriteFile("/tmp/x.txt", "no")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})
}

func TestPluginContextListDir(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, writeFileHelper(tmpDir+"/a.txt", "a"))
		require.NoError(t, writeFileHelper(tmpDir+"/b.txt", "b"))

		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, tmpDir)
		entries, err := ctx.ListDir(tmpDir)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		// Entries should contain a.txt and b.txt.
		names := []string{entries[0].Name, entries[1].Name}
		assert.Contains(t, names, "a.txt")
		assert.Contains(t, names, "b.txt")
		assert.False(t, entries[0].IsDir)
	})

	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.ListDir("/tmp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("nonexistent", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.ListDir("/nonexistent/dir/xyz")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ListDir")
	})
}

func TestPluginContextSearchFiles(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, writeFileHelper(tmpDir+"/foo.txt", "foo"))
		require.NoError(t, writeFileHelper(tmpDir+"/bar.txt", "bar"))

		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, tmpDir)
		matches, err := ctx.SearchFiles(tmpDir + "/*.txt")
		require.NoError(t, err)
		assert.Len(t, matches, 2)
	})

	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.SearchFiles("*.txt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})
}

func TestPluginContextExec(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		result, err := ctx.Exec("echo", "hello")
		require.NoError(t, err)
		assert.Contains(t, result.Stdout, "hello")
		assert.Equal(t, 0, result.ExitCode)
	})

	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.Exec("echo", "hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("nonzero exit", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		result, err := ctx.Exec("sh", "-c", "exit 42")
		require.NoError(t, err)
		assert.Equal(t, 42, result.ExitCode)
	})
}

func TestPluginContextComplete(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.Complete("prompt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("not configured", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.Complete("prompt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestPluginContextFetch(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.Fetch("http://example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("not configured", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.Fetch("http://example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestPluginContextGitDiff(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.GitDiff("HEAD")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("not configured", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.GitDiff("HEAD")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestPluginContextGitLog(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.GitLog("-n", "5")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("not configured", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.GitLog("-n", "5")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestPluginContextGitStatus(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.GitStatus()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("not configured", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.GitStatus()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestPluginContextGetEnv(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		t.Setenv("TEST_GOPLUGIN_VAR", "test_value")
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		assert.Equal(t, "test_value", ctx.GetEnv("TEST_GOPLUGIN_VAR"))
	})

	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		assert.Equal(t, "", ctx.GetEnv("PATH"))
	})
}

func TestPluginContextInvokeSkill(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, "")
		_, err := ctx.InvokeSkill("other-skill", map[string]any{"key": "val"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("not configured", func(t *testing.T) {
		ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
		_, err := ctx.InvokeSkill("other-skill", map[string]any{"key": "val"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestPluginContextReadFileError(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
	_, err := ctx.ReadFile("/nonexistent/file/xyz.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ReadFile")
}

func TestPluginContextWriteFileError(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, "")
	err := ctx.WriteFile("/nonexistent/dir/xyz/file.txt", "content")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WriteFile")
}

func TestWithOptionFunctions(t *testing.T) {
	llm := &stubLLMCompleter{}
	fetcher := &stubHTTPFetcher{}
	git := &stubGitRunner{}
	invoker := &stubSkillInvoker{}

	backend := NewGoPluginBackend(
		WithLLMCompleter(llm),
		WithHTTPFetcher(fetcher),
		WithGitRunner(git),
		WithSkillInvoker(invoker),
	)

	assert.NotNil(t, backend.llmCompleter)
	assert.NotNil(t, backend.httpFetcher)
	assert.NotNil(t, backend.gitRunner)
	assert.NotNil(t, backend.skillInvoker)
}

type stubLLMCompleter struct{}

func (s *stubLLMCompleter) Complete(_ string) (string, error) { return "ok", nil }

type stubHTTPFetcher struct{}

func (s *stubHTTPFetcher) Fetch(_ string) (string, error) { return "ok", nil }

type stubGitRunner struct{}

func (s *stubGitRunner) Diff(_ ...string) (string, error)              { return "ok", nil }
func (s *stubGitRunner) Log(_ ...string) ([]skillsdk.GitCommit, error) { return nil, nil }
func (s *stubGitRunner) Status() ([]skillsdk.GitFileStatus, error)     { return nil, nil }

type stubSkillInvoker struct{}

func (s *stubSkillInvoker) Invoke(_ string, _ map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func TestPluginContextCompleteNotConfigured(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	_, err := ctx.Complete("test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginContextFetchNotConfigured(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	_, err := ctx.Fetch("http://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginContextGitDiffNotConfigured(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	_, err := ctx.GitDiff()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginContextGitLogNotConfigured(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	_, err := ctx.GitLog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginContextGitStatusNotConfigured(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	_, err := ctx.GitStatus()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginContextInvokeSkillNotConfigured(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	_, err := ctx.InvokeSkill("test", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestPluginContextResolveSandboxedPathEscape(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)

	_, err := ctx.resolveSandboxedPath("../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes skill directory")
}

func TestPluginContextResolveSandboxedPathAbsOutside(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)

	_, err := ctx.resolveSandboxedPath("/etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes skill directory")
}

func TestPluginContextReadFileSandboxEscape(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)

	_, err := ctx.ReadFile("../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ReadFile")
}

func TestPluginContextListDirNotFound(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)

	_, err := ctx.ListDir("nonexistent-subdir")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ListDir")
}

func TestPluginContextSearchFilesNoMatch(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)

	results, err := ctx.SearchFiles("*.xyz")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestPluginContextGetEnvDenied(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: false}, t.TempDir())
	result := ctx.GetEnv("HOME")
	assert.Equal(t, "", result)
}

func TestPluginContextCompleteWithProvider(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	ctx.llmCompleter = &stubLLMCompleter{}
	result, err := ctx.Complete("test prompt")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
}

func TestPluginContextFetchWithProvider(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	ctx.httpFetcher = &stubHTTPFetcher{}
	result, err := ctx.Fetch("http://example.com")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
}

func TestPluginContextGitDiffWithProvider(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	ctx.gitRunner = &stubGitRunner{}
	result, err := ctx.GitDiff("HEAD")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
}

func TestPluginContextGitLogWithProvider(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	ctx.gitRunner = &stubGitRunner{}
	_, err := ctx.GitLog("-n", "5")
	require.NoError(t, err)
}

func TestPluginContextGitStatusWithProvider(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	ctx.gitRunner = &stubGitRunner{}
	_, err := ctx.GitStatus()
	require.NoError(t, err)
}

func TestPluginContextInvokeSkillWithProvider(t *testing.T) {
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, t.TempDir())
	ctx.skillInvoker = &stubSkillInvoker{}
	result, err := ctx.InvokeSkill("test-skill", map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
}

func TestPluginContextReadFileNonExistent(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)
	_, err := ctx.ReadFile("missing.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ReadFile")
}

func TestPluginContextWriteFileSandboxEscape(t *testing.T) {
	dir := t.TempDir()
	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)
	err := ctx.WriteFile("../../../tmp/escape.txt", "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WriteFile")
}

func TestPluginContextSearchFilesWithMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte("package test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))

	ctx := newPluginContext(&mockPermissionChecker{allowAll: true}, dir)
	results, err := ctx.SearchFiles("*.go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestPluginContextLoadWithIntegrations(t *testing.T) {
	loader := &mockPluginLoader{plugin: &fakePlugin{}}
	backend := NewGoPluginBackend(
		WithSkillDir(t.TempDir()),
		WithLLMCompleter(&stubLLMCompleter{}),
		WithHTTPFetcher(&stubHTTPFetcher{}),
		WithGitRunner(&stubGitRunner{}),
		WithSkillInvoker(&stubSkillInvoker{}),
	)
	backend.loader = loader

	manifest := skills.SkillManifest{
		Name: "test-plugin", Version: "1.0.0",
		Types:          []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{Entrypoint: "plugin.so"},
	}

	err := backend.Load(manifest, &mockPermissionChecker{allowAll: true})
	require.NoError(t, err)

	// Verify that the integrations were wired through to the context.
	assert.NotNil(t, backend.ctx.llmCompleter)
	assert.NotNil(t, backend.ctx.httpFetcher)
	assert.NotNil(t, backend.ctx.gitRunner)
	assert.NotNil(t, backend.ctx.skillInvoker)
}

// --- Additional fake plugin types for error testing ---

type fakePluginActivateError struct{}

func (fp *fakePluginActivateError) Manifest() skillsdk.Manifest {
	return skillsdk.Manifest{Name: "error-plugin"}
}

func (fp *fakePluginActivateError) Activate(_ skillsdk.Context) error {
	return fmt.Errorf("activation failed")
}

func (fp *fakePluginActivateError) Deactivate(_ skillsdk.Context) error {
	return nil
}

type fakePluginDeactivateError struct{}

func (fp *fakePluginDeactivateError) Manifest() skillsdk.Manifest {
	return skillsdk.Manifest{Name: "error-plugin"}
}

func (fp *fakePluginDeactivateError) Activate(_ skillsdk.Context) error {
	return nil
}

func (fp *fakePluginDeactivateError) Deactivate(_ skillsdk.Context) error {
	return fmt.Errorf("deactivation failed")
}

// writeTestFile is a helper that writes content to a file for testing.
func writeTestFile(path, content string) error {
	return writeFileHelper(path, content)
}
