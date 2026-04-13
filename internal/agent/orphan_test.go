package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
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

	// Walk the conversation: every tool_use in the final assistant message
	// must have a matching tool_result somewhere after it.
	msgs := a.conversation.Messages()
	var assistantIdx = -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			assistantIdx = i
			break
		}
	}
	if assistantIdx == -1 {
		t.Fatalf("no assistant message found in conversation")
	}

	var pending []string
	for _, b := range msgs[assistantIdx].Content {
		if b.Type == "tool_use" {
			pending = append(pending, b.ID)
		}
	}
	if len(pending) == 0 {
		t.Fatalf("expected tool_use blocks in assistant message; got none")
	}

	answered := map[string]bool{}
	for i := assistantIdx + 1; i < len(msgs); i++ {
		for _, b := range msgs[i].Content {
			if b.Type == "tool_result" {
				answered[b.ToolUseID] = true
			}
		}
	}
	for _, id := range pending {
		if !answered[id] {
			t.Fatalf("orphan tool_use %q has no matching tool_result; sweeper not wired", id)
		}
	}
}
