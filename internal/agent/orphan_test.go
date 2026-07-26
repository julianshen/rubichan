package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestSynthesizeMissingToolResultsFillsOrphans(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "text", Text: "using tools"},
		{Type: "tool_use", ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"a"}`)},
		{Type: "tool_use", ID: "call_2", Name: "read_file", Input: json.RawMessage(`{"path":"b"}`)},
	})

	n := synthesizeMissingToolResults(conv, "stream aborted")
	if n != 2 {
		t.Fatalf("want 2 orphans sealed, got %d", n)
	}

	msgs := conv.Messages()
	got := map[string]bool{}
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == "tool_result" {
				got[b.ToolUseID] = true
			}
		}
	}
	if !got["call_1"] || !got["call_2"] {
		t.Fatalf("missing tool_result for orphans: %v", got)
	}

	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID == "call_1" {
				if !strings.Contains(b.Text, "read_file") {
					t.Errorf("synthesized content missing tool name: %q", b.Text)
				}
				if !strings.Contains(b.Text, "stream aborted") {
					t.Errorf("synthesized content missing reason: %q", b.Text)
				}
			}
		}
	}
}

func TestSynthesizeMissingToolResultsEmptyToolName(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "tool_use", ID: "call_x", Name: "", Input: json.RawMessage(`{}`)},
	})
	n := synthesizeMissingToolResults(conv, "boom")
	if n != 1 {
		t.Fatalf("want 1 sealed, got %d", n)
	}
	msgs := conv.Messages()
	var content string
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID == "call_x" {
				content = b.Text
			}
		}
	}
	if !strings.Contains(content, "<unknown>") {
		t.Fatalf("empty tool name should render as <unknown>, got %q", content)
	}
}

func TestSynthesizeMissingToolResultsSkipsIfAlreadyAnswered(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "tool_use", ID: "call_1", Name: "read_file", Input: json.RawMessage(`{}`)},
	})
	conv.AddToolResult("call_1", "ok", false)

	n := synthesizeMissingToolResults(conv, "reason")
	if n != 0 {
		t.Fatalf("want 0 orphans, got %d", n)
	}
}

func TestSynthesizeMissingToolResultsNoAssistantTail(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")

	n := synthesizeMissingToolResults(conv, "reason")
	if n != 0 {
		t.Fatalf("want 0 when last message is user, got %d", n)
	}
}

func TestSynthesizeMissingToolResultsPartialOrphans(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "tool_use", ID: "call_1", Name: "read_file", Input: json.RawMessage(`{}`)},
		{Type: "tool_use", ID: "call_2", Name: "read_file", Input: json.RawMessage(`{}`)},
	})
	conv.AddToolResult("call_1", "ok", false)

	n := synthesizeMissingToolResults(conv, "reason")
	if n != 1 {
		t.Fatalf("want 1 orphan (call_2 only), got %d", n)
	}
}

// cancelOnExecuteTool is a test tool that cancels a pre-supplied context the
// first time it executes. Used to simulate a mid-tool-execution cancellation
// so the runLoop "cancelled during tool execution" exit path fires.
type cancelOnExecuteTool struct {
	cancel  context.CancelFunc
	invoked bool
}

func (c *cancelOnExecuteTool) Name() string        { return "cancel_tool" }
func (c *cancelOnExecuteTool) Description() string { return "cancels the agent context" }
func (c *cancelOnExecuteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (c *cancelOnExecuteTool) Execute(_ context.Context, _ json.RawMessage) (agentsdk.ToolResult, error) {
	c.invoked = true
	c.cancel()
	return agentsdk.ToolResult{Content: "cancelled"}, nil
}

// TestRunLoopToolCancelLeavesNoOrphans verifies that the sweeper is wired into
// the runLoop path that exits when the context is cancelled mid-tool-execution.
// We provide a batch of two tool calls, the first of which cancels the ctx.
// After the first tool records its result, the sequential executor observes
// ctx.Err() and returns early, leaving the second tool_use without a result.
// The sweeper must seal it before the runLoop emits "done".
func TestRunLoopToolCancelLeavesNoOrphans(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelTool := &cancelOnExecuteTool{cancel: cancel}

	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "cancel_tool", Input: json.RawMessage(`{}`)}},
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "cancel_tool", Input: json.RawMessage(`{}`)}},
			{Type: "stop"},
		},
	}

	reg := tools.NewRegistry()
	if err := reg.Register(cancelTool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	cfg := config.DefaultConfig()
	a := New(mp, reg, autoApprove, cfg)

	ch, err := a.Turn(ctx, "run the tool")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	for range ch {
	}

	if !cancelTool.invoked {
		t.Fatalf("cancel tool was not invoked — test precondition failed")
	}

	assertNoOrphanToolUses(t, a)
}

// TestLoadSessionHistory_SynthesizesOrphans verifies that loadSessionHistory
// calls synthesizeMissingToolResults so that a persisted session whose last
// assistant message contains unanswered tool_use blocks is repaired on resume.
// Without the fix a subsequent API call would fail with a 400 protocol error.
func TestLoadSessionHistory_SynthesizesOrphans(t *testing.T) {
	t.Parallel()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	const sessionID = "sess-orphan"
	require.NoError(t, s.CreateSession(store.Session{
		ID:           sessionID,
		Model:        "gpt-4",
		SystemPrompt: "You are helpful.",
	}))

	// Persist a user message followed by an assistant message whose
	// tool_use block has no matching tool_result — simulating a session
	// that died between emitting tool_use and executing the tool.
	require.NoError(t, s.AppendMessage(sessionID, "user", []provider.ContentBlock{
		{Type: "text", Text: "Run the tool please"},
	}))
	require.NoError(t, s.AppendMessage(sessionID, "assistant", []provider.ContentBlock{
		{Type: "text", Text: "Sure, calling the tool now."},
		{Type: "tool_use", ID: "orphan_call_1", Name: "read_file", Input: json.RawMessage(`{"path":"file.go"}`)},
	}))

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "gpt-4"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{{Type: "stop"}}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))

	err = a.ResumeSession(context.Background(), sessionID)
	require.NoError(t, err)

	// After load, the orphaned tool_use must have been sealed with a
	// synthetic error tool_result so the conversation is protocol-valid.
	msgs := a.conversation.Messages()
	var found bool
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID == "orphan_call_1" {
				found = true
				if !b.IsError {
					t.Errorf("synthesized tool_result should have IsError=true")
				}
				if !strings.Contains(b.Text, orphanReasonLoad) {
					t.Errorf("synthesized content missing load reason: %q", b.Text)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no tool_result found for orphan_call_1 after session load; orphan repair not wired into loadSessionHistory")
	}
}

// assertNoOrphanToolUses fails if any tool_use in the last assistant message
// lacks a matching tool_result later in the conversation.
func assertNoOrphanToolUses(t *testing.T, a *Agent) {
	t.Helper()
	msgs := a.conversation.Messages()
	assistantIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			assistantIdx = i
			break
		}
	}
	if assistantIdx == -1 {
		t.Fatalf("no assistant message found in conversation")
	}
	answered := map[string]bool{}
	for i := assistantIdx + 1; i < len(msgs); i++ {
		for _, b := range msgs[i].Content {
			if b.Type == "tool_result" {
				answered[b.ToolUseID] = true
			}
		}
	}
	for _, b := range msgs[assistantIdx].Content {
		if b.Type == "tool_use" && !answered[b.ID] {
			t.Fatalf("orphan tool_use %q (%s) has no matching tool_result; "+
				"the next provider call would fail with a protocol error", b.ID, b.Name)
		}
	}
}

// TestTaskCompletePathToolCancelLeavesNoOrphans covers the task_complete
// terminal path, which executes the whole pending batch before exiting. If the
// batch is cancelled part-way the trailing tool_use blocks never get results,
// so the sweeper must run here too — not only on the main execution path.
func TestTaskCompletePathToolCancelLeavesNoOrphans(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelTool := &cancelOnExecuteTool{cancel: cancel}

	// The cancelling tool runs first; task_complete is later in the batch and
	// so is skipped by the sequential executor once ctx is cancelled.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "cancel_tool", Input: json.RawMessage(`{}`)}},
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: tools.TaskCompleteName, Input: json.RawMessage(`{}`)}},
			{Type: "stop"},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(cancelTool))

	a := New(mp, reg, autoApprove, config.DefaultConfig())

	ch, err := a.Turn(ctx, "finish up")
	require.NoError(t, err)

	var (
		exitReason      agentsdk.TurnExitReason
		cancellationErr error
	)
	for ev := range ch {
		switch ev.Type {
		case "done":
			exitReason = ev.ExitReason
		case "error":
			cancellationErr = ev.Error
		}
	}

	require.True(t, cancelTool.invoked, "cancel tool was not invoked — test precondition failed")
	assertNoOrphanToolUses(t, a)

	// task_complete never ran — the cancelling sibling preceded it — so
	// reporting completion would tell every consumer the turn succeeded.
	// The hook's response_reason still says task_complete; that answers a
	// different question and is resolved before execution.
	require.Equal(t, agentsdk.ExitCancelled, exitReason,
		"a batch cancelled before task_complete ran must report cancellation, not completion")

	// The done event is only half of what this path emits. Consumers that
	// surface the failure to the user read the error event, so assert it too
	// or it could go missing without any test noticing.
	require.ErrorIs(t, cancellationErr, context.Canceled,
		"the cancelled task_complete path must emit the cancellation error alongside its done event")
}

// TestTaskCompletePathToolCancelPersistsSeal covers the persistence half of
// the same defect. Sealing the batch in memory is not enough when a store is
// attached: executeTools returns early on cancellation, skipping its trailing
// snapshot save, so the newest snapshot is still the pre-provider one Turn
// wrote. Because loadSessionHistory prefers a snapshot over the message log
// whenever one exists, resuming would restore the session from before the
// assistant turn — dropping the completed tool results along with the seal.
func TestTaskCompletePathToolCancelPersistsSeal(t *testing.T) {
	t.Parallel()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelTool := &cancelOnExecuteTool{cancel: cancel}

	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "cancel_tool", Input: json.RawMessage(`{}`)}},
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: tools.TaskCompleteName, Input: json.RawMessage(`{}`)}},
			{Type: "stop"},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(cancelTool))

	a := New(mp, reg, autoApprove, config.DefaultConfig(), WithStore(s))

	ch, err := a.Turn(ctx, "finish up")
	require.NoError(t, err)
	for range ch {
	}

	require.True(t, cancelTool.invoked, "cancel tool was not invoked — test precondition failed")

	snap, err := s.GetSnapshot(a.sessionID)
	require.NoError(t, err)
	require.NotNil(t, snap, "Turn always writes a snapshot, so one must exist")

	// A resume loads exactly these messages, so the assistant turn and the
	// seal for the tool that never ran must both be in them.
	assertSnapshotSealsToolUses(t, snap)
}

// assertSnapshotSealsToolUses fails if the persisted messages contain a
// tool_use with no matching tool_result — the shape a resume would restore.
func assertSnapshotSealsToolUses(t *testing.T, msgs []provider.Message) {
	t.Helper()
	answered := map[string]bool{}
	var pending []provider.ContentBlock
	for _, m := range msgs {
		for _, b := range m.Content {
			switch b.Type {
			case "tool_use":
				pending = append(pending, b)
			case "tool_result":
				answered[b.ToolUseID] = true
			}
		}
	}
	if len(pending) == 0 {
		t.Fatalf("snapshot has no tool_use blocks; the assistant turn was never persisted, so a resume would lose it entirely")
	}
	for _, b := range pending {
		if !answered[b.ID] {
			t.Fatalf("snapshot leaves tool_use %q (%s) unanswered; a resumed session would replay it and fail a protocol check", b.ID, b.Name)
		}
	}
}

// sideEffectTool records that it ran and returns a distinctive result, so a
// test can tell a real execution apart from a synthesized cancellation seal.
type sideEffectTool struct{ invoked bool }

func (s *sideEffectTool) Name() string        { return "side_effect_tool" }
func (s *sideEffectTool) Description() string { return "performs a side effect" }
func (s *sideEffectTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

func (s *sideEffectTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	s.invoked = true
	return agentsdk.ToolResult{Content: sideEffectResultText}, nil
}

const sideEffectResultText = "side effect committed"

// TestToolCancelKeepsCompletedBatchResults covers the batched execution path
// (the one taken when an approvalChecker is configured). executeTools collects
// every result from the BatchExecutor and only afterwards checks ctx.Err(),
// returning before any of them reaches the conversation. The orphan sweeper
// then seals every call as "did not complete" — including tools that ran and
// whose side effects already landed — and the snapshot save makes that false
// history durable, so a resumed agent may repeat those side effects.
//
// Only the calls that genuinely did not run may be sealed.
func TestToolCancelKeepsCompletedBatchResults(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sideEffect := &sideEffectTool{}
	cancelTool := &cancelOnExecuteTool{cancel: cancel}

	// The side-effect tool runs first and completes; the cancelling tool runs
	// second, so the batch is interrupted only after a real result exists.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "side_effect_tool", Input: json.RawMessage(`{}`)}},
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "cancel_tool", Input: json.RawMessage(`{}`)}},
			{Type: "stop"},
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(sideEffect))
	require.NoError(t, reg.Register(cancelTool))

	// An approvalChecker selects the batched path; without one executeTools
	// falls back to the sequential executor, which commits as it goes.
	a := New(mp, reg, autoApprove, config.DefaultConfig(),
		WithApprovalChecker(AlwaysAutoApprove{}))

	ch, err := a.Turn(ctx, "do the thing")
	require.NoError(t, err)
	for range ch {
	}

	require.True(t, sideEffect.invoked, "side-effect tool did not run — test precondition failed")

	got := toolResultText(t, a, "t1")
	require.Equal(t, sideEffectResultText, got,
		"the completed tool's real result was replaced by a cancellation seal; "+
			"a resumed agent would see the side effect as never having happened")
}

// toolResultText returns the content of the tool_result answering toolUseID.
func toolResultText(t *testing.T, a *Agent, toolUseID string) string {
	t.Helper()
	for _, m := range a.conversation.Messages() {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID == toolUseID {
				return b.Text
			}
		}
	}
	t.Fatalf("no tool_result found for %q", toolUseID)
	return ""
}
