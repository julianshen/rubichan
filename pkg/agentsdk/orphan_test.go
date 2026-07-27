package agentsdk

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSealOrphanedToolUses covers the branches that decide whether a
// conversation needs repair at all. Both agent cores call this, so the
// shape of a conversation it declines to touch is part of the contract:
// sealing something already answered would duplicate a tool_result, and
// sealing when there is no assistant turn would attach a result to nothing.
func TestSealOrphanedToolUses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		build      func(*Conversation)
		wantSealed int
	}{
		{
			name:       "empty conversation has nothing to seal",
			build:      func(*Conversation) {},
			wantSealed: 0,
		},
		{
			name: "no assistant message means no tool_use can exist",
			build: func(c *Conversation) {
				c.AddUser("hi")
			},
			wantSealed: 0,
		},
		{
			name: "assistant message without tool calls needs no repair",
			build: func(c *Conversation) {
				c.AddUser("hi")
				c.AddAssistant([]ContentBlock{{Type: "text", Text: "just talking"}})
			},
			wantSealed: 0,
		},
		{
			name: "a tool_use already answered is left alone",
			build: func(c *Conversation) {
				c.AddUser("hi")
				c.AddAssistant([]ContentBlock{
					{Type: "tool_use", ID: "call_1", Name: "read_file"},
				})
				c.AddToolResult("call_1", "contents", false)
			},
			wantSealed: 0,
		},
		{
			name: "only the unanswered half of a batch is sealed",
			build: func(c *Conversation) {
				c.AddUser("hi")
				c.AddAssistant([]ContentBlock{
					{Type: "tool_use", ID: "call_1", Name: "read_file"},
					{Type: "tool_use", ID: "call_2", Name: "read_file"},
				})
				c.AddToolResult("call_1", "contents", false)
			},
			wantSealed: 1,
		},
		{
			name: "a tool_use with no ID cannot be answered and is skipped",
			build: func(c *Conversation) {
				c.AddUser("hi")
				c.AddAssistant([]ContentBlock{
					{Type: "tool_use", ID: "", Name: "read_file"},
				})
			},
			wantSealed: 0,
		},
		{
			name: "only the most recent assistant turn is considered",
			build: func(c *Conversation) {
				c.AddAssistant([]ContentBlock{
					{Type: "tool_use", ID: "old_call", Name: "read_file"},
				})
				c.AddUser("next")
				c.AddAssistant([]ContentBlock{{Type: "text", Text: "done"}})
			},
			wantSealed: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conv := NewConversation("sys")
			tt.build(conv)
			before := len(conv.Messages())

			got := SealOrphanedToolUses(conv, "test reason")

			assert.Equal(t, tt.wantSealed, got)
			assert.Len(t, conv.Messages(), before+tt.wantSealed,
				"sealed count and appended messages must agree")
		})
	}
}

// TestSealOrphanedToolUsesSynthesizedResult pins what a sealed result says.
// The text reaches the model on the next turn and lands in captured
// conversations, so both the tool name and the reason have to survive.
func TestSealOrphanedToolUsesSynthesizedResult(t *testing.T) {
	t.Parallel()

	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]ContentBlock{
		{Type: "tool_use", ID: "call_1", Name: "read_file"},
	})

	require.Equal(t, 1, SealOrphanedToolUses(conv, OrphanReasonToolCancel))

	block := lastToolResult(t, conv)
	assert.Equal(t, "call_1", block.ToolUseID)
	assert.True(t, block.IsError, "a tool that never ran is not a success")
	assert.Contains(t, block.Text, "read_file", "the model needs to know which tool failed")
	assert.Contains(t, block.Text, OrphanReasonToolCancel)
}

// TestSealOrphanedToolUsesUnnamedTool covers the fallback for a tool_use whose
// name never arrived — possible when a stream dies mid-block. The result still
// has to be emitted, since the protocol cares about the ID, not the name.
func TestSealOrphanedToolUsesUnnamedTool(t *testing.T) {
	t.Parallel()

	conv := NewConversation("sys")
	conv.AddAssistant([]ContentBlock{
		{Type: "tool_use", ID: "call_1", Name: ""},
	})

	require.Equal(t, 1, SealOrphanedToolUses(conv, "stream died"))

	text := lastToolResult(t, conv).Text
	assert.Contains(t, text, "<unknown>",
		"an unnamed tool should read as unknown rather than leaving a gap in the sentence")
	assert.False(t, strings.Contains(text, "tool  did not"),
		"the name placeholder should not collapse into a double space")
}

func lastToolResult(t *testing.T, conv *Conversation) ContentBlock {
	t.Helper()
	msgs := conv.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		for _, b := range msgs[i].Content {
			if b.Type == "tool_result" {
				return b
			}
		}
	}
	t.Fatalf("no tool_result found in conversation")
	return ContentBlock{}
}
