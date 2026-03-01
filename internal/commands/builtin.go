package commands

import (
	"context"
	"fmt"
	"strings"
)

// --- quit ---

type quitCommand struct{}

// NewQuitCommand creates a command that requests the host to terminate.
func NewQuitCommand() SlashCommand {
	return &quitCommand{}
}

func (c *quitCommand) Name() string        { return "quit" }
func (c *quitCommand) Description() string { return "Quit the application" }
func (c *quitCommand) Arguments() []ArgumentDef {
	return nil
}

func (c *quitCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *quitCommand) Execute(_ context.Context, _ []string) (Result, error) {
	return Result{Action: ActionQuit}, nil
}

// --- exit ---

type exitCommand struct{}

// NewExitCommand creates a command that requests the host to terminate.
// It behaves identically to quit.
func NewExitCommand() SlashCommand {
	return &exitCommand{}
}

func (c *exitCommand) Name() string        { return "exit" }
func (c *exitCommand) Description() string { return "Exit the application" }
func (c *exitCommand) Arguments() []ArgumentDef {
	return nil
}

func (c *exitCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *exitCommand) Execute(_ context.Context, _ []string) (Result, error) {
	return Result{Action: ActionQuit}, nil
}

// --- clear ---

type clearCommand struct {
	onClear func()
}

// NewClearCommand creates a command that invokes the given callback to clear
// the conversation display.
func NewClearCommand(onClear func()) SlashCommand {
	return &clearCommand{onClear: onClear}
}

func (c *clearCommand) Name() string        { return "clear" }
func (c *clearCommand) Description() string { return "Clear the conversation" }
func (c *clearCommand) Arguments() []ArgumentDef {
	return nil
}

func (c *clearCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *clearCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.onClear != nil {
		c.onClear()
	}
	return Result{}, nil
}

// --- model ---

type modelCommand struct {
	onSwitch func(name string)
}

// NewModelCommand creates a command that switches the active LLM model.
// It requires one argument: the model name.
func NewModelCommand(onSwitch func(name string)) SlashCommand {
	return &modelCommand{onSwitch: onSwitch}
}

func (c *modelCommand) Name() string        { return "model" }
func (c *modelCommand) Description() string { return "Switch the active model" }
func (c *modelCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{
		{
			Name:        "name",
			Description: "Model name to switch to",
			Required:    true,
		},
	}
}

func (c *modelCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *modelCommand) Execute(_ context.Context, args []string) (Result, error) {
	if len(args) == 0 {
		return Result{}, fmt.Errorf("model name is required")
	}
	name := args[0]
	if c.onSwitch != nil {
		c.onSwitch(name)
	}
	return Result{Output: fmt.Sprintf("Model switched to %s", name)}, nil
}

// --- config ---

type configCommand struct{}

// NewConfigCommand creates a command that requests the host to open
// the configuration editor.
func NewConfigCommand() SlashCommand {
	return &configCommand{}
}

func (c *configCommand) Name() string        { return "config" }
func (c *configCommand) Description() string { return "Open configuration" }
func (c *configCommand) Arguments() []ArgumentDef {
	return nil
}

func (c *configCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *configCommand) Execute(_ context.Context, _ []string) (Result, error) {
	return Result{Action: ActionOpenConfig}, nil
}

// --- help ---

type helpCommand struct {
	registry *Registry
}

// NewHelpCommand creates a command that lists all registered commands
// with their descriptions.
func NewHelpCommand(registry *Registry) SlashCommand {
	return &helpCommand{registry: registry}
}

func (c *helpCommand) Name() string        { return "help" }
func (c *helpCommand) Description() string { return "Show available commands" }
func (c *helpCommand) Arguments() []ArgumentDef {
	return nil
}

func (c *helpCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *helpCommand) Execute(_ context.Context, _ []string) (Result, error) {
	cmds := c.registry.All()
	if len(cmds) == 0 {
		return Result{Output: "No commands available."}, nil
	}

	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, cmd := range cmds {
		fmt.Fprintf(&b, "  /%s — %s\n", cmd.Name(), cmd.Description())
	}
	return Result{Output: b.String()}, nil
}
