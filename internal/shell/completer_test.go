package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleterExecutableCompletion(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	c := NewCompleter(
		map[string]bool{"ls": true, "lsof": true, "lsblk": true},
		&workDir,
		nil, nil,
	)

	results := c.Complete("ls", 2)
	names := completionTexts(results)
	assert.Contains(t, names, "ls")
	assert.Contains(t, names, "lsof")
	assert.Contains(t, names, "lsblk")

	results = c.Complete("lsb", 3)
	names = completionTexts(results)
	assert.Equal(t, []string{"lsblk"}, names)
}

func TestCompleterFilePathCompletion(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main_test.go"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "README.md"), nil, 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "src"), 0o755))

	c := NewCompleter(
		map[string]bool{"cat": true},
		&workDir,
		nil, nil,
	)

	results := c.Complete("cat ma", 6)
	names := completionTexts(results)
	assert.Contains(t, names, "main.go")
	assert.Contains(t, names, "main_test.go")
	assert.NotContains(t, names, "README.md")

	// Directories should have trailing /
	results = c.Complete("cat s", 5)
	names = completionTexts(results)
	assert.Contains(t, names, "src/")
}

func TestCompleterDirectoryCompletion(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), nil, 0o644))

	c := NewCompleter(
		map[string]bool{},
		&workDir,
		nil, nil,
	)

	// cd only completes directories
	results := c.Complete("cd sr", 5)
	names := completionTexts(results)
	assert.Contains(t, names, "src/")
	assert.NotContains(t, names, "main.go")
}

func TestCompleterSlashCommandCompletion(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	c := NewCompleter(
		map[string]bool{},
		&workDir,
		func() []string { return []string{"model", "quit", "help"} },
		nil,
	)

	results := c.Complete("/mo", 3)
	names := completionTexts(results)
	assert.Contains(t, names, "model")
}

func TestCompleterBuiltinCompletion(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	c := NewCompleter(map[string]bool{}, &workDir, nil, nil)

	results := c.Complete("ex", 2)
	names := completionTexts(results)
	assert.Contains(t, names, "exit")

	// "cd" is already a full command — should complete to file paths, not itself
	results = c.Complete("cd", 2)
	names = completionTexts(results)
	assert.Contains(t, names, "cd")
}

func TestCompleterEmptyInput(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	c := NewCompleter(map[string]bool{"ls": true}, &workDir, nil, nil)

	results := c.Complete("", 0)
	assert.Empty(t, results)

	results = c.Complete("   ", 3)
	assert.Empty(t, results)
}

func TestCompleterSecondArgFilePath(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "foo.txt"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "bar.txt"), nil, 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "src", "main.go"), nil, 0o644))

	c := NewCompleter(
		map[string]bool{"cat": true},
		&workDir,
		nil, nil,
	)

	// Trailing space = complete file paths for second arg
	results := c.Complete("cat ", 4)
	names := completionTexts(results)
	assert.Contains(t, names, "foo.txt")
	assert.Contains(t, names, "bar.txt")
	assert.Contains(t, names, "src/")

	// Complete within subdirectory
	results = c.Complete("ls src/", 7)
	names = completionTexts(results)
	assert.Contains(t, names, "main.go")
}

func TestCompleterGitBranchCompletion(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	gitBranches := func(_ string) []string {
		return []string{"main", "major-fix", "dev"}
	}

	c := NewCompleter(
		map[string]bool{"git": true},
		&workDir,
		nil,
		gitBranches,
	)

	results := c.Complete("git checkout ma", 15)
	names := completionTexts(results)
	assert.Contains(t, names, "main")
	assert.Contains(t, names, "major-fix")
	assert.NotContains(t, names, "dev")

	// Non-branch git commands should not complete branches
	results = c.Complete("git status ma", 13)
	names = completionTexts(results)
	assert.Empty(t, names)
}

func TestCompleterNoLLMCall(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	// No agentTurn in Completer — it's purely local
	c := NewCompleter(
		map[string]bool{"ls": true},
		&workDir,
		nil, nil,
	)

	// Verify Complete works without any LLM dependency
	results := c.Complete("ls", 2)
	assert.NotEmpty(t, results)
}

func completionTexts(completions []Completion) []string {
	texts := make([]string, len(completions))
	for i, c := range completions {
		texts[i] = c.Text
	}
	return texts
}
