package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/julianshen/rubichan/internal/checkpoint"
)

// --- undo ---

type undoCommand struct {
	mgr *checkpoint.Manager
}

// NewUndoCommand creates a command that reverts the last file edit via checkpoint.
func NewUndoCommand(mgr *checkpoint.Manager) SlashCommand {
	return &undoCommand{mgr: mgr}
}

func (c *undoCommand) Name() string        { return "undo" }
func (c *undoCommand) Description() string { return "Undo the last file edit" }
func (c *undoCommand) Arguments() []ArgumentDef {
	return nil
}
func (c *undoCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *undoCommand) Execute(ctx context.Context, _ []string) (Result, error) {
	if c.mgr == nil {
		return Result{Output: "Checkpoints not available."}, nil
	}
	path, err := c.mgr.Undo(ctx)
	if err != nil {
		if errors.Is(err, checkpoint.ErrNoCheckpoints) {
			return Result{Output: "No checkpoints to undo."}, nil
		}
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("Reverted %s", path)}, nil
}

// --- rewind ---

type rewindCommand struct {
	mgr *checkpoint.Manager
}

// NewRewindCommand creates a command that reverts all file edits after turn N.
func NewRewindCommand(mgr *checkpoint.Manager) SlashCommand {
	return &rewindCommand{mgr: mgr}
}

func (c *rewindCommand) Name() string        { return "rewind" }
func (c *rewindCommand) Description() string { return "Rewind all edits after turn N" }
func (c *rewindCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{{Name: "turn", Description: "Turn number to rewind to", Required: true}}
}
func (c *rewindCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *rewindCommand) Execute(ctx context.Context, args []string) (Result, error) {
	if len(args) == 0 {
		return Result{}, fmt.Errorf("turn number is required: /rewind N")
	}
	turn, err := strconv.Atoi(args[0])
	if err != nil {
		return Result{}, fmt.Errorf("invalid turn number: %s", args[0])
	}
	if c.mgr == nil {
		return Result{Output: "Checkpoints not available."}, nil
	}
	paths, err := c.mgr.RewindToTurn(ctx, turn)
	if err != nil {
		return Result{}, err
	}
	if len(paths) == 0 {
		return Result{Output: "No checkpoints to rewind."}, nil
	}
	return Result{Output: fmt.Sprintf("Reverted %d file(s):\n  - %s", len(paths), strings.Join(paths, "\n  - "))}, nil
}
