package commands

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Quit Command ---

func TestQuitCommandName(t *testing.T) {
	cmd := NewQuitCommand()
	assert.Equal(t, "quit", cmd.Name())
}

func TestQuitCommandDescription(t *testing.T) {
	cmd := NewQuitCommand()
	assert.NotEmpty(t, cmd.Description())
}

func TestQuitCommandExecute(t *testing.T) {
	cmd := NewQuitCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionQuit, result.Action)
}

// --- Exit Command ---

func TestExitCommandName(t *testing.T) {
	cmd := NewExitCommand()
	assert.Equal(t, "exit", cmd.Name())
}

func TestExitCommandDescription(t *testing.T) {
	cmd := NewExitCommand()
	assert.NotEmpty(t, cmd.Description())
}

func TestExitCommandExecute(t *testing.T) {
	cmd := NewExitCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionQuit, result.Action)
}

// --- Clear Command ---

func TestClearCommandName(t *testing.T) {
	cmd := NewClearCommand(func() {})
	assert.Equal(t, "clear", cmd.Name())
}

func TestClearCommandDescription(t *testing.T) {
	cmd := NewClearCommand(func() {})
	assert.NotEmpty(t, cmd.Description())
}

func TestClearCommandExecuteCallsCallback(t *testing.T) {
	called := false
	cmd := NewClearCommand(func() { called = true })

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, ActionNone, result.Action)
}

// --- Model Command ---

func TestModelCommandName(t *testing.T) {
	cmd := NewModelCommand(func(string) {})
	assert.Equal(t, "model", cmd.Name())
}

func TestModelCommandDescription(t *testing.T) {
	cmd := NewModelCommand(func(string) {})
	assert.NotEmpty(t, cmd.Description())
}

func TestModelCommandArguments(t *testing.T) {
	cmd := NewModelCommand(func(string) {})
	args := cmd.Arguments()
	require.Len(t, args, 1)
	assert.Equal(t, "name", args[0].Name)
	assert.False(t, args[0].Required)
}

func TestModelCommandExecute(t *testing.T) {
	var switched string
	cmd := NewModelCommand(func(name string) { switched = name })

	result, err := cmd.Execute(context.Background(), []string{"gpt-4o"})
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", switched)
	assert.Contains(t, result.Output, "gpt-4o")
	assert.Equal(t, ActionNone, result.Action)
}

func TestModelCommandExecuteNoArgsOpensPicker(t *testing.T) {
	cmd := NewModelCommand(func(string) {})

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionOpenModelPicker, result.Action)
}

func TestModelCommandExecuteEmptyArgsOpensPicker(t *testing.T) {
	cmd := NewModelCommand(func(string) {})

	result, err := cmd.Execute(context.Background(), []string{})
	require.NoError(t, err)
	assert.Equal(t, ActionOpenModelPicker, result.Action)
}

// --- Config Command ---

func TestConfigCommandName(t *testing.T) {
	cmd := NewConfigCommand()
	assert.Equal(t, "config", cmd.Name())
}

func TestConfigCommandDescription(t *testing.T) {
	cmd := NewConfigCommand()
	assert.NotEmpty(t, cmd.Description())
}

func TestConfigCommandExecute(t *testing.T) {
	cmd := NewConfigCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionOpenConfig, result.Action)
}

// --- Help Command ---

func TestHelpCommandName(t *testing.T) {
	reg := NewRegistry()
	cmd := NewHelpCommand(reg)
	assert.Equal(t, "help", cmd.Name())
}

func TestHelpCommandDescription(t *testing.T) {
	reg := NewRegistry()
	cmd := NewHelpCommand(reg)
	assert.NotEmpty(t, cmd.Description())
}

func TestHelpCommandExecuteListsCommands(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "quit", desc: "quit the app"}))
	require.NoError(t, reg.Register(&stubCommand{name: "help", desc: "show help"}))

	cmd := NewHelpCommand(reg)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "help")
	assert.Contains(t, result.Output, "quit")
	assert.Contains(t, result.Output, "show help")
	assert.Contains(t, result.Output, "quit the app")
	assert.Equal(t, ActionNone, result.Action)
}

func TestHelpCommandExecuteEmptyRegistry(t *testing.T) {
	reg := NewRegistry()
	cmd := NewHelpCommand(reg)

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No commands")
}

// --- Arguments and Complete return nil for commands without them ---

func TestQuitCommandArgumentsAndComplete(t *testing.T) {
	cmd := NewQuitCommand()
	assert.Nil(t, cmd.Arguments())
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestExitCommandArgumentsAndComplete(t *testing.T) {
	cmd := NewExitCommand()
	assert.Nil(t, cmd.Arguments())
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestClearCommandArgumentsAndComplete(t *testing.T) {
	cmd := NewClearCommand(func() {})
	assert.Nil(t, cmd.Arguments())
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestModelCommandComplete(t *testing.T) {
	cmd := NewModelCommand(func(string) {})
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestConfigCommandArgumentsAndComplete(t *testing.T) {
	cmd := NewConfigCommand()
	assert.Nil(t, cmd.Arguments())
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestHelpCommandArgumentsAndComplete(t *testing.T) {
	cmd := NewHelpCommand(NewRegistry())
	assert.Nil(t, cmd.Arguments())
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestDebugVerificationSnapshotCommand(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(func() string {
		return "Verification snapshot:\n- verdict: passed"
	})

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Verification snapshot")
	assert.Contains(t, result.Output, "verdict: passed")
}

func TestDebugVerificationSnapshotCommandUnavailable(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(func() string { return "" })

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "unavailable")
}

// --- Interface conformance ---

func TestBuiltinCommandsImplementSlashCommand(t *testing.T) {
	var _ SlashCommand = NewQuitCommand()
	var _ SlashCommand = NewExitCommand()
	var _ SlashCommand = NewClearCommand(func() {})
	var _ SlashCommand = NewModelCommand(func(string) {})
	var _ SlashCommand = NewConfigCommand()
	var _ SlashCommand = NewHelpCommand(NewRegistry())
	var _ SlashCommand = NewDebugVerificationSnapshotCommand(nil)
	var _ SlashCommand = NewRalphLoopCommand(nil)
	var _ SlashCommand = NewCancelRalphCommand(nil)
	var _ SlashCommand = NewResumeCommand()
}

// --- Resume Command ---

func TestResumeCommandName(t *testing.T) {
	cmd := NewResumeCommand()
	assert.Equal(t, "resume", cmd.Name())
}

func TestResumeCommandExecute(t *testing.T) {
	cmd := NewResumeCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionResume, result.Action)
}

func TestRalphLoopCommandExecute(t *testing.T) {
	var got RalphLoopConfig
	cmd := NewRalphLoopCommand(func(cfg RalphLoopConfig) error {
		got = cfg
		return nil
	})

	result, err := cmd.Execute(context.Background(), []string{
		"finish", "the", "feature",
		"--completion-promise", "DONE",
		"--max-iterations", "4",
	})
	require.NoError(t, err)
	assert.Equal(t, "finish the feature", got.Prompt)
	assert.Equal(t, "DONE", got.CompletionPromise)
	assert.Equal(t, 4, got.MaxIterations)
	assert.Contains(t, result.Output, "Ralph loop started")
}

func TestRalphLoopCommandRequiresCompletionPromise(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)

	_, err := cmd.Execute(context.Background(), []string{"finish", "it"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--completion-promise")
}

func TestRalphLoopCommandErrorPaths(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "non integer max iterations",
			args: []string{"prompt", "--completion-promise", "DONE", "--max-iterations", "abc"},
			want: "positive integer",
		},
		{
			name: "zero max iterations",
			args: []string{"prompt", "--completion-promise", "DONE", "--max-iterations", "0"},
			want: "positive integer",
		},
		{
			name: "negative max iterations",
			args: []string{"prompt", "--completion-promise", "DONE", "--max-iterations", "-1"},
			want: "positive integer",
		},
		{
			name: "unknown flag",
			args: []string{"prompt", "--completion-promise", "DONE", "--unknown"},
			want: "unknown flag",
		},
		{
			name: "missing prompt",
			args: []string{"--completion-promise", "DONE"},
			want: "prompt is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cmd.Execute(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCancelRalphCommandExecute(t *testing.T) {
	called := false
	cmd := NewCancelRalphCommand(func() bool {
		called = true
		return true
	})

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Contains(t, result.Output, "cancelled")
}

func TestCancelRalphCommandExecuteWithoutActiveLoop(t *testing.T) {
	cmd := NewCancelRalphCommand(func() bool { return false })

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No active Ralph loop")
}

func TestAboutCommandName(t *testing.T) {
	cmd := NewAboutCommand()
	assert.Equal(t, "about", cmd.Name())
}

func TestAboutCommandDescription(t *testing.T) {
	cmd := NewAboutCommand()
	assert.NotEmpty(t, cmd.Description())
	assert.Contains(t, cmd.Description(), "Rubichan")
}

func TestAboutCommandArguments(t *testing.T) {
	cmd := NewAboutCommand()
	assert.Nil(t, cmd.Arguments())
}

func TestAboutCommandComplete(t *testing.T) {
	cmd := NewAboutCommand()
	candidates := cmd.Complete(context.Background(), []string{})
	assert.Nil(t, candidates)
}

func TestAboutCommandExecute(t *testing.T) {
	cmd := NewAboutCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionOpenAbout, result.Action)
	assert.Empty(t, result.Output)
}

// --- InitKnowledgeGraph Command ---

func TestInitKnowledgeGraphCommandName(t *testing.T) {
	cmd := NewInitKnowledgeGraphCommand()
	assert.Equal(t, "initknowledgegraph", cmd.Name())
}

func TestInitKnowledgeGraphCommandDescription(t *testing.T) {
	cmd := NewInitKnowledgeGraphCommand()
	assert.NotEmpty(t, cmd.Description())
	assert.Contains(t, cmd.Description(), "knowledge graph")
}

func TestInitKnowledgeGraphCommandArguments(t *testing.T) {
	cmd := NewInitKnowledgeGraphCommand()
	assert.Nil(t, cmd.Arguments())
}

func TestInitKnowledgeGraphCommandComplete(t *testing.T) {
	cmd := NewInitKnowledgeGraphCommand()
	candidates := cmd.Complete(context.Background(), []string{})
	assert.Nil(t, candidates)
}

func TestInitKnowledgeGraphCommandExecute(t *testing.T) {
	cmd := NewInitKnowledgeGraphCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionInitKnowledgeGraph, result.Action)
	assert.Empty(t, result.Output)
}

// --- Undo Overlay Command ---

func TestUndoOverlayCommandName(t *testing.T) {
	cmd := NewUndoOverlayCommand()
	assert.Equal(t, "undo", cmd.Name())
}

func TestUndoOverlayCommandDescription(t *testing.T) {
	cmd := NewUndoOverlayCommand()
	assert.NotEmpty(t, cmd.Description())
}

func TestUndoOverlayCommandArguments(t *testing.T) {
	cmd := NewUndoOverlayCommand()
	assert.Nil(t, cmd.Arguments())
}

func TestUndoOverlayCommandComplete(t *testing.T) {
	cmd := NewUndoOverlayCommand()
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestUndoOverlayCommandExecute(t *testing.T) {
	cmd := NewUndoOverlayCommand()
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionOpenUndo, result.Action)
}

// --- Debug Verification Snapshot Command full coverage ---

func TestDebugVerificationSnapshotCommandName(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(nil)
	assert.Equal(t, "debug-verification-snapshot", cmd.Name())
}

func TestDebugVerificationSnapshotCommandDescription(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(nil)
	assert.NotEmpty(t, cmd.Description())
}

func TestDebugVerificationSnapshotCommandArguments(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(nil)
	assert.Nil(t, cmd.Arguments())
}

func TestDebugVerificationSnapshotCommandComplete(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(nil)
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestDebugVerificationSnapshotCommandNilCallback(t *testing.T) {
	cmd := NewDebugVerificationSnapshotCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "unavailable")
}

// --- Resume Command full coverage ---

func TestResumeCommandDescription(t *testing.T) {
	cmd := NewResumeCommand()
	assert.NotEmpty(t, cmd.Description())
}

func TestResumeCommandArguments(t *testing.T) {
	cmd := NewResumeCommand()
	assert.Nil(t, cmd.Arguments())
}

func TestResumeCommandComplete(t *testing.T) {
	cmd := NewResumeCommand()
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

// --- Ralph Loop Command metadata ---

func TestRalphLoopCommandName(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	assert.Equal(t, "ralph-loop", cmd.Name())
}

func TestRalphLoopCommandDescription(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	assert.NotEmpty(t, cmd.Description())
}

func TestRalphLoopCommandArguments(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	args := cmd.Arguments()
	require.NotEmpty(t, args)
}

func TestRalphLoopCommandComplete(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

// --- Cancel Ralph Command metadata ---

func TestCancelRalphCommandName(t *testing.T) {
	cmd := NewCancelRalphCommand(nil)
	assert.Equal(t, "cancel-ralph", cmd.Name())
}

func TestCancelRalphCommandDescription(t *testing.T) {
	cmd := NewCancelRalphCommand(nil)
	assert.NotEmpty(t, cmd.Description())
}

func TestCancelRalphCommandArguments(t *testing.T) {
	cmd := NewCancelRalphCommand(nil)
	assert.Nil(t, cmd.Arguments())
}

func TestCancelRalphCommandComplete(t *testing.T) {
	cmd := NewCancelRalphCommand(nil)
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

// --- formatNum negative numbers ---

func TestFormatNumNegative(t *testing.T) {
	// Exercise formatNum with a negative number to cover the n < 0 branch.
	// We use context command with a budget that triggers negative values
	// indirectly through the formatNum path.
	budget := agentsdk.ContextBudget{
		Total:            100,
		MaxOutputTokens:  0,
		SystemPrompt:     50,
		SkillPrompts:     50,
		ToolDescriptions: 50,
		Conversation:     50,
	}
	cmd := NewContextCommand(func() agentsdk.ContextBudget { return budget })
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	// The remaining tokens will be negative, triggering the negative formatNum path.
	assert.Contains(t, result.Output, "Context Usage:")
}

// --- CancelRalph with nil canceler ---

func TestCancelRalphCommandNilCanceler(t *testing.T) {
	cmd := NewCancelRalphCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No active Ralph loop")
}

// --- Ralph loop with --max-iterations missing value ---

func TestRalphLoopCommandMaxIterationsMissingValue(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	_, err := cmd.Execute(context.Background(), []string{"prompt", "--completion-promise", "DONE", "--max-iterations"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a value")
}

// --- Ralph loop with --completion-promise missing value ---

func TestRalphLoopCommandCompletionPromiseMissingValue(t *testing.T) {
	cmd := NewRalphLoopCommand(nil)
	_, err := cmd.Execute(context.Background(), []string{"prompt", "--completion-promise"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a value")
}

// --- Ralph loop starter returns error ---

func TestRalphLoopCommandStarterError(t *testing.T) {
	cmd := NewRalphLoopCommand(func(cfg RalphLoopConfig) error {
		return fmt.Errorf("loop already active")
	})
	_, err := cmd.Execute(context.Background(), []string{"test", "--completion-promise", "DONE"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop already active")
}
