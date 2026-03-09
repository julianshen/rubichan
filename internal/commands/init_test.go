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

func TestInitCommandIncludesReadmeContext(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My Project\n\nA tool that does cool things.\n"), 0o644))

	cmd := NewInitCommand(dir)
	_, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	s := string(content)
	// Should include a project overview section
	assert.Contains(t, s, "## Project Overview")
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
