package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestShellTool(dir string, timeout time.Duration) *ShellTool {
	st := NewShellTool(dir, timeout)
	st.SetSandbox(nil)
	return st
}

// initGitRepo initializes a git repo in dir with the given committed files.
// Each file is created with "initial" as content, staged, and committed.
func initGitRepo(t *testing.T, dir string, files ...string) {
	t.Helper()
	cmds := []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
	}
	for _, f := range files {
		cmds = append(cmds, "echo initial > "+f)
	}
	cmds = append(cmds, "git add "+strings.Join(files, " "))
	cmds = append(cmds, "git commit -m init")

	for _, cmd := range cmds {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		st := newTestShellTool(dir, 30*time.Second)
		r, err := st.Execute(context.Background(), input)
		require.NoError(t, err, "setup cmd %q", cmd)
		require.False(t, r.IsError, "setup cmd %q: %s", cmd, r.Content)
	}
}

func TestShellToolExecute(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

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
	st := newTestShellTool(dir, 100*time.Millisecond)

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
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo error output >&2; exit 1",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "error output")
}

func TestShellToolExecuteStreamEmitsEvents(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello stream",
	})
	var events []ToolEvent
	result, err := st.ExecuteStream(context.Background(), input, func(ev ToolEvent) {
		events = append(events, ev)
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello stream")
	require.NotEmpty(t, events)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)
}

func TestShellToolOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

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
	st := newTestShellTool(dir, 30*time.Second)

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
	st := newTestShellTool(dir, 30*time.Second)

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
	st := newTestShellTool(dir, 30*time.Second)

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
	st := newTestShellTool(dir, 30*time.Second)

	result, err := st.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}

func TestShellToolSetDiffTracker(t *testing.T) {
	dir := t.TempDir()
	dt := NewDiffTracker()
	st := newTestShellTool(dir, 30*time.Second)
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
	st := newTestShellTool(dir, 30*time.Second) // No DiffTracker

	input, _ := json.Marshal(map[string]string{
		"command": "echo safe",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestShellToolDetectChangesInGitRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, "tracked.txt")

	dt := NewDiffTracker()
	st := newTestShellTool(dir, 30*time.Second)
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
	initGitRepo(t, dir, "file.txt")

	dt := NewDiffTracker()
	st := newTestShellTool(dir, 30*time.Second)
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
	initGitRepo(t, dir, "file.txt")

	dt := NewDiffTracker()
	st := newTestShellTool(dir, 30*time.Second)
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
	initGitRepo(t, dir, "tracked.txt", "preexisting.txt")

	// Dirty preexisting.txt BEFORE attaching the tracker, simulating
	// a file that was already modified before the agent turn.
	preInput, _ := json.Marshal(map[string]string{
		"command": "echo dirty > preexisting.txt",
	})
	preShell := newTestShellTool(dir, 30*time.Second)
	_, err := preShell.Execute(context.Background(), preInput)
	require.NoError(t, err)

	dt := NewDiffTracker()
	st := newTestShellTool(dir, 30*time.Second)
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
	initGitRepo(t, dir, "file.txt")

	dt := NewDiffTracker()
	// Very short timeout to trigger the timeout path.
	st := newTestShellTool(dir, 100*time.Millisecond)
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

func TestShellToolInterceptorAllowsRecursiveRMInsideWorkdir(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	victim := dir + "/victim"
	require.NoError(t, os.Mkdir(victim, 0755))

	input, _ := json.Marshal(map[string]string{
		"command": "rm -rf victim",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	_, statErr := os.Stat(victim)
	assert.Error(t, statErr, "in-workdir recursive rm should execute")
	assert.True(t, os.IsNotExist(statErr))
}

func TestShellToolInterceptorBlocksRecursiveRMOutsideWorkdir(t *testing.T) {
	dir := t.TempDir()
	parentVictim := filepath.Join(filepath.Dir(dir), "outside-victim")
	require.NoError(t, os.Mkdir(parentVictim, 0755))
	t.Cleanup(func() { _ = os.RemoveAll(parentVictim) })

	st := newTestShellTool(dir, 30*time.Second)
	input, _ := json.Marshal(map[string]string{
		"command": "rm -rf ../outside-victim",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command blocked")
	assert.Contains(t, result.Content, "escape working directory")
	_, statErr := os.Stat(parentVictim)
	assert.NoError(t, statErr, "outside target should remain because command is blocked (abs)")
}

func TestShellToolInterceptorWarnsOnRedirect(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo hi > redirected.txt",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "warning: shell safety interceptor")
	assert.Contains(t, result.Content, "redirects output to a file")

	data, readErr := os.ReadFile(dir + "/redirected.txt")
	require.NoError(t, readErr)
	assert.Equal(t, "hi\n", string(data))
}

func TestShellToolInterceptorWarnsOnSedInPlace(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "sed -i",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "warning: shell safety interceptor")
	assert.Contains(t, result.Content, "sed -i")
}

func TestShellToolInterceptorRoutesApplyPatchToFileTool(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "apply_patch foo",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command requires routing")
	assert.Contains(t, result.Content, "routed through the file tool")
}

func TestShellToolInterceptorRoutesApplyPatchAfterCommandSeparator(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo ok; apply_patch foo",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command requires routing")
}

func TestShellToolInterceptorRoutesApplyPatchWithEnvPrefix(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "FOO=1 apply_patch foo",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command requires routing")
}

func TestShellToolInterceptorRoutesApplyPatchViaShC(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": `sh -c 'apply_patch foo'`,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command requires routing")
}

func TestShellToolInterceptorRoutesApplyPatchInsideQuotedSeparator(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": `sh -c "echo \"a|b\"; apply_patch foo"`,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command requires routing")
}

func TestShellToolInterceptorRoutesApplyPatchViaEnv(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "env FOO=1 apply_patch foo",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command requires routing")
}

func TestShellToolExecuteStreamTimeout(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 200*time.Millisecond)

	input, _ := json.Marshal(map[string]string{
		"command": "echo before && sleep 10",
	})

	var events []ToolEvent
	emit := func(ev ToolEvent) {
		events = append(events, ev)
	}

	result, err := st.ExecuteStream(context.Background(), input, emit)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")
}

func TestShellToolExecuteStreamExitCode(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo error >&2; exit 1",
	})

	var events []ToolEvent
	emit := func(ev ToolEvent) {
		events = append(events, ev)
	}

	result, err := st.ExecuteStream(context.Background(), input, emit)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)
	assert.True(t, events[len(events)-1].IsError)
}

func TestShellToolExecuteStreamInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	result, err := st.ExecuteStream(context.Background(), json.RawMessage(`{invalid`), func(ev ToolEvent) {})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}

func TestShellToolExecuteStreamBlockedCommand(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "apply_patch foo",
	})

	var events []ToolEvent
	result, err := st.ExecuteStream(context.Background(), input, func(ev ToolEvent) {
		events = append(events, ev)
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "routing")
	// Main's ExecuteStream emits EventEnd even on routing failures.
	if len(events) > 0 {
		assert.Equal(t, EventEnd, events[len(events)-1].Stage)
	}
}

func TestShellToolExecuteStreamDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, "tracked.txt")

	dt := NewDiffTracker()
	st := newTestShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	input, _ := json.Marshal(map[string]string{
		"command": "echo modified > tracked.txt",
	})

	result, err := st.ExecuteStream(context.Background(), input, func(ev ToolEvent) {})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	changes := dt.Changes()
	require.GreaterOrEqual(t, len(changes), 1)
	pathSet := make(map[string]bool)
	for _, c := range changes {
		pathSet[c.Path] = true
	}
	assert.True(t, pathSet["tracked.txt"])
}

func TestShellToolInterceptorBlocksRecursiveRMViaShC(t *testing.T) {
	dir := t.TempDir()
	parentVictim := filepath.Join(filepath.Dir(dir), "outside-victim-shc")
	require.NoError(t, os.Mkdir(parentVictim, 0755))
	t.Cleanup(func() { _ = os.RemoveAll(parentVictim) })

	st := newTestShellTool(dir, 30*time.Second)
	input, _ := json.Marshal(map[string]string{
		"command": "sh -c 'rm -rf ../outside-victim-shc'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command blocked")
	_, statErr := os.Stat(parentVictim)
	assert.NoError(t, statErr, "outside target should remain because command is blocked")
}

// --- Command substitution blocking tests ---

func TestShellToolBlocksCommandSubstitutionDollarParen(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo $(whoami)",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command substitution")
}

func TestShellToolBlocksCommandSubstitutionBackticks(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo `whoami`",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command substitution")
}

func TestShellToolBlocksProcessSubstitution(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "diff <(ls) >(cat)",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command substitution")
}

func TestShellToolAllowsDollarSignInStrings(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	// Plain $VAR usage should be allowed (variable expansion, not command substitution)
	input, _ := json.Marshal(map[string]string{
		"command": "echo $HOME",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestShellToolBlocksNestedCommandSubstitution(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "sh -c 'echo $(whoami)'",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command substitution")
}

// --- Extended shell input tests ---

func TestShellToolCustomTimeout(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	// Override timeout via input parameter — 200ms should cause a timeout on sleep 10
	input, _ := json.Marshal(map[string]interface{}{
		"command": "sleep 10",
		"timeout": 200,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")
}

func TestShellToolCustomTimeoutMaxCap(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	// Timeout above max (600000ms) should be capped
	input, _ := json.Marshal(map[string]interface{}{
		"command": "echo ok",
		"timeout": 999999,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "ok")
}

func TestShellToolCustomDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0755))

	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]interface{}{
		"command":   "pwd",
		"directory": subdir,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "subdir")
}

func TestShellToolDirectoryMustBeAbsolute(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]interface{}{
		"command":   "pwd",
		"directory": "relative/path",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "absolute")
}

func TestShellToolDirectoryMustExist(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]interface{}{
		"command":   "pwd",
		"directory": filepath.Join(dir, "nonexistent"),
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "does not exist")
}

func TestShellToolDirectoryMustBeWithinWorkDir(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]interface{}{
		"command":   "pwd",
		"directory": "/tmp",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "within the project root")
}

func TestShellToolDescriptionField(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]interface{}{
		"command":     "echo hello",
		"description": "Print hello to verify shell works",
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
}

func TestShellToolBackgroundExecution(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{})
	st := newTestShellTool(dir, 30*time.Second)
	st.SetProcessManager(pm)

	input, _ := json.Marshal(map[string]interface{}{
		"command":       "echo background_output && sleep 60",
		"is_background": true,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "process_id:")
	pm.Shutdown(context.Background())
}

func TestShellToolBackgroundBlockedByInterceptor(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{})
	st := newTestShellTool(dir, 30*time.Second)
	st.SetProcessManager(pm)
	defer pm.Shutdown(context.Background())

	// Command substitution should be blocked even in background mode
	input, _ := json.Marshal(map[string]interface{}{
		"command":       "echo $(whoami)",
		"is_background": true,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command substitution")
}

func TestShellToolBackgroundWithoutManager(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)
	// No ProcessManager set

	input, _ := json.Marshal(map[string]interface{}{
		"command":       "echo test",
		"is_background": true,
	})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "background")
}

func TestShellToolInputSchemaIncludesNewFields(t *testing.T) {
	dir := t.TempDir()
	st := newTestShellTool(dir, 30*time.Second)

	schema := st.InputSchema()
	schemaStr := string(schema)
	assert.Contains(t, schemaStr, "timeout")
	assert.Contains(t, schemaStr, "directory")
	assert.Contains(t, schemaStr, "description")
	assert.Contains(t, schemaStr, "is_background")
}

// --- Read-only command detection tests ---

func TestIsReadOnlyCommandLS(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("ls -la"))
}

func TestIsReadOnlyCommandGitStatus(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("git status"))
}

func TestIsReadOnlyCommandGitLog(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("git log --oneline"))
}

func TestIsReadOnlyCommandGitDiff(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("git diff HEAD"))
}

func TestIsReadOnlyCommandCat(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("cat foo.go"))
}

func TestIsReadOnlyCommandHead(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("head -n 10 foo.go"))
}

func TestIsReadOnlyCommandTail(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("tail -f server.log"))
}

func TestIsReadOnlyCommandFind(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("find . -name '*.go'"))
}

func TestIsReadOnlyCommandGrep(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("grep -r TODO ./src"))
}

func TestIsReadOnlyCommandWc(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("wc -l *.go"))
}

func TestIsReadOnlyCommandPwd(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("pwd"))
}

func TestIsReadOnlyCommandEcho(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("echo hello"))
}

func TestIsReadOnlyCommandRm(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("rm file.txt"))
}

func TestIsReadOnlyCommandMkdir(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("mkdir newdir"))
}

func TestIsReadOnlyCommandGitPush(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("git push origin main"))
}

func TestIsReadOnlyCommandGitCommit(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("git commit -m 'msg'"))
}

func TestIsReadOnlyCommandChmod(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("chmod +x script.sh"))
}

func TestIsReadOnlyCommandPipe(t *testing.T) {
	// A pipe where all commands are read-only should be read-only
	assert.True(t, IsReadOnlyCommand("ls | grep foo"))
}

func TestIsReadOnlyCommandPipeWithWrite(t *testing.T) {
	// A pipe with a write command should not be read-only
	assert.False(t, IsReadOnlyCommand("echo data | tee file.txt"))
}

func TestIsReadOnlyCommandEnvPrefix(t *testing.T) {
	// env prefix with read-only command
	assert.True(t, IsReadOnlyCommand("env FOO=1 ls"))
}

func TestIsReadOnlyCommandGitBranch(t *testing.T) {
	// git branch can create branches, so it's not read-only
	assert.False(t, IsReadOnlyCommand("git branch"))
}

func TestIsReadOnlyCommandGitShow(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("git show HEAD"))
}

func TestIsReadOnlyCommandAndSeparator(t *testing.T) {
	// "ls && rm -rf /" should NOT be read-only
	assert.False(t, IsReadOnlyCommand("ls && rm -rf /"))
}

func TestIsReadOnlyCommandSemicolonSeparator(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("ls; rm -rf /"))
}

func TestIsReadOnlyCommandOrSeparator(t *testing.T) {
	assert.False(t, IsReadOnlyCommand("ls || rm -rf /"))
}

func TestIsReadOnlyCommandAllReadOnlyWithAnd(t *testing.T) {
	assert.True(t, IsReadOnlyCommand("ls && pwd && echo ok"))
}

func TestIsReadOnlyCommandGitTag(t *testing.T) {
	// git tag can create tags, so it's not read-only
	assert.False(t, IsReadOnlyCommand("git tag"))
}

func TestIsReadOnlyCommandGitRemote(t *testing.T) {
	// git remote can add/remove remotes, so it's not read-only
	assert.False(t, IsReadOnlyCommand("git remote"))
}

func TestIsReadOnlyCommandBareGit(t *testing.T) {
	// bare "git" with no subcommand just prints help — safe
	assert.True(t, IsReadOnlyCommand("git"))
}
