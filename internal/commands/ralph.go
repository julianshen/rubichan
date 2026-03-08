package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const defaultRalphMaxIterations = 10

// RalphLoopConfig captures the loop behavior for the built-in Ralph mode.
type RalphLoopConfig struct {
	Prompt            string
	MaxIterations     int
	CompletionPromise string
}

// RalphLoopStarter starts the loop in the host environment.
type RalphLoopStarter func(cfg RalphLoopConfig) error

// RalphLoopCanceler stops the loop in the host environment.
type RalphLoopCanceler func() bool

type ralphLoopCommand struct {
	start RalphLoopStarter
}

// NewRalphLoopCommand creates the built-in Ralph loop command.
func NewRalphLoopCommand(start RalphLoopStarter) SlashCommand {
	return &ralphLoopCommand{start: start}
}

func (c *ralphLoopCommand) Name() string { return "ralph-loop" }

func (c *ralphLoopCommand) Description() string {
	return "Repeat the same prompt until a completion promise is observed"
}

func (c *ralphLoopCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{
		{Name: "prompt", Description: "Prompt to repeat on each iteration", Required: true},
		{Name: "--completion-promise", Description: "String that ends the loop", Required: true},
		{Name: "--max-iterations", Description: "Maximum loop count (default: 10)"},
	}
}

func (c *ralphLoopCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *ralphLoopCommand) Execute(_ context.Context, args []string) (Result, error) {
	cfg, err := parseRalphLoopArgs(args)
	if err != nil {
		return Result{}, err
	}
	if c.start != nil {
		if err := c.start(cfg); err != nil {
			return Result{}, err
		}
	}
	return Result{
		Output: fmt.Sprintf(
			"Ralph loop started: max %d iterations, completion promise %q",
			cfg.MaxIterations,
			cfg.CompletionPromise,
		),
	}, nil
}

type cancelRalphCommand struct {
	cancel RalphLoopCanceler
}

// NewCancelRalphCommand creates the built-in Ralph loop cancellation command.
func NewCancelRalphCommand(cancel RalphLoopCanceler) SlashCommand {
	return &cancelRalphCommand{cancel: cancel}
}

func (c *cancelRalphCommand) Name() string        { return "cancel-ralph" }
func (c *cancelRalphCommand) Description() string { return "Cancel the active Ralph loop" }
func (c *cancelRalphCommand) Arguments() []ArgumentDef {
	return nil
}
func (c *cancelRalphCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *cancelRalphCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.cancel != nil && c.cancel() {
		return Result{Output: "Ralph loop cancelled."}, nil
	}
	return Result{Output: "No active Ralph loop."}, nil
}

func parseRalphLoopArgs(args []string) (RalphLoopConfig, error) {
	cfg := RalphLoopConfig{MaxIterations: defaultRalphMaxIterations}
	var promptParts []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--completion-promise":
			i++
			if i >= len(args) {
				return RalphLoopConfig{}, fmt.Errorf("--completion-promise requires a value")
			}
			cfg.CompletionPromise = args[i]
		case "--max-iterations":
			i++
			if i >= len(args) {
				return RalphLoopConfig{}, fmt.Errorf("--max-iterations requires a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return RalphLoopConfig{}, fmt.Errorf("--max-iterations must be a positive integer")
			}
			cfg.MaxIterations = n
		default:
			if strings.HasPrefix(arg, "--") {
				return RalphLoopConfig{}, fmt.Errorf("unknown flag: %s", arg)
			}
			promptParts = append(promptParts, arg)
		}
	}

	cfg.Prompt = strings.TrimSpace(strings.Join(promptParts, " "))
	if cfg.Prompt == "" {
		return RalphLoopConfig{}, fmt.Errorf("prompt is required")
	}
	if cfg.CompletionPromise == "" {
		return RalphLoopConfig{}, fmt.Errorf("--completion-promise is required")
	}
	return cfg, nil
}
