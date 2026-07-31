package subagents_test

import (
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/subagents"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOptions() subagents.Options {
	return subagents.Options{
		Config:     &config.Config{},
		Registry:   tools.NewRegistry(),
		EnableTask: true,
	}
}

func TestWireRequiresConfig(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.Config = nil
	_, err := subagents.Wire(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is required")
}

func TestWireRequiresRegistry(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.Registry = nil
	_, err := subagents.Wire(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool registry is required")
}

func TestWireRegistersTheBuiltinGeneralDefinition(t *testing.T) {
	t.Parallel()
	w, err := subagents.Wire(testOptions())
	require.NoError(t, err)
	def, ok := w.AgentDefs.Get("general")
	require.True(t, ok, "every session must be able to spawn a general-purpose subagent")
	assert.Contains(t, def.Description, "General-purpose")
}

func TestWireRegistersConfigDefinitions(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	inherit := true
	// Every field, with a distinct value: the translation is field-by-field
	// and a dropped one would otherwise go unnoticed.
	opts.Config.Agent.Definitions = []config.AgentDefConf{{
		Name:          "reviewer",
		Description:   "reviews code",
		SystemPrompt:  "review carefully",
		Tools:         []string{"read_file"},
		MaxTurns:      7,
		MaxDepth:      2,
		Model:         "test-model",
		InheritSkills: &inherit,
		ExtraSkills:   []string{"extra"},
		DisableSkills: []string{"disabled"},
	}}
	w, err := subagents.Wire(opts)
	require.NoError(t, err)
	def, ok := w.AgentDefs.Get("reviewer")
	require.True(t, ok)
	assert.Equal(t, "reviews code", def.Description)
	assert.Equal(t, "review carefully", def.SystemPrompt)
	assert.Equal(t, []string{"read_file"}, def.Tools)
	assert.Equal(t, 7, def.MaxTurns)
	assert.Equal(t, 2, def.MaxDepth)
	assert.Equal(t, "test-model", def.Model)
	require.NotNil(t, def.InheritSkills)
	assert.True(t, *def.InheritSkills)
	assert.Equal(t, []string{"extra"}, def.ExtraSkills)
	assert.Equal(t, []string{"disabled"}, def.DisableSkills)
}

// TestWireReportsUnregisterableDefinitions covers a config that names a
// definition twice, or reuses the built-in name. Silently dropping it leaves
// the user with a subagent that does not exist and no clue why.
func TestWireReportsUnregisterableDefinitions(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.Config.Agent.Definitions = []config.AgentDefConf{
		{Name: "general", Description: "shadowing the built-in"},
	}
	var logged []string
	opts.Logf = func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	_, err := subagents.Wire(opts)
	require.NoError(t, err, "a bad definition must not abort the session")
	require.NotEmpty(t, logged, "the user has to learn the definition was dropped")
	assert.Contains(t, logged[0], "general")
}

func TestWireToleratesANilLogger(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.Config.Agent.Definitions = []config.AgentDefConf{{Name: "general"}}
	opts.Logf = nil
	_, err := subagents.Wire(opts)
	assert.NoError(t, err)
}

// TestWireReusesTheSessionWorktreeManager pins the WorktreeManager option:
// a supplied manager is used as-is and the repository is not re-discovered.
// This is redundancy avoided, not a bug fixed — worktree.Manager keeps no
// state, so a second instance over the same root would have behaved
// identically.
func TestWireReusesTheSessionWorktreeManager(t *testing.T) {
	t.Parallel()
	session := worktree.NewManager(t.TempDir(), worktree.Config{MaxWorktrees: 3})

	opts := testOptions()
	opts.WorktreeManager = session
	opts.GitRoot = func() (string, error) {
		t.Fatal("GitRoot must not be consulted when a session manager was supplied")
		return "", nil
	}

	w, err := subagents.Wire(opts)
	require.NoError(t, err)
	require.NotNil(t, w.Spawner.WorktreeProvider)
}

func TestWireFallsBackToTheRepositoryRoot(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.GitRoot = func() (string, error) { return t.TempDir(), nil }

	w, err := subagents.Wire(opts)
	require.NoError(t, err)
	assert.NotNil(t, w.Spawner.WorktreeProvider,
		"subagents can use worktree isolation even when the session is not in one")
}

// TestWireSurvivesOutsideARepository: no worktree provider is a supported
// state, not a construction failure.
func TestWireSurvivesOutsideARepository(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.GitRoot = func() (string, error) { return "", fmt.Errorf("not a git repository") }

	w, err := subagents.Wire(opts)
	require.NoError(t, err)
	assert.Nil(t, w.Spawner.WorktreeProvider)
}

func TestWireRegistersOnlyTheEnabledTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		task, list         bool
		wantTask, wantList bool
	}{
		{"both", true, true, true, true},
		{"task only", true, false, true, false},
		{"list only", false, true, false, true},
		{"neither", false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := testOptions()
			opts.EnableTask, opts.EnableListTasks = tt.task, tt.list
			_, err := subagents.Wire(opts)
			require.NoError(t, err)
			names := map[string]bool{}
			for _, n := range opts.Registry.Names() {
				names[n] = true
			}
			assert.Equal(t, tt.wantTask, names["task"])
			assert.Equal(t, tt.wantList, names["list_tasks"])
		})
	}
}

// TestWireResolvesIsolationEvenWithNoTaskTools guards the ordering. The
// caller receives the spawner whether or not the task tools are registered,
// and a spawner that cannot isolate is a different object from one that can,
// so the worktree provider must not depend on the tool allowlist.
func TestWireResolvesIsolationEvenWithNoTaskTools(t *testing.T) {
	t.Parallel()
	opts := testOptions()
	opts.EnableTask, opts.EnableListTasks = false, false
	opts.GitRoot = func() (string, error) { return t.TempDir(), nil }

	w, err := subagents.Wire(opts)
	require.NoError(t, err)
	require.NotNil(t, w.AgentDefs)
	assert.NotNil(t, w.Spawner.WorktreeProvider,
		"isolation is a property of the spawner, not of which tools were registered")
}
