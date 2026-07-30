package subagents

// In-package: these exercise unexported adapters, which are wiring details
// rather than part of this package's contract.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentDefLookupAdapter_Found(t *testing.T) {
	t.Parallel()
	reg := agent.NewAgentDefRegistry()
	_ = reg.Register(&agent.AgentDef{
		Name:        "test-agent",
		Description: "A test agent",
		MaxTurns:    10,
	})
	adapter := &agentDefLookupAdapter{reg: reg}

	def, ok := adapter.GetAgentDef("test-agent")
	assert.True(t, ok)
	require.NotNil(t, def)
	assert.Equal(t, "test-agent", def.Name)
	assert.Equal(t, 10, def.MaxTurns)
}

func TestAgentDefLookupAdapter_NotFound(t *testing.T) {
	t.Parallel()
	reg := agent.NewAgentDefRegistry()
	adapter := &agentDefLookupAdapter{reg: reg}

	def, ok := adapter.GetAgentDef("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, def)
}

func TestWakeManagerAdapter_SubmitAndComplete(t *testing.T) {
	t.Parallel()
	wm := agent.NewWakeManager()
	adapter := &wakeManagerAdapter{wm: wm}

	taskID := adapter.SubmitBackground("test-task", func() {})
	assert.NotEmpty(t, taskID)

	// Before completion, task should be pending/running.
	statuses := wm.Status()
	require.Len(t, statuses, 1)
	assert.Equal(t, taskID, statuses[0].ID)
	assert.Equal(t, "running", statuses[0].Status)

	// Complete the task and drain the event.
	adapter.CompleteBackground(taskID, "done", nil)
	<-wm.Events()

	// After completion, task should be removed from pending.
	statuses = wm.Status()
	assert.Empty(t, statuses)
}

func TestWakeStatusAdapter_EmptyStatus(t *testing.T) {
	t.Parallel()
	wm := agent.NewWakeManager()
	adapter := &wakeStatusAdapter{wm: wm}

	statuses := adapter.BackgroundTaskStatus()
	assert.Empty(t, statuses)
}

func TestWakeStatusAdapter_WithTasks(t *testing.T) {
	t.Parallel()
	wm := agent.NewWakeManager()
	adapter := &wakeStatusAdapter{wm: wm}

	wm.Submit("task-a", func() {})
	wm.Submit("task-b", func() {})

	statuses := adapter.BackgroundTaskStatus()
	assert.Len(t, statuses, 2)
}

func TestWorktreeProviderAdapter_HasChangesAndRemove_NotFound(t *testing.T) {
	t.Parallel()
	// A throwaway repository, so the test does not depend on this checkout
	// having git history — the same trap slice 4 hit.
	mgr := worktree.NewManager(testRepo(t), worktree.Config{})
	adapter := &worktreeProviderAdapter{mgr: mgr}

	// HasWorktreeChanges on a non-existent worktree should error.
	_, err := adapter.HasWorktreeChanges(context.Background(), "nonexistent-worktree-xyz")
	assert.Error(t, err)

	// RemoveWorktree on non-existent should also error.
	err = adapter.RemoveWorktree(context.Background(), "nonexistent-worktree-xyz")
	assert.Error(t, err)
}

func TestSpawnerAdapter_SpawnRequiresProvider(t *testing.T) {
	t.Parallel()
	spawner := &agent.DefaultSubagentSpawner{
		Config:    config.DefaultConfig(),
		AgentDefs: agent.NewAgentDefRegistry(),
	}
	adapter := &spawnerAdapter{spawner: spawner}
	_, err := adapter.Spawn(context.Background(), tools.TaskSpawnConfig{
		Name: "test-task",
	}, "do something")
	assert.Error(t, err)
}

// testRepo builds a throwaway repository with one commit.
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x\n"), 0o600))
	run("add", "file.txt")
	run("commit", "--quiet", "-m", "initial commit")
	return dir
}
