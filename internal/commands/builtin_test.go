package commands

import (
	"context"
	"testing"

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
	assert.True(t, args[0].Required)
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

func TestModelCommandExecuteNoArgs(t *testing.T) {
	cmd := NewModelCommand(func(string) {})

	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model name")
}

func TestModelCommandExecuteEmptyArgs(t *testing.T) {
	cmd := NewModelCommand(func(string) {})

	_, err := cmd.Execute(context.Background(), []string{})
	assert.Error(t, err)
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

// --- Interface conformance ---

func TestBuiltinCommandsImplementSlashCommand(t *testing.T) {
	var _ SlashCommand = NewQuitCommand()
	var _ SlashCommand = NewExitCommand()
	var _ SlashCommand = NewClearCommand(func() {})
	var _ SlashCommand = NewModelCommand(func(string) {})
	var _ SlashCommand = NewConfigCommand()
	var _ SlashCommand = NewHelpCommand(NewRegistry())
}
