package normalize

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func msg(role string, blocks ...agentsdk.ContentBlock) agentsdk.Message {
	return agentsdk.Message{Role: role, Content: blocks}
}

func textBlock(text string) agentsdk.ContentBlock {
	return agentsdk.ContentBlock{Type: "text", Text: text}
}

func thinkingBlock(text string) agentsdk.ContentBlock {
	return agentsdk.ContentBlock{Type: "thinking", Text: text}
}

func toolUseBlock(id, name string) agentsdk.ContentBlock {
	return agentsdk.ContentBlock{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(`{}`)}
}

func toolResultBlock(toolUseID, text string) agentsdk.ContentBlock {
	return agentsdk.ContentBlock{Type: "tool_result", ToolUseID: toolUseID, Text: text}
}

// --- RemoveEmptyMessages ---

func TestRemoveEmptyMessages_EmptyText(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("user", textBlock("")),
		msg("user", textBlock("hello")),
	}
	result := RemoveEmptyMessages(msgs)
	require.Len(t, result, 1)
	assert.Equal(t, "hello", result[0].Content[0].Text)
}

func TestRemoveEmptyMessages_EmptyThinking(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("assistant", thinkingBlock("")),
	}
	result := RemoveEmptyMessages(msgs)
	assert.Empty(t, result)
}

func TestRemoveEmptyMessages_PreservesToolUse(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("assistant", toolUseBlock("id1", "search")),
	}
	result := RemoveEmptyMessages(msgs)
	require.Len(t, result, 1)
	assert.Equal(t, "tool_use", result[0].Content[0].Type)
}

func TestRemoveEmptyMessages_MixedBlocks(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("assistant", textBlock(""), thinkingBlock(""), textBlock("kept")),
	}
	result := RemoveEmptyMessages(msgs)
	require.Len(t, result, 1)
	require.Len(t, result[0].Content, 1)
	assert.Equal(t, "kept", result[0].Content[0].Text)
}

// --- ScrubToolIDs ---

func TestScrubAnthropicToolID(t *testing.T) {
	assert.Equal(t, "call_1_abc", ScrubAnthropicToolID("call:1/abc"))
	assert.Equal(t, "simple_id", ScrubAnthropicToolID("simple_id"))
	assert.Equal(t, "a-b-c", ScrubAnthropicToolID("a-b-c"))
	assert.Equal(t, "has_space", ScrubAnthropicToolID("has space"))
}

func TestScrubToolIDs_BothDirections(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("assistant", toolUseBlock("call:1/abc", "search")),
		msg("user", toolResultBlock("call:1/abc", "done")),
	}
	result := ScrubToolIDs(msgs, ScrubAnthropicToolID)
	assert.Equal(t, "call_1_abc", result[0].Content[0].ID)
	assert.Equal(t, "call_1_abc", result[1].Content[0].ToolUseID)
}

// --- InsertAssistantBetweenToolAndUser ---

func TestInsertAssistantBetweenToolAndUser(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("user", toolResultBlock("id1", "result")),
		msg("user", textBlock("next question")),
	}
	result := InsertAssistantBetweenToolAndUser(msgs)
	require.Len(t, result, 3)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "assistant", result[1].Role)
	assert.Equal(t, "user", result[2].Role)
}

func TestInsertAssistantBetweenToolAndUser_NoInsertWhenNotNeeded(t *testing.T) {
	msgs := []agentsdk.Message{
		msg("user", textBlock("hello")),
		msg("assistant", textBlock("hi")),
		msg("user", textBlock("bye")),
	}
	result := InsertAssistantBetweenToolAndUser(msgs)
	require.Len(t, result, 3)
}

func TestTruncateToolID(t *testing.T) {
	assert.Equal(t, "abcde", TruncateToolID("abcdefghij", 5))
	assert.Equal(t, "abc", TruncateToolID("abc", 5))
	assert.Equal(t, "abc", TruncateToolID("abc", 0)) // 0 = no limit
}
