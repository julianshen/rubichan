package starlark_test

import (
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
