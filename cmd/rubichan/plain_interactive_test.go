package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSkillSummaryProvider struct {
	summaries []skills.SkillSummary
}

func (s *stubSkillSummaryProvider) GetAllSkillSummaries() []skills.SkillSummary {
	return s.summaries
}

func TestPlainInteractiveStatusLineIncludesSkills(t *testing.T) {
	host := newPlainInteractiveHost(bytes.NewBufferString(""), &bytes.Buffer{}, "gpt-test", 20, commands.NewRegistry())
	host.activeSkills = []string{"alpha", "beta", "gamma"}

	line := host.statusLine()
	assert.Contains(t, line, "gpt-test")
	assert.Contains(t, line, "Turn 0/20")
	assert.Contains(t, line, "Skills: 3 active (alpha, beta, +1)")
}

func TestPlainInteractiveApprovalCachesAlwaysApproveForNonDestructiveTools(t *testing.T) {
	in := bytes.NewBufferString("a\n")
	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(in, out, "gpt-test", 20, commands.NewRegistry())

	fn := host.MakeApprovalFunc()
	approved, err := fn(context.Background(), "file", json.RawMessage(`{"operation":"read","path":"README.md"}`))
	require.NoError(t, err)
	assert.True(t, approved)
	assert.Equal(t, agent.AutoApproved, host.CheckApproval("file", json.RawMessage(`{"operation":"read","path":"README.md"}`)))
}

func TestPlainInteractiveApprovalDoesNotCacheAlwaysForDestructiveTools(t *testing.T) {
	in := bytes.NewBufferString("a\n")
	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(in, out, "gpt-test", 20, commands.NewRegistry())

	fn := host.MakeApprovalFunc()
	approved, err := fn(context.Background(), "shell", json.RawMessage(`{"command":"rm -rf dist"}`))
	require.NoError(t, err)
	assert.True(t, approved)
	assert.Equal(t, agent.ApprovalRequired, host.CheckApproval("shell", json.RawMessage(`{"command":"rm -rf dist"}`)))
}

func TestPlainInteractiveHandleCommandRefreshesSkillState(t *testing.T) {
	reg := commands.NewRegistry()
	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(bytes.NewBufferString(""), out, "gpt-test", 20, reg)
	provider := &stubSkillSummaryProvider{
		summaries: []skills.SkillSummary{{Name: "app-generation-workflow", State: skills.SkillStateInactive}},
	}
	host.SetSkillRuntime(provider)

	require.NoError(t, reg.Register(commands.NewClearCommand(func() {
		provider.summaries[0].State = skills.SkillStateActive
	})))

	quit, err := host.handleCommand(context.Background(), "/clear")
	require.NoError(t, err)
	assert.False(t, quit)
	assert.Equal(t, []string{"app-generation-workflow"}, host.activeSkills)
	assert.Contains(t, out.String(), `Skill "app-generation-workflow" activated.`)
}

func TestPlainInteractiveHandleCommandEmitsSessionEvent(t *testing.T) {
	reg := commands.NewRegistry()
	require.NoError(t, reg.Register(commands.NewHelpCommand(reg)))
	host := newPlainInteractiveHost(bytes.NewBufferString(""), &bytes.Buffer{}, "gpt-test", 20, reg)
	var events []session.Event
	host.eventSink = session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	})

	quit, err := host.handleCommand(context.Background(), "/help")
	require.NoError(t, err)
	assert.False(t, quit)
	require.Len(t, events, 1)
	assert.Equal(t, session.EventTypeCommandResult, events[0].Type)
	require.NotNil(t, events[0].Command)
	assert.Equal(t, "/help", events[0].Command.Command)
}

func TestPlainInteractiveRunRewritesInlineSkillDirective(t *testing.T) {
	reg := commands.NewRegistry()
	stub := &testutil.StubSlashCommand{CommandName: "skill", Output: "Skill \"brainstorming\" activated."}
	require.NoError(t, reg.Register(stub))

	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(bytes.NewBufferString("__skill({\"name\":\"brainstorming\"})\n"), out, "gpt-test", 20, reg)

	err := host.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"activate", "brainstorming"}, stub.LastArgs)
	assert.Contains(t, out.String(), `Inline skill directive: activate "brainstorming"`)
	assert.Contains(t, out.String(), `Skill "brainstorming" activated.`)
}

func TestPlainInteractiveRunRewritesSkillDirectiveAlias(t *testing.T) {
	reg := commands.NewRegistry()
	stub := &testutil.StubSlashCommand{CommandName: "skill", Output: "Skill \"brainstorming\" activated."}
	require.NoError(t, reg.Register(stub))

	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(bytes.NewBufferString("skill({\"tool\":\"brainstorming\"})\n"), out, "gpt-test", 20, reg)

	err := host.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"activate", "brainstorming"}, stub.LastArgs)
	assert.Contains(t, out.String(), `Inline skill directive: activate "brainstorming"`)
	assert.Contains(t, out.String(), `Skill "brainstorming" activated.`)
}

func TestDiffStringSet(t *testing.T) {
	activated, deactivated := diffStringSet(
		[]string{"alpha", "beta"},
		[]string{"beta", "gamma"},
	)
	assert.Equal(t, []string{"gamma"}, activated)
	assert.Equal(t, []string{"alpha"}, deactivated)
}

func TestPlainInteractiveDebugVerificationSnapshot(t *testing.T) {
	host := newPlainInteractiveHost(bytes.NewBufferString(""), &bytes.Buffer{}, "gpt-test", 20, commands.NewRegistry())
	host.sessionState = session.NewState()
	host.sessionState.ResetForPrompt("Create a backend-only todo API using Node.js and SQLite")
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "1", Name: "shell", Input: json.RawMessage(`{"command":"npm install express better-sqlite3"}`)},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "1", Name: "shell", Content: "added 10 packages"},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "2", Name: "file", Input: json.RawMessage(`{"operation":"write","path":"schema.sql"}`)},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "2", Name: "file", Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)"},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "3", Name: "process", Input: json.RawMessage(`{"operation":"exec","command":"node index.js"}`)},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "3", Name: "process", Content: "Todo API server listening on http://localhost:3000"},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "4", Name: "shell", Input: json.RawMessage(`{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/todos"}`)},
	})
	host.sessionState.ApplyEvent(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "4", Name: "shell", Content: `{"id":1,"title":"Test Todo","completed":false}
[{"id":1,"title":"Test Todo","completed":false}]`},
	})

	snapshot := host.DebugVerificationSnapshot()
	assert.Contains(t, snapshot, "verdict: passed")
	assert.Contains(t, snapshot, "api round-trip: true")
}

func TestPlainInteractiveReadLineCtxCancelled(t *testing.T) {
	host := newPlainInteractiveHost(bytes.NewBufferString(""), &bytes.Buffer{}, "gpt-test", 20, commands.NewRegistry())
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.Canceled)
	_, err := host.readLineCtx(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPlainInteractiveDisplayExitMessageNoAgent(t *testing.T) {
	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(bytes.NewBufferString(""), out, "gpt-test", 20, commands.NewRegistry())
	// agent is nil

	host.displayExitMessage()

	output := out.String()
	assert.Empty(t, output, "exit message should be empty when no agent is available")
}

// TestExitMessageAdvertisesOnlyWhatPlainModeSupports pins that the banner does
// not send the user to a command this mode declines to run. Plain mode has no
// resume overlay, so /resume is a dead end here; --resume works because it is
// applied at construction via agent.WithResumeSession, before any UI exists.
//
// The banner is the more damaging half of that gap: it prints as the user
// leaves, so a false instruction is not discovered until their next session.
func TestExitMessageAdvertisesOnlyWhatPlainModeSupports(t *testing.T) {
	msg := exitMessage("sess-abc123")

	assert.Contains(t, msg, "sess-abc123", "the user needs the ID to resume")
	assert.Contains(t, msg, "--resume sess-abc123", "the flag is the path that works in plain mode")
	assert.NotContains(t, msg, "/resume", "plain mode has no resume overlay; advertising it strands the user")
}

// TestPlainInteractiveResumeReportsUnavailable pins that /resume says something
// rather than nothing. The command resolves and returns ActionResume with an
// empty Output, so before this case existed the host printed nothing at all and
// carried on — indistinguishable from success, and therefore unreportable by
// the user who hit it.
func TestPlainInteractiveResumeReportsUnavailable(t *testing.T) {
	reg := commands.NewRegistry()
	require.NoError(t, reg.Register(commands.NewResumeCommand()))
	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(bytes.NewBufferString(""), out, "gpt-test", 20, reg)

	quit, err := host.handleCommand(context.Background(), "/resume")

	require.NoError(t, err)
	assert.False(t, quit, "an unsupported command must not end the session")
	assert.Contains(t, out.String(), "not available in plain interactive mode")
	assert.Contains(t, out.String(), "--resume", "point the user at the path that does work")
}

// unsupportedActionCommand returns an Action this host has no case for, standing
// in for any overlay action added in future.
type unsupportedActionCommand struct{ action commands.Action }

func (c *unsupportedActionCommand) Name() string                      { return "futurecmd" }
func (c *unsupportedActionCommand) Description() string               { return "an overlay command" }
func (c *unsupportedActionCommand) Arguments() []commands.ArgumentDef { return nil }
func (c *unsupportedActionCommand) Complete(_ context.Context, _ []string) []commands.Candidate {
	return nil
}
func (c *unsupportedActionCommand) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{Action: c.action}, nil
}

// TestPlainInteractiveOverlayCommandsReportUnavailable covers the class, not
// just the instances. /resume was one of five overlay actions reachable in plain
// mode with no case and no Output; the others were silent for the same reason.
// The default arm means a newly added Action cannot join them unnoticed.
func TestPlainInteractiveOverlayCommandsReportUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  commands.SlashCommand
		line string
	}{
		{"about", commands.NewAboutCommand(), "/about"},
		{"undo", commands.NewUndoOverlayCommand(), "/undo"},
		{"bare model opens the picker", commands.NewModelCommand(func(string) {}), "/model"},
		{"an action added later", &unsupportedActionCommand{action: commands.ActionOpenModelPicker}, "/futurecmd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := commands.NewRegistry()
			require.NoError(t, reg.Register(tc.cmd))
			out := &bytes.Buffer{}
			host := newPlainInteractiveHost(bytes.NewBufferString(""), out, "gpt-test", 20, reg)

			quit, err := host.handleCommand(context.Background(), tc.line)

			require.NoError(t, err)
			assert.False(t, quit)
			assert.NotEmpty(t, out.String(), "a command that does nothing must at least say so")
			assert.Contains(t, out.String(), "not available in plain interactive mode")
		})
	}
}

// TestPlainInteractiveOrdinaryCommandStaysQuiet guards the other side: the
// default arm must not fire for ActionNone, which is what every ordinary
// command returns.
func TestPlainInteractiveOrdinaryCommandStaysQuiet(t *testing.T) {
	reg := commands.NewRegistry()
	require.NoError(t, reg.Register(&unsupportedActionCommand{action: commands.ActionNone}))
	out := &bytes.Buffer{}
	host := newPlainInteractiveHost(bytes.NewBufferString(""), out, "gpt-test", 20, reg)

	_, err := host.handleCommand(context.Background(), "/futurecmd")

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "not available in plain interactive mode")
}
