package testutil

import (
	"context"

	"github.com/julianshen/rubichan/internal/commands"
)

type StubSlashCommand struct {
	CommandName string
	Output      string
	LastArgs    []string
}

func (s *StubSlashCommand) Name() string { return s.CommandName }

func (s *StubSlashCommand) Description() string { return "stub" }

func (s *StubSlashCommand) Arguments() []commands.ArgumentDef { return nil }

func (s *StubSlashCommand) Complete(context.Context, []string) []commands.Candidate { return nil }

func (s *StubSlashCommand) Execute(_ context.Context, args []string) (commands.Result, error) {
	s.LastArgs = append([]string(nil), args...)
	return commands.Result{Output: s.Output}, nil
}
