package agent

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
)

func TestStripThinkingBlocks(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "hello"},
		}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "thinking", Text: "let me think"},
			{Type: "text", Text: "answer"},
		}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "redacted_thinking"},
			{Type: "text", Text: "more"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 3, len(stripped), "should preserve non-thinking messages")
	assert.Equal(t, 1, len(stripped[1].Content), "assistant msg should have thinking removed")
	assert.Equal(t, "text", stripped[1].Content[0].Type)
}

func TestStripThinkingBlocks_RemovesAllThinking(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "thinking", Text: "deep thought"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 0, len(stripped), "message with only thinking should be removed")
}

func TestStripThinkingBlocks_PreservesNonThinkingMessages(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "hello"},
		}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "response"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 2, len(stripped))
	assert.Equal(t, 1, len(stripped[0].Content))
	assert.Equal(t, 1, len(stripped[1].Content))
}
