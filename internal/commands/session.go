package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/store"
)

// --- sessions ---

type sessionsCommand struct {
	listSessions func() ([]store.Session, error)
}

// NewSessionsCommand creates a command that lists recent sessions.
func NewSessionsCommand(listSessions func() ([]store.Session, error)) SlashCommand {
	return &sessionsCommand{listSessions: listSessions}
}

func (c *sessionsCommand) Name() string                                       { return "sessions" }
func (c *sessionsCommand) Description() string                                { return "List recent sessions" }
func (c *sessionsCommand) Arguments() []ArgumentDef                           { return nil }
func (c *sessionsCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *sessionsCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.listSessions == nil {
		return Result{Output: "Session listing not available."}, nil
	}

	sessions, err := c.listSessions()
	if err != nil {
		return Result{}, err
	}
	if len(sessions) == 0 {
		return Result{Output: "No sessions found."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Recent sessions:\n")
	for _, s := range sessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		if s.ForkedFrom != "" {
			fid := s.ForkedFrom
			if len(fid) > 8 {
				fid = fid[:8]
			}
			title += fmt.Sprintf(" (forked from %s)", fid)
		}
		sid := s.ID
		if len(sid) > 8 {
			sid = sid[:8]
		}
		fmt.Fprintf(&sb, "  %s  %-30s  %s  %s\n",
			sid, title, s.Model, s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return Result{Output: sb.String()}, nil
}

// --- fork ---

type forkCommand struct {
	doFork func(ctx context.Context) (string, error)
}

// NewForkCommand creates a command that forks the current session.
func NewForkCommand(doFork func(ctx context.Context) (string, error)) SlashCommand {
	return &forkCommand{doFork: doFork}
}

func (c *forkCommand) Name() string                                       { return "fork" }
func (c *forkCommand) Description() string                                { return "Fork the current session" }
func (c *forkCommand) Arguments() []ArgumentDef                           { return nil }
func (c *forkCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *forkCommand) Execute(ctx context.Context, _ []string) (Result, error) {
	if c.doFork == nil {
		return Result{Output: "Session forking not available."}, nil
	}

	newID, err := c.doFork(ctx)
	if err != nil {
		return Result{}, err
	}

	return Result{Output: fmt.Sprintf("Forked current session → %s\nNew conversation continues on the fork.", newID)}, nil
}
