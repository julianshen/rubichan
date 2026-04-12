package gittools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitToolMetadataAccessors exercises Name/Description/InputSchema/SearchHint
// for each constructor so the trivial accessor paths are covered.
func TestGitToolMetadataAccessors(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	type searchHinter interface {
		SearchHint() string
	}
	cases := []struct {
		name     string
		tool     tools.Tool
		wantName string
	}{
		{"status", NewStatusTool(repo), "git_status"},
		{"diff", NewDiffTool(repo), "git_diff"},
		{"log", NewLogTool(repo), "git_log"},
		{"show", NewShowTool(repo), "git_show"},
		{"blame", NewBlameTool(repo), "git_blame"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantName, tc.tool.Name())
			assert.NotEmpty(t, tc.tool.Description())

			schema := tc.tool.InputSchema()
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(schema, &parsed))
			assert.Equal(t, "object", parsed["type"])

			sh, ok := tc.tool.(searchHinter)
			require.True(t, ok)
			assert.NotEmpty(t, sh.SearchHint())
		})
	}
}

// TestGitInvalidJSONInput ensures every runner reports an invalid-input error.
func TestGitInvalidJSONInput(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	badJSON := json.RawMessage(`{not json`)

	stat, err := NewStatusTool(repo).Execute(context.Background(), badJSON)
	require.NoError(t, err)
	assert.True(t, stat.IsError)
	assert.Contains(t, stat.Content, "invalid input")

	diff, err := NewDiffTool(repo).Execute(context.Background(), badJSON)
	require.NoError(t, err)
	assert.True(t, diff.IsError)

	logR, err := NewLogTool(repo).Execute(context.Background(), badJSON)
	require.NoError(t, err)
	assert.True(t, logR.IsError)

	show, err := NewShowTool(repo).Execute(context.Background(), badJSON)
	require.NoError(t, err)
	assert.True(t, show.IsError)

	blame, err := NewBlameTool(repo).Execute(context.Background(), badJSON)
	require.NoError(t, err)
	assert.True(t, blame.IsError)
}

// TestGitDiffHeadWithoutBase covers the validation error for diff.
func TestGitDiffHeadWithoutBase(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewDiffTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"head":"HEAD"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "base is required")
}

// TestGitDiffStagedCombinedWithBase covers the "staged cannot be combined" check.
func TestGitDiffStagedCombinedWithBase(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewDiffTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"staged":true,"base":"HEAD"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "staged cannot be combined")
}

// TestGitDiffWithStagedFlag exercises the staged path successfully.
func TestGitDiffWithStagedFlag(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	file := filepath.Join(repo, "staged.txt")
	require.NoError(t, os.WriteFile(file, []byte("new content\n"), 0o644))
	_, _ = run(t, repo, "add", "staged.txt")

	tool := NewDiffTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"staged":true}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "new content")
}

// TestGitDiffWithBaseAndHead walks both base+head args.
func TestGitDiffWithBaseAndHead(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	// Make a second commit so we have two refs.
	file := filepath.Join(repo, "another.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello\n"), 0o644))
	_, _ = run(t, repo, "add", "another.txt")
	_, _ = run(t, repo, "commit", "-m", "second commit")

	tool := NewDiffTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"base":"HEAD~1","head":"HEAD"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "another.txt")
}

// TestGitLogWithAuthorAndPath exercises the author + path branches.
func TestGitLogWithAuthorAndPath(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewLogTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"author":"Test User","path":"README.md","limit":5}`),
	)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "initial commit")
}

// TestGitLogDefaultsLimit covers the "limit <= 0" default path.
func TestGitLogDefaultsLimit(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewLogTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

// TestGitShowMissingRev checks the empty-rev validation path.
func TestGitShowMissingRev(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewShowTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"rev":"   "}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "rev is required")
}

// TestGitShowUnknownRev covers the runGit failure path.
func TestGitShowUnknownRev(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewShowTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"rev":"definitely-not-a-real-rev"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// TestGitBlameMissingPath covers the empty-path validation.
func TestGitBlameMissingPath(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewBlameTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"  "}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path is required")
}

// TestGitBlameLineEndBeforeStart covers the range validation error.
func TestGitBlameLineEndBeforeStart(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewBlameTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"path":"README.md","line_start":5,"line_end":2}`),
	)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "line_end must be greater")
}

// TestGitBlameLineStartOnly auto-sets line_end to line_start (covers end<=0 branch).
func TestGitBlameLineStartOnly(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewBlameTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"path":"README.md","line_start":1}`),
	)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
}

// TestGitStatusWithUntrackedFalse exercises the untracked=false flag path.
func TestGitStatusWithUntrackedFalse(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("x\n"), 0o644))

	tool := NewStatusTool(repo)
	untFalse := false
	input, err := json.Marshal(statusInput{Untracked: &untFalse})
	require.NoError(t, err)
	result, err := tool.Execute(context.Background(), json.RawMessage(input))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.NotContains(t, result.Content, "untracked.txt")
}

// TestGitStatusWithPathFilter exercises path filter + resolveRepoPath.
func TestGitStatusWithPathFilter(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewStatusTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"README.md"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

// TestGitStatusPathEscapesRepo verifies resolveRepoPath rejects escapes.
func TestGitStatusPathEscapesRepo(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	outside := t.TempDir() // different directory
	tool := NewStatusTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"path":"`+strings.ReplaceAll(outside, `\`, `\\`)+`"}`),
	)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "within the git repository")
}

// TestGitDiffPathEscapesRepo verifies diff's resolveRepoPath error path.
func TestGitDiffPathEscapesRepo(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	outside := t.TempDir()
	tool := NewDiffTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"path":"`+strings.ReplaceAll(outside, `\`, `\\`)+`"}`),
	)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "within the git repository")
}

// TestGitLogPathEscapesRepo verifies log's resolveRepoPath error path.
func TestGitLogPathEscapesRepo(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	outside := t.TempDir()
	tool := NewLogTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"path":"`+strings.ReplaceAll(outside, `\`, `\\`)+`"}`),
	)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "within the git repository")
}

// TestGitBlamePathEscapesRepo verifies blame's resolveRepoPath error path.
func TestGitBlamePathEscapesRepo(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	outside := t.TempDir()
	tool := NewBlameTool(repo)
	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{"path":"`+strings.ReplaceAll(outside, `\`, `\\`)+`"}`),
	)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "within the git repository")
}

// TestTruncateGit hits the truncate() helper with large data.
func TestTruncateGit(t *testing.T) {
	t.Parallel()

	small := "small content"
	got, disp := truncate(small)
	assert.Equal(t, small, got)
	assert.Empty(t, disp)

	mid := strings.Repeat("x", 40*1024)
	got, disp = truncate(mid)
	assert.Contains(t, got, "output truncated")
	// display <= max (100k), so mid should be kept entirely in display
	assert.Equal(t, mid, disp)

	huge := strings.Repeat("y", 200*1024)
	got, disp = truncate(huge)
	assert.Contains(t, got, "output truncated")
	assert.Contains(t, disp, "output truncated")
	assert.Less(t, len(got), len(huge))
}

// TestRunGitEmptyOutput verifies the "<empty>" path for clean status.
func TestRunGitEmptyOutput(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	tool := NewStatusTool(repo)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "<empty>", result.Content)
}

// TestResolveRepoPathAbsolute ensures absolute paths resolve into a relative form.
func TestResolveRepoPathAbsolute(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	abs := filepath.Join(repo, "README.md")
	tool := NewStatusTool(repo)
	payload, err := json.Marshal(map[string]any{"path": abs})
	require.NoError(t, err)
	result, err := tool.Execute(context.Background(), json.RawMessage(payload))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}
