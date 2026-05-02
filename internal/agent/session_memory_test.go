package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestSessionMemoryStrategy_Name(t *testing.T) {
	s := &sessionMemoryStrategy{}
	require.Equal(t, "session_memory", s.Name())
}

func TestCalculateMessagesToKeepIndex_Empty(t *testing.T) {
	s := &sessionMemoryStrategy{}
	counter := func(msgs []provider.Message) int { return len(msgs) * 1000 }
	idx := s.calculateMessagesToKeepIndex(nil, counter)
	require.Equal(t, 0, idx)
}

func TestCalculateMessagesToKeepIndex_UnderBudget(t *testing.T) {
	// 5 messages, each 1000 tokens — under minPreserveTokens (10_000)
	messages := makeSizedMessages(5, 1000)
	s := &sessionMemoryStrategy{}
	counter := func(msgs []provider.Message) int { return len(msgs) * 1000 }
	idx := s.calculateMessagesToKeepIndex(messages, counter)
	// Should keep all 5 (5_000 tokens < 10_000 min)
	require.Equal(t, 0, idx)
}

func TestCalculateMessagesToKeepIndex_OverBudget(t *testing.T) {
	// 20 messages, each 1000 tokens — over minPreserveTokens
	messages := makeSizedMessages(20, 1000)
	s := &sessionMemoryStrategy{}
	counter := func(msgs []provider.Message) int { return len(msgs) * 1000 }
	idx := s.calculateMessagesToKeepIndex(messages, counter)
	// Should keep at least minPreserveTokens (10_000) worth of messages.
	// With 20 messages of 1000 tokens each, we need to summarize some.
	// The exact idx depends on the algorithm: it starts from 0 and expands
	// backwards until minPreserveTokens is met. Since we have 20_000 tokens
	// total and need to keep 10_000, idx should be around 10.
	require.Greater(t, idx, 0, "should summarize some messages")
	require.Less(t, idx, 20, "should keep some messages")
}

func TestCalculateMessagesToKeepIndex_MinTextBlocks(t *testing.T) {
	// 15 messages, each 1000 tokens, but only 3 have text blocks
	messages := makeSizedMessages(15, 1000)
	// Remove text blocks from some messages
	for i := range messages {
		if i%2 == 0 {
			messages[i].Content = []provider.ContentBlock{{Type: "tool_result"}}
		}
	}
	s := &sessionMemoryStrategy{}
	counter := func(msgs []provider.Message) int { return len(msgs) * 1000 }
	idx := s.calculateMessagesToKeepIndex(messages, counter)
	// Should expand backwards to get 5 text-block messages
	require.LessOrEqual(t, idx, 10)
}

func TestAdjustIndexPreservesToolPairs(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "done"}}},
	}

	// Trying to split at idx=2 (between tool_use and tool_result)
	idx := adjustIndexToPreserveAPIInvariants(messages, 2)
	// Should move to idx=1 to include the assistant with tool_use
	require.Equal(t, 1, idx)
}

func TestAdjustIndexPreservesToolPairs_NoSplitNeeded(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "bye"}}},
	}

	idx := adjustIndexToPreserveAPIInvariants(messages, 1)
	require.Equal(t, 1, idx)
}

func TestAdjustIndexPreservesThinkingBlocks(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "thinking", ID: "th1"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "thinking", ID: "th1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "bye"}}},
	}

	// Trying to split at idx=2 (middle of thinking block)
	idx := adjustIndexToPreserveAPIInvariants(messages, 2)
	// Should move to idx=1 to keep thinking blocks together
	require.Equal(t, 1, idx)
}

func TestSessionMemoryStrategy_Compact(t *testing.T) {
	// Use 25 messages so we definitely exceed minPreserveTokens
	messages := makeSizedMessages(25, 1000)
	s := &sessionMemoryStrategy{}
	result, err := s.Compact(context.Background(), messages, 50_000)
	require.NoError(t, err)

	// Should have summary message + kept messages
	require.Greater(t, len(result), 0)
	// If compaction happened, first message is the summary
	if len(result) < len(messages) {
		require.Equal(t, "system", result[0].Role)
		require.Contains(t, result[0].Content[0].Text, "summarized")
	}
}

func TestSessionMemoryStrategy_Compact_NothingToCompact(t *testing.T) {
	messages := makeSizedMessages(5, 1000)
	s := &sessionMemoryStrategy{}
	result, err := s.Compact(context.Background(), messages, 20_000)
	require.NoError(t, err)
	// Should return original messages unchanged
	require.Equal(t, messages, result)
}

func TestSessionMemoryStrategy_Compact_PreservesToolPairs(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "done"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "more"}}},
	}

	s := &sessionMemoryStrategy{}
	result, err := s.Compact(context.Background(), messages, 100)
	require.NoError(t, err)

	// The tool_use/tool_result pair should not be split
	foundToolUse := false
	foundToolResult := false
	for _, m := range result {
		for _, c := range m.Content {
			if c.Type == "tool_use" {
				foundToolUse = true
			}
			if c.Type == "tool_result" {
				foundToolResult = true
			}
		}
	}
	// Both should be present or both absent
	require.Equal(t, foundToolUse, foundToolResult, "tool_use/tool_result pair should not be split")
}

// makeSizedMessages creates n messages with the given token size each.
func makeSizedMessages(n, tokensPerMsg int) []provider.Message {
	messages := make([]provider.Message, n)
	for i := range messages {
		messages[i] = provider.Message{
			Role: "user",
			Content: []provider.ContentBlock{{
				Type: "text",
				Text: string(make([]byte, tokensPerMsg*4)), // ~4 chars per token
			}},
		}
	}
	return messages
}
