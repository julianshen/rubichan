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
