package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellToolExecute(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	assert.Equal(t, "shell", st.Name())
	assert.NotEmpty(t, st.Description())
	assert.NotNil(t, st.InputSchema())

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello world",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "hello world\n", result.Content)
}

func TestShellToolTimeout(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 100*time.Millisecond)

	input, _ := json.Marshal(map[string]string{
		"command": "sleep 10",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")
}

func TestShellToolExitCode(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo error output >&2; exit 1",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "error output")
}

func TestShellToolOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 30KB (30 * 1024 = 30720 bytes)
	// Use printf to generate a known large output
	input, _ := json.Marshal(map[string]string{
		"command": "dd if=/dev/zero bs=1024 count=40 2>/dev/null | tr '\\0' 'A'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Output should be truncated to maxOutputBytes
	assert.LessOrEqual(t, len(result.Content), 30*1024+100) // some slack for truncation message
	assert.True(t, strings.Contains(result.Content, "truncated"))
}

func TestShellToolLargeOutputSetsDisplayContent(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 30KB but smaller than 100KB.
	// 50KB = 50 * 1024 = 51200 bytes.
	input, _ := json.Marshal(map[string]string{
		"command": "dd if=/dev/zero bs=1024 count=50 2>/dev/null | tr '\\0' 'B'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Content should be truncated at 30KB for the LLM.
	assert.LessOrEqual(t, len(result.Content), maxOutputBytes+50)
	assert.Contains(t, result.Content, "truncated")
	// DisplayContent should have more data than Content.
	assert.NotEmpty(t, result.DisplayContent)
	assert.Greater(t, len(result.DisplayContent), len(result.Content))
}

func TestShellToolHugeOutputCapsDisplayContent(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 100KB (maxDisplayBytes).
	// 120KB = 120 * 1024 = 122880 bytes.
	input, _ := json.Marshal(map[string]string{
		"command": "dd if=/dev/zero bs=1024 count=120 2>/dev/null | tr '\\0' 'C'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Content should be truncated at 30KB.
	assert.LessOrEqual(t, len(result.Content), maxOutputBytes+50)
	assert.Contains(t, result.Content, "truncated")
	// DisplayContent should be truncated at 100KB.
	assert.NotEmpty(t, result.DisplayContent)
	assert.LessOrEqual(t, len(result.DisplayContent), maxDisplayBytes+50)
	assert.Contains(t, result.DisplayContent, "truncated")
}

func TestShellToolSmallOutputNoDisplayContent(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Small output should not set DisplayContent (no redundancy).
	assert.Empty(t, result.DisplayContent)
}

func TestShellToolInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	result, err := st.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}

func TestShellToolSetDiffTracker(t *testing.T) {
	dir := t.TempDir()
	dt := NewDiffTracker()
	st := NewShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	// Run a command that doesn't change files — git diff should not record anything
	// (temp dir isn't a git repo, so detectChanges is a no-op).
	input, _ := json.Marshal(map[string]string{
		"command": "echo hello",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Empty(t, dt.Changes())
}

func TestShellToolNoDiffTrackerDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second) // No DiffTracker

	input, _ := json.Marshal(map[string]string{
		"command": "echo safe",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestShellToolDetectChangesInGitRepo(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo with one committed file.
	for _, cmd := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo initial > tracked.txt",
		"git add tracked.txt",
		"git commit -m init",
	} {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		st := NewShellTool(dir, 30*time.Second)
		r, err := st.Execute(context.Background(), input)
		require.NoError(t, err, "setup cmd %q", cmd)
		require.False(t, r.IsError, "setup cmd %q: %s", cmd, r.Content)
	}

	dt := NewDiffTracker()
	st := NewShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	// Modify a tracked file and create an untracked file in one command.
	input, _ := json.Marshal(map[string]string{
		"command": "echo modified > tracked.txt && echo new > untracked.txt",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	changes := dt.Changes()
	require.GreaterOrEqual(t, len(changes), 2, "should detect modified + untracked")

	pathSet := make(map[string]Operation)
	for _, c := range changes {
		pathSet[c.Path] = c.Operation
		assert.Equal(t, "shell", c.Tool)
	}
	assert.Equal(t, OpModified, pathSet["tracked.txt"], "tracked.txt should be modified")
	assert.Equal(t, OpCreated, pathSet["untracked.txt"], "untracked.txt should be created")
}

func TestShellToolDetectChangesRespectsOwnTimeout(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo.
	for _, cmd := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo initial > file.txt",
		"git add file.txt",
		"git commit -m init",
	} {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		st := NewShellTool(dir, 30*time.Second)
		r, err := st.Execute(context.Background(), input)
		require.NoError(t, err, "setup cmd %q", cmd)
		require.False(t, r.IsError, "setup cmd %q: %s", cmd, r.Content)
	}

	dt := NewDiffTracker()
	st := NewShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	// Modify a file so git status has something to report.
	input, _ := json.Marshal(map[string]string{
		"command": "echo changed > file.txt",
	})
	_, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	dt.Reset()

	// Verify detectChanges succeeds with a nil baseline (no pre-existing
	// dirty files). It creates its own timeout context internally rather
	// than relying on any parent context.
	st.detectChanges(nil)

	changes := dt.Changes()
	assert.GreaterOrEqual(t, len(changes), 1, "detectChanges should use its own timeout, not the parent context")
}

func TestShellToolDetectChangesDeduplicates(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo.
	for _, cmd := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo initial > file.txt",
		"git add file.txt",
		"git commit -m init",
	} {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		st := NewShellTool(dir, 30*time.Second)
		r, err := st.Execute(context.Background(), input)
		require.NoError(t, err, "setup cmd %q", cmd)
		require.False(t, r.IsError, "setup cmd %q: %s", cmd, r.Content)
	}

	dt := NewDiffTracker()
	st := NewShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	// First command: modify file.txt
	input1, _ := json.Marshal(map[string]string{
		"command": "echo changed > file.txt",
	})
	_, err := st.Execute(context.Background(), input1)
	require.NoError(t, err)

	// Second command: another no-op. detectChanges should not re-add file.txt.
	input2, _ := json.Marshal(map[string]string{
		"command": "echo done",
	})
	_, err = st.Execute(context.Background(), input2)
	require.NoError(t, err)

	changes := dt.Changes()
	// file.txt should appear exactly once despite two detectChanges calls.
	count := 0
	for _, c := range changes {
		if c.Path == "file.txt" {
			count++
		}
	}
	assert.Equal(t, 1, count, "file.txt should be recorded once, not duplicated")
}

func TestShellToolDetectChangesIgnoresPreExistingDirtyFiles(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo with one committed file.
	for _, cmd := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo initial > tracked.txt",
		"echo initial > preexisting.txt",
		"git add tracked.txt preexisting.txt",
		"git commit -m init",
	} {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		st := NewShellTool(dir, 30*time.Second)
		r, err := st.Execute(context.Background(), input)
		require.NoError(t, err, "setup cmd %q", cmd)
		require.False(t, r.IsError, "setup cmd %q: %s", cmd, r.Content)
	}

	// Dirty preexisting.txt BEFORE attaching the tracker, simulating
	// a file that was already modified before the agent turn.
	preInput, _ := json.Marshal(map[string]string{
		"command": "echo dirty > preexisting.txt",
	})
	preShell := NewShellTool(dir, 30*time.Second)
	_, err := preShell.Execute(context.Background(), preInput)
	require.NoError(t, err)

	dt := NewDiffTracker()
	st := NewShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	// Now modify tracked.txt — only this should be recorded.
	input, _ := json.Marshal(map[string]string{
		"command": "echo changed > tracked.txt",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	changes := dt.Changes()
	pathSet := make(map[string]bool)
	for _, c := range changes {
		pathSet[c.Path] = true
	}
	assert.True(t, pathSet["tracked.txt"], "tracked.txt should be recorded")
	assert.False(t, pathSet["preexisting.txt"], "preexisting.txt should NOT be recorded (pre-existing dirty file)")
}

func TestShellToolDetectChangesRunsOnTimeout(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo.
	for _, cmd := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo initial > file.txt",
		"git add file.txt",
		"git commit -m init",
	} {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		st := NewShellTool(dir, 30*time.Second)
		r, err := st.Execute(context.Background(), input)
		require.NoError(t, err, "setup cmd %q", cmd)
		require.False(t, r.IsError, "setup cmd %q: %s", cmd, r.Content)
	}

	dt := NewDiffTracker()
	// Very short timeout to trigger the timeout path.
	st := NewShellTool(dir, 100*time.Millisecond)
	st.SetDiffTracker(dt)

	// Command that writes a file then sleeps past the timeout.
	input, _ := json.Marshal(map[string]string{
		"command": "echo modified > file.txt && sleep 10",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")

	// Despite the timeout, detectChanges should have recorded the file change.
	changes := dt.Changes()
	require.GreaterOrEqual(t, len(changes), 1, "file changes should be detected even on timeout")
	pathSet := make(map[string]bool)
	for _, c := range changes {
		pathSet[c.Path] = true
	}
	assert.True(t, pathSet["file.txt"], "file.txt should be detected despite command timeout")
}
