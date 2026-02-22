package starlark_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/sandbox"
	"github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementations for dependency injection ---

// mockLLMCompleter is a mock LLM provider that returns a canned response.
type mockLLMCompleter struct {
	response string
	err      error
}

func (m *mockLLMCompleter) Complete(_ context.Context, prompt string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// mockHTTPFetcher is a mock HTTP fetcher that returns a canned response.
type mockHTTPFetcher struct {
	response string
	err      error
}

func (m *mockHTTPFetcher) Fetch(_ context.Context, url string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// mockGitRunner is a mock git runner returning canned data.
type mockGitRunner struct {
	diffResult   string
	logResult    []starlark.GitLogEntry
	statusResult []starlark.GitStatusEntry
	err          error
}

func (m *mockGitRunner) Diff(_ context.Context, args ...string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.diffResult, nil
}

func (m *mockGitRunner) Log(_ context.Context, args ...string) ([]starlark.GitLogEntry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.logResult, nil
}

func (m *mockGitRunner) Status(_ context.Context) ([]starlark.GitStatusEntry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.statusResult, nil
}

// mockSkillInvoker is a mock skill invoker returning canned data.
type mockSkillInvoker struct {
	result map[string]any
	err    error
}

func (m *mockSkillInvoker) Invoke(_ context.Context, name string, input map[string]any) (map[string]any, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// newTestEngine creates a Starlark engine with real sandbox permissions for testing.
// The sandbox is backed by an in-memory SQLite store with pre-approved permissions.
func newTestEngine(t *testing.T, dir string, perms []skills.Permission) *starlark.Engine {
	t.Helper()
	st, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	for _, p := range perms {
		err := st.Approve("test-skill", string(p), "always")
		require.NoError(t, err)
	}

	sb := sandbox.New(st, "test-skill", perms, sandbox.DefaultPolicy())
	return starlark.NewEngine("test-skill", dir, sb)
}

// loadStar writes a .star file and loads it into the engine.
func loadStar(t *testing.T, engine *starlark.Engine, dir, filename, code string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, filename), []byte(code), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: filename,
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.NoError(t, err)
}

func TestBuiltinReadFile(t *testing.T) {
	dir := t.TempDir()

	// Create a file to read.
	testFile := filepath.Join(dir, "hello.txt")
	err := os.WriteFile(testFile, []byte("hello world"), 0o644)
	require.NoError(t, err)

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})
	loadStar(t, engine, dir, "main.star", `
result = read_file("`+testFile+`")
`)

	// The global variable "result" should contain the file contents.
	val := engine.Global("result")
	require.NotNil(t, val)
	assert.Equal(t, "hello world", val)
}

func TestBuiltinReadFilePermissionDenied(t *testing.T) {
	dir := t.TempDir()

	testFile := filepath.Join(dir, "secret.txt")
	err := os.WriteFile(testFile, []byte("secret"), 0o644)
	require.NoError(t, err)

	// No file:read permission declared.
	engine := newTestEngine(t, dir, []skills.Permission{})

	starCode := `result = read_file("` + testFile + `")`
	err = os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinWriteFile(t *testing.T) {
	dir := t.TempDir()

	outFile := filepath.Join(dir, "output.txt")

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileWrite})
	loadStar(t, engine, dir, "main.star", `
write_file("`+outFile+`", "written by starlark")
`)

	// Verify file was created with the correct content.
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, "written by starlark", string(data))
}

func TestBuiltinListDir(t *testing.T) {
	dir := t.TempDir()

	// Create some files.
	for _, name := range []string{"a.txt", "b.txt", "c.go"} {
		err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o644)
		require.NoError(t, err)
	}

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})
	loadStar(t, engine, dir, "main.star", `
entries = list_dir("`+dir+`")
count = len(entries)
`)

	val := engine.Global("count")
	require.NotNil(t, val)
	// Should list all files including main.star itself.
	assert.Equal(t, int64(4), val)
}

func TestBuiltinSearchFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some files in a subdirectory.
	subdir := filepath.Join(dir, "sub")
	err := os.Mkdir(subdir, 0o755)
	require.NoError(t, err)

	for _, name := range []string{"one.go", "two.go", "three.txt"} {
		err := os.WriteFile(filepath.Join(subdir, name), []byte(""), 0o644)
		require.NoError(t, err)
	}

	pattern := filepath.Join(subdir, "*.go")
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})
	loadStar(t, engine, dir, "main.star", `
matches = search_files("`+pattern+`")
count = len(matches)
`)

	val := engine.Global("count")
	require.NotNil(t, val)
	assert.Equal(t, int64(2), val)
}

func TestBuiltinExec(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermShellExec})
	loadStar(t, engine, dir, "main.star", `
result = exec("echo", "hello", "starlark")
stdout = result["stdout"]
exit_code = result["exit_code"]
`)

	stdout := engine.Global("stdout")
	require.NotNil(t, stdout)
	assert.Equal(t, "hello starlark\n", stdout)

	exitCode := engine.Global("exit_code")
	require.NotNil(t, exitCode)
	assert.Equal(t, int64(0), exitCode)
}

func TestBuiltinExecPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No shell:exec permission declared.
	engine := newTestEngine(t, dir, []skills.Permission{})

	starCode := `result = exec("echo", "hello")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinEnv(t *testing.T) {
	dir := t.TempDir()

	// Set a test environment variable.
	t.Setenv("RUBICHAN_TEST_VAR", "test-value-42")

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermEnvRead})
	loadStar(t, engine, dir, "main.star", `
result = env("RUBICHAN_TEST_VAR")
`)

	val := engine.Global("result")
	require.NotNil(t, val)
	assert.Equal(t, "test-value-42", val)
}

func TestBuiltinProjectRoot(t *testing.T) {
	dir := t.TempDir()

	// project_root() should return the skill directory (no permission needed).
	engine := newTestEngine(t, dir, []skills.Permission{})
	loadStar(t, engine, dir, "main.star", `
root = project_root()
`)

	val := engine.Global("root")
	require.NotNil(t, val)
	assert.Equal(t, dir, val)
}

func TestBuiltinReadFileNotFound(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})

	starCode := `result = read_file("` + filepath.Join(dir, "nonexistent.txt") + `")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read_file")
}

func TestBuiltinWriteFilePermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No file:write permission.
	engine := newTestEngine(t, dir, []skills.Permission{})

	starCode := `write_file("` + filepath.Join(dir, "out.txt") + `", "data")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinListDirPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No file:read permission.
	engine := newTestEngine(t, dir, []skills.Permission{})

	starCode := `entries = list_dir("` + dir + `")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinSearchFilesPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No file:read permission.
	engine := newTestEngine(t, dir, []skills.Permission{})

	starCode := `matches = search_files("` + filepath.Join(dir, "*.txt") + `")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinEnvPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No env:read permission.
	engine := newTestEngine(t, dir, []skills.Permission{})

	starCode := `val = env("PATH")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinExecNonZeroExit(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermShellExec})
	loadStar(t, engine, dir, "main.star", `
result = exec("false")
exit_code = result["exit_code"]
`)

	exitCode := engine.Global("exit_code")
	require.NotNil(t, exitCode)
	assert.NotEqual(t, int64(0), exitCode)
}

func TestBuiltinGlobalUndefined(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{})
	loadStar(t, engine, dir, "main.star", `
x = 1
`)

	// Accessing a non-existent global should return nil.
	val := engine.Global("nonexistent")
	assert.Nil(t, val)
}

func TestBuiltinGlobalNonStringNonInt(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{})
	loadStar(t, engine, dir, "main.star", `
items = [1, 2, 3]
`)

	// A list value should fall through to .String() representation.
	val := engine.Global("items")
	require.NotNil(t, val)
	assert.Equal(t, "[1, 2, 3]", val)
}

func TestBuiltinListDirNotFound(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})

	starCode := `entries = list_dir("` + filepath.Join(dir, "nonexistent") + `")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list_dir")
}

func TestBuiltinWriteFileInvalidPath(t *testing.T) {
	dir := t.TempDir()

	// Write to a path inside a non-existent directory.
	badPath := filepath.Join(dir, "nonexistent", "subdir", "file.txt")

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileWrite})

	starCode := `write_file("` + badPath + `", "data")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write_file")
}

func TestBuiltinExecCommandNotFound(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermShellExec})

	starCode := `result = exec("nonexistent_command_xyz_123")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec")
}

func TestBuiltinExecNoArgs(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermShellExec})

	// exec() with no args should error.
	starCode := `result = exec()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires at least 1 argument")
}

func TestBuiltinSearchFilesNoMatches(t *testing.T) {
	dir := t.TempDir()

	pattern := filepath.Join(dir, "*.xyz")
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})
	loadStar(t, engine, dir, "main.star", `
matches = search_files("`+pattern+`")
count = len(matches)
`)

	val := engine.Global("count")
	require.NotNil(t, val)
	assert.Equal(t, int64(0), val)
}

func TestBuiltinExecWithStderr(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermShellExec})
	loadStar(t, engine, dir, "main.star", `
result = exec("sh", "-c", "echo error >&2; exit 1")
stderr = result["stderr"]
exit_code = result["exit_code"]
`)

	stderr := engine.Global("stderr")
	require.NotNil(t, stderr)
	assert.Contains(t, stderr, "error")

	exitCode := engine.Global("exit_code")
	require.NotNil(t, exitCode)
	assert.Equal(t, int64(1), exitCode)
}

// --- Tests for Task 11: LLM, network, git, skill-invoke built-ins ---

func TestBuiltinLLMComplete(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermLLMCall})
	engine.SetLLMCompleter(&mockLLMCompleter{response: "Hello from LLM!"})

	loadStar(t, engine, dir, "main.star", `
result = llm_complete("Say hello")
`)

	val := engine.Global("result")
	require.NotNil(t, val)
	assert.Equal(t, "Hello from LLM!", val)
}

func TestBuiltinLLMCompletePermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No llm:call permission declared.
	engine := newTestEngine(t, dir, []skills.Permission{})
	engine.SetLLMCompleter(&mockLLMCompleter{response: "should not reach"})

	starCode := `result = llm_complete("Say hello")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinFetch(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermNetFetch})
	engine.SetHTTPFetcher(&mockHTTPFetcher{response: `{"status":"ok"}`})

	loadStar(t, engine, dir, "main.star", `
result = fetch("https://example.com/api")
`)

	val := engine.Global("result")
	require.NotNil(t, val)
	assert.Equal(t, `{"status":"ok"}`, val)
}

func TestBuiltinFetchPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No net:fetch permission declared.
	engine := newTestEngine(t, dir, []skills.Permission{})
	engine.SetHTTPFetcher(&mockHTTPFetcher{response: "should not reach"})

	starCode := `result = fetch("https://example.com")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinGitDiff(t *testing.T) {
	dir := t.TempDir()

	diffOutput := "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n"
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})
	engine.SetGitRunner(&mockGitRunner{diffResult: diffOutput})

	loadStar(t, engine, dir, "main.star", `
result = git_diff("HEAD~1")
`)

	val := engine.Global("result")
	require.NotNil(t, val)
	assert.Contains(t, val, "diff --git")
	assert.Contains(t, val, "-old")
	assert.Contains(t, val, "+new")
}

func TestBuiltinGitLog(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})
	engine.SetGitRunner(&mockGitRunner{
		logResult: []starlark.GitLogEntry{
			{Hash: "abc123", Author: "Alice", Message: "Initial commit"},
			{Hash: "def456", Author: "Bob", Message: "Add feature"},
		},
	})

	loadStar(t, engine, dir, "main.star", `
commits = git_log("--oneline", "-n", "2")
count = len(commits)
first_hash = commits[0]["hash"]
first_author = commits[0]["author"]
first_message = commits[0]["message"]
second_hash = commits[1]["hash"]
`)

	count := engine.Global("count")
	require.NotNil(t, count)
	assert.Equal(t, int64(2), count)

	assert.Equal(t, "abc123", engine.Global("first_hash"))
	assert.Equal(t, "Alice", engine.Global("first_author"))
	assert.Equal(t, "Initial commit", engine.Global("first_message"))
	assert.Equal(t, "def456", engine.Global("second_hash"))
}

func TestBuiltinGitStatus(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})
	engine.SetGitRunner(&mockGitRunner{
		statusResult: []starlark.GitStatusEntry{
			{Path: "file.go", Status: "M"},
			{Path: "new.go", Status: "A"},
		},
	})

	loadStar(t, engine, dir, "main.star", `
files = git_status()
count = len(files)
first_path = files[0]["path"]
first_status = files[0]["status"]
second_path = files[1]["path"]
second_status = files[1]["status"]
`)

	count := engine.Global("count")
	require.NotNil(t, count)
	assert.Equal(t, int64(2), count)

	assert.Equal(t, "file.go", engine.Global("first_path"))
	assert.Equal(t, "M", engine.Global("first_status"))
	assert.Equal(t, "new.go", engine.Global("second_path"))
	assert.Equal(t, "A", engine.Global("second_status"))
}

func TestBuiltinInvokeSkill(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermSkillInvoke})
	engine.SetSkillInvoker(&mockSkillInvoker{
		result: map[string]any{
			"output": "skill result",
			"code":   42.0,
		},
	})

	loadStar(t, engine, dir, "main.star", `
result = invoke_skill("other-skill", {"key": "value"})
output = result["output"]
code = result["code"]
`)

	output := engine.Global("output")
	require.NotNil(t, output)
	assert.Equal(t, "skill result", output)

	code := engine.Global("code")
	require.NotNil(t, code)
	assert.Equal(t, int64(42), code)
}

func TestBuiltinGitDiffPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No git:read permission declared.
	engine := newTestEngine(t, dir, []skills.Permission{})
	engine.SetGitRunner(&mockGitRunner{diffResult: "should not reach"})

	starCode := `result = git_diff()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinInvokeSkillPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	// No skill:invoke permission declared.
	engine := newTestEngine(t, dir, []skills.Permission{})
	engine.SetSkillInvoker(&mockSkillInvoker{result: map[string]any{}})

	starCode := `result = invoke_skill("other-skill", {})`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinLLMCompleteError(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermLLMCall})
	engine.SetLLMCompleter(&mockLLMCompleter{err: fmt.Errorf("LLM unavailable")})

	starCode := `result = llm_complete("Say hello")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm_complete")
}

func TestBuiltinFetchError(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermNetFetch})
	engine.SetHTTPFetcher(&mockHTTPFetcher{err: fmt.Errorf("network error")})

	starCode := `result = fetch("https://example.com")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch")
}

func TestBuiltinLLMCompleteNilProvider(t *testing.T) {
	dir := t.TempDir()

	// Don't set any LLM completer — should get an error about nil provider.
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermLLMCall})

	starCode := `result = llm_complete("Say hello")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm_complete")
}

func TestBuiltinFetchNilProvider(t *testing.T) {
	dir := t.TempDir()

	// Don't set any HTTP fetcher — should get an error about nil provider.
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermNetFetch})

	starCode := `result = fetch("https://example.com")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch")
}

func TestBuiltinGitDiffNilProvider(t *testing.T) {
	dir := t.TempDir()

	// Don't set any git runner — should get an error about nil provider.
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})

	starCode := `result = git_diff()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_diff")
}

func TestBuiltinInvokeSkillNilProvider(t *testing.T) {
	dir := t.TempDir()

	// Don't set any skill invoker — should get an error about nil provider.
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermSkillInvoke})

	starCode := `result = invoke_skill("other-skill", {})`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoke_skill")
}

func TestBuiltinInvokeSkillComplexInput(t *testing.T) {
	dir := t.TempDir()

	// Test that complex Starlark types (int, bool, None, list, dict, float) are
	// correctly converted to Go values when passed to invoke_skill.
	engine := newTestEngine(t, dir, []skills.Permission{skills.PermSkillInvoke})
	engine.SetSkillInvoker(&mockSkillInvoker{
		result: map[string]any{"ok": true},
	})

	loadStar(t, engine, dir, "main.star", `
result = invoke_skill("other-skill", {
    "name": "test",
    "count": 42,
    "flag": True,
    "empty": None,
    "items": [1, 2, 3],
    "nested": {"a": "b"},
    "pi": 3.14,
})
`)

	// The mock always returns {"ok": true}, so just verify it didn't error.
	val := engine.Global("result")
	require.NotNil(t, val)
}

func TestBuiltinGitLogNilProvider(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})

	starCode := `commits = git_log()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_log")
}

func TestBuiltinGitStatusNilProvider(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})

	starCode := `files = git_status()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_status")
}

func TestBuiltinGitLogPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{})
	engine.SetGitRunner(&mockGitRunner{})

	starCode := `commits = git_log()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinGitStatusPermissionDenied(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{})
	engine.SetGitRunner(&mockGitRunner{})

	starCode := `files = git_status()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}

func TestBuiltinGitLogError(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})
	engine.SetGitRunner(&mockGitRunner{err: fmt.Errorf("git error")})

	starCode := `commits = git_log()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_log")
}

func TestBuiltinGitStatusError(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})
	engine.SetGitRunner(&mockGitRunner{err: fmt.Errorf("git error")})

	starCode := `files = git_status()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_status")
}

func TestBuiltinGitDiffError(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermGitRead})
	engine.SetGitRunner(&mockGitRunner{err: fmt.Errorf("git error")})

	starCode := `result = git_diff()`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_diff")
}

func TestBuiltinInvokeSkillError(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermSkillInvoke})
	engine.SetSkillInvoker(&mockSkillInvoker{err: fmt.Errorf("skill not found")})

	starCode := `result = invoke_skill("missing-skill", {})`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoke_skill")
}

func TestBuiltinReadFileSandboxEscape(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})

	// Attempt to read a file outside the skill directory.
	starCode := `result = read_file("../../etc/passwd")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestBuiltinWriteFileSandboxEscape(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileWrite})

	starCode := `write_file("../../tmp/escape.txt", "evil")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestBuiltinListDirSandboxEscape(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})

	starCode := `entries = list_dir("../../")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestBuiltinReadFileRelativePath(t *testing.T) {
	dir := t.TempDir()

	// Create a file inside the skill directory using a relative path.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte("relative data"), 0o644))

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})
	loadStar(t, engine, dir, "main.star", `
result = read_file("data.txt")
`)

	val := engine.Global("result")
	require.NotNil(t, val)
	assert.Equal(t, "relative data", val)
}

func TestBuiltinLog(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{})
	loadStar(t, engine, dir, "main.star", `
log("hello from starlark")
x = 42
`)

	val := engine.Global("x")
	require.NotNil(t, val)
	assert.Equal(t, int64(42), val)
}

func TestBuiltinSearchFilesSandboxEscapeFiltered(t *testing.T) {
	dir := t.TempDir()

	// Create a file outside the skill directory.
	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "outside.txt"), []byte("outside"), 0o644))

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermFileRead})

	// search_files with path that resolves outside should return 0 matches
	// (filtered out by sandbox).
	pattern := filepath.Join(outsideDir, "*.txt")
	loadStar(t, engine, dir, "main.star", `
matches = search_files("`+pattern+`")
count = len(matches)
`)

	val := engine.Global("count")
	require.NotNil(t, val)
	assert.Equal(t, int64(0), val, "matches outside skill dir should be filtered")
}

func TestBuiltinInvokeSkillNonDictInput(t *testing.T) {
	dir := t.TempDir()

	engine := newTestEngine(t, dir, []skills.Permission{skills.PermSkillInvoke})
	engine.SetSkillInvoker(&mockSkillInvoker{result: map[string]any{"ok": true}})

	// Pass a non-dict second argument — should error.
	starCode := `result = invoke_skill("other-skill", "not-a-dict")`
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(starCode), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	err = engine.Load(manifest, engine.Checker())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoke_skill")
}
