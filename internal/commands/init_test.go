package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Init Command: Interface ---

func TestInitCommandName(t *testing.T) {
	cmd := NewInitCommand(t.TempDir())
	assert.Equal(t, "init", cmd.Name())
}

func TestInitCommandDescription(t *testing.T) {
	cmd := NewInitCommand(t.TempDir())
	assert.NotEmpty(t, cmd.Description())
}

func TestInitCommandArguments(t *testing.T) {
	cmd := NewInitCommand(t.TempDir())
	args := cmd.Arguments()
	require.Len(t, args, 1)
	assert.Equal(t, "format", args[0].Name)
	assert.False(t, args[0].Required)
	assert.Contains(t, args[0].Static, "agents")
	assert.Contains(t, args[0].Static, "claude")
}

func TestInitCommandComplete(t *testing.T) {
	cmd := NewInitCommand(t.TempDir())
	candidates := cmd.Complete(context.Background(), nil)
	assert.Nil(t, candidates)
}

func TestInitCommandImplementsSlashCommand(t *testing.T) {
	var _ SlashCommand = NewInitCommand(t.TempDir())
}

// --- Init Command: Execute generates AGENTS.md by default ---

func TestInitCommandDefaultGeneratesAgentsMD(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENTS.md")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENTS.md")
}

func TestInitCommandExplicitAgentsArg(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	result, err := cmd.Execute(context.Background(), []string{"agents"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENTS.md")

	_, err = os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.NoError(t, err)
}

func TestInitCommandGeneratesClaudeMD(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	result, err := cmd.Execute(context.Background(), []string{"claude"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "CLAUDE.md")

	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# CLAUDE.md")
}

func TestInitCommandUnknownFormatReturnsError(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	_, err := cmd.Execute(context.Background(), []string{"unknown"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown format")
}

func TestInitCommandRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(existing, []byte("existing"), 0o644))

	cmd := NewInitCommand(dir)
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Original content preserved
	content, err := os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, "existing", string(content))
}

// --- Init Command: Project detection ---

func TestInitCommandDetectsGoProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0o644))

	cmd := NewInitCommand(dir)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENTS.md")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "go test")
	assert.Contains(t, s, "go build")
}

func TestInitCommandDetectsNodeProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"foo","scripts":{"test":"jest","build":"tsc","lint":"eslint ."}}`), 0o644))

	cmd := NewInitCommand(dir)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENTS.md")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "npm")
}

func TestInitCommandDetectsPythonProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname = \"foo\"\n"), 0o644))

	cmd := NewInitCommand(dir)
	_, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "Python")
}

func TestInitCommandDetectsRustProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"foo\"\n"), 0o644))

	cmd := NewInitCommand(dir)
	_, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "cargo")
}

func TestInitCommandEmptyProject(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENTS.md")

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	// Should still generate a valid file with placeholder sections
	assert.Contains(t, string(content), "# AGENTS.md")
	assert.Contains(t, string(content), "## Project Overview")
}

func TestInitCommandDetectsMultipleLanguages(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"frontend","scripts":{"build":"vite build"}}`), 0o644))

	cmd := NewInitCommand(dir)
	_, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "go")
	assert.Contains(t, s, "npm")
}

// --- projectInfo detection ---

func TestDetectProjectInfoGoModule(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/user/myproject\n\ngo 1.22\n"), 0o644))

	info := detectProjectInfo(dir)
	assert.Contains(t, info.languages, "Go")
	assert.NotEmpty(t, info.buildCmds)
	assert.NotEmpty(t, info.testCmds)
}

func TestDetectProjectInfoEmpty(t *testing.T) {
	dir := t.TempDir()
	info := detectProjectInfo(dir)
	assert.Empty(t, info.languages)
	assert.Empty(t, info.buildCmds)
}

func TestDetectProjectInfoNodeScripts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"app","scripts":{"test":"vitest","build":"vite build","lint":"eslint ."}}`), 0o644))

	info := detectProjectInfo(dir)
	assert.Contains(t, info.languages, "JavaScript/TypeScript")
	assert.NotEmpty(t, info.testCmds)
	assert.NotEmpty(t, info.buildCmds)
	assert.NotEmpty(t, info.lintCmds)
}

// --- Case-insensitive format argument ---

func TestInitCommandFormatCaseInsensitive(t *testing.T) {
	tests := []struct {
		arg      string
		filename string
	}{
		{"AGENTS", "AGENTS.md"},
		{"Claude", "CLAUDE.md"},
		{"CLAUDE", "CLAUDE.md"},
		{"Agents", "AGENTS.md"},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			dir := t.TempDir()
			cmd := NewInitCommand(dir)
			result, err := cmd.Execute(context.Background(), []string{tt.arg})
			require.NoError(t, err)
			assert.Contains(t, result.Output, tt.filename)

			_, err = os.Stat(filepath.Join(dir, tt.filename))
			assert.NoError(t, err)
		})
	}
}

// --- os.Stat error handling (non-ErrNotExist) ---

func TestInitCommandStatErrorReturnsError(t *testing.T) {
	// Use a non-existent parent directory to trigger a stat error
	// that is not ErrNotExist (the parent doesn't exist, so stat
	// on a child path gives a different error on some systems).
	// Alternatively, use a workDir that is a file, not a directory.
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	// workDir is a file, so filepath.Join(file, "AGENTS.md") will
	// fail stat with a "not a directory" error, not ErrNotExist.
	cmd := NewInitCommand(file)
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AGENTS.md")
}

// --- Write failure ---

func TestInitCommandWriteFailure(t *testing.T) {
	cmd := NewInitCommand("/nonexistent/path/that/does/not/exist")
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}

// --- detectNodePM ---

func TestDetectNodePMBun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{}, 0o644))
	assert.Equal(t, "bun", detectNodePM(dir))
}

func TestDetectNodePMBunLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bun.lock"), []byte{}, 0o644))
	assert.Equal(t, "bun", detectNodePM(dir))
}

func TestDetectNodePMPnpm(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte{}, 0o644))
	assert.Equal(t, "pnpm", detectNodePM(dir))
}

func TestDetectNodePMYarn(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte{}, 0o644))
	assert.Equal(t, "yarn", detectNodePM(dir))
}

func TestDetectNodePMDefaultNpm(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "npm", detectNodePM(dir))
}

func TestDetectNodePMBunTakesPriority(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte{}, 0o644))
	assert.Equal(t, "bun", detectNodePM(dir))
}

// --- Python detection alternatives ---

func TestDetectProjectInfoPythonSetupPy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup.py"), []byte("from setuptools import setup\n"), 0o644))

	info := detectProjectInfo(dir)
	assert.Contains(t, info.languages, "Python")
	assert.NotEmpty(t, info.testCmds)
}

func TestDetectProjectInfoPythonRequirements(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0o644))

	info := detectProjectInfo(dir)
	assert.Contains(t, info.languages, "Python")
}

// --- readPackageScripts error resilience ---

func TestDetectProjectInfoMalformedPackageJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{bad json`), 0o644))

	info := detectProjectInfo(dir)
	// Should still detect JS/TS language but have no script-based commands
	assert.Contains(t, info.languages, "JavaScript/TypeScript")
	assert.Empty(t, info.buildCmds)
	assert.Empty(t, info.testCmds)
	assert.Empty(t, info.lintCmds)
}

func TestReadPackageScriptsNonexistentFile(t *testing.T) {
	result := readPackageScripts("/nonexistent/package.json")
	assert.Nil(t, result)
}

func TestInitCommandGeneratesAgentMD(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	result, err := cmd.Execute(context.Background(), []string{"agent"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENT.md")

	content, err := os.ReadFile(filepath.Join(dir, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENT.md")
	assert.Contains(t, string(content), "## Project Overview")
}
