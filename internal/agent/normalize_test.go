package agent

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveOrphanedToolCalls_NoOrphans(t *testing.T) {
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "Let me read that file."},
			{Type: "tool_use", ID: "tc_1", Name: "file", Input: json.RawMessage(`{"op":"read"}`)},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "tc_1", Text: "file contents"},
		}},
	}

	result := removeOrphanedToolCalls(messages)
	require.Len(t, result, 2)
	assert.Len(t, result[0].Content, 2) // both blocks kept
}

func TestRemoveOrphanedToolCalls_RemovesOrphans(t *testing.T) {
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "I'll use a tool."},
			{Type: "tool_use", ID: "tc_orphan", Name: "shell", Input: json.RawMessage(`{"cmd":"ls"}`)},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "text", Text: "never mind"},
		}},
	}

	result := removeOrphanedToolCalls(messages)
	require.Len(t, result, 2)
	// The orphaned tool_use should be removed, only text remains.
	require.Len(t, result[0].Content, 1)
	assert.Equal(t, "text", result[0].Content[0].Type)
}

func TestRemoveOrphanedToolCalls_RemovesAssistantMessageWhenAllBlocksOrphaned(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "text", Text: "do something"},
		}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "tool_use", ID: "tc_only", Name: "file", Input: json.RawMessage(`{}`)},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "text", Text: "that didn't work"},
		}},
	}

	result := removeOrphanedToolCalls(messages)
	// The assistant message should be removed entirely since its only block was orphaned.
	require.Len(t, result, 2)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "user", result[1].Role)
}

func TestRemoveOrphanedToolCalls_MixedOrphanedAndMatched(t *testing.T) {
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "tool_use", ID: "tc_matched", Name: "file", Input: json.RawMessage(`{}`)},
			{Type: "tool_use", ID: "tc_orphan", Name: "shell", Input: json.RawMessage(`{}`)},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "tc_matched", Text: "ok"},
		}},
	}

	result := removeOrphanedToolCalls(messages)
	require.Len(t, result, 2)
	// Only the matched tool_use survives.
	require.Len(t, result[0].Content, 1)
	assert.Equal(t, "tc_matched", result[0].Content[0].ID)
}

func TestRemoveOrphanedToolCalls_EmptyMessages(t *testing.T) {
	result := removeOrphanedToolCalls(nil)
	assert.Nil(t, result)
}

func TestRemoveOrphanedToolCalls_ToolUseWithEmptyID(t *testing.T) {
	// tool_use blocks with empty ID should not be considered orphaned
	// (they might be malformed but we shouldn't strip them).
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "tool_use", ID: "", Name: "file", Input: json.RawMessage(`{}`)},
		}},
	}

	result := removeOrphanedToolCalls(messages)
	require.Len(t, result, 1)
	assert.Len(t, result[0].Content, 1)
}

func TestMergeConsecutiveAssistant_NoConsecutive(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "bye"}}},
	}

	result := mergeConsecutiveAssistant(messages)
	require.Len(t, result, 3)
}

func TestMergeConsecutiveAssistant_MergesTwo(t *testing.T) {
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "part1"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "part2"}}},
	}

	result := mergeConsecutiveAssistant(messages)
	require.Len(t, result, 1)
	assert.Equal(t, "assistant", result[0].Role)
	require.Len(t, result[0].Content, 2)
	assert.Equal(t, "part1", result[0].Content[0].Text)
	assert.Equal(t, "part2", result[0].Content[1].Text)
}

func TestMergeConsecutiveAssistant_MergesThree(t *testing.T) {
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "a"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "b"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "c"}}},
	}

	result := mergeConsecutiveAssistant(messages)
	require.Len(t, result, 1)
	require.Len(t, result[0].Content, 3)
}

func TestMergeConsecutiveAssistant_DoesNotMergeConsecutiveUser(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "b"}}},
	}

	result := mergeConsecutiveAssistant(messages)
	// User messages should NOT be merged.
	require.Len(t, result, 2)
}

func TestMergeConsecutiveAssistant_EmptyAndSingle(t *testing.T) {
	assert.Nil(t, mergeConsecutiveAssistant(nil))
	assert.Len(t, mergeConsecutiveAssistant([]provider.Message{}), 0)

	single := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "only"}}},
	}
	result := mergeConsecutiveAssistant(single)
	require.Len(t, result, 1)
}

func TestNormalizeMessages_CombinesBothPasses(t *testing.T) {
	// Build a scenario where compaction left:
	// 1. An orphaned tool_use (no matching tool_result)
	// 2. Consecutive assistant messages
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "start"}}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "thinking..."},
			{Type: "tool_use", ID: "tc_gone", Name: "file", Input: json.RawMessage(`{}`)},
		}},
		// After compaction removed the tool_result, the next assistant is consecutive.
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "continued"},
		}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
	}

	result := NormalizeMessages(messages)

	// The orphaned tool_use is removed. The two assistant messages merge.
	require.Len(t, result, 3)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "assistant", result[1].Role)
	assert.Equal(t, "user", result[2].Role)

	// Merged assistant should have both text blocks.
	require.Len(t, result[1].Content, 2)
	assert.Equal(t, "thinking...", result[1].Content[0].Text)
	assert.Equal(t, "continued", result[1].Content[1].Text)
}

func TestNormalizeMessages_PreservesValidConversation(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "read foo.go"}}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "tool_use", ID: "tc_1", Name: "file", Input: json.RawMessage(`{"op":"read"}`)},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "tc_1", Text: "package main"},
		}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "Here's the file content."},
		}},
	}

	result := NormalizeMessages(messages)
	require.Len(t, result, 4)
	// All messages preserved unchanged.
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "assistant", result[1].Role)
	assert.Len(t, result[1].Content, 1) // tool_use kept
	assert.Equal(t, "user", result[2].Role)
	assert.Equal(t, "assistant", result[3].Role)
}
