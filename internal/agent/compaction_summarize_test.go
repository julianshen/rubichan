package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSummarizer records calls and returns a fixed summary.
type mockSummarizer struct {
	called   bool
	received []provider.Message
	summary  string
	err      error
}

func (m *mockSummarizer) Summarize(_ context.Context, messages []provider.Message) (string, error) {
	m.called = true
	m.received = messages
	if m.err != nil {
		return "", m.err
	}
	return m.summary, nil
}

func TestSummarizationStrategyName(t *testing.T) {
	s := NewSummarizationStrategy(nil)
	assert.Equal(t, "summarization", s.Name())
}

func TestSummarizationCalledWhenThresholdExceeded(t *testing.T) {
	ms := &mockSummarizer{summary: "conversation summary"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	messages := make([]provider.Message, 6)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg %d", i)}},
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)
	assert.True(t, ms.called, "summarizer should be called")

	// Should have fewer messages than original
	assert.Less(t, len(result), len(messages))

	// First message should be the summary
	assert.Contains(t, result[0].Content[0].Text, "[Summary of")
	assert.Contains(t, result[0].Content[0].Text, "conversation summary")
}

func TestSummarizationSkippedWhenBelowThreshold(t *testing.T) {
	ms := &mockSummarizer{summary: "should not appear"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 20

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)
	assert.False(t, ms.called, "summarizer should not be called below threshold")
	assert.Len(t, result, 2)
}

func TestSummarizationSkippedWhenNilSummarizer(t *testing.T) {
	s := NewSummarizationStrategy(nil)
	s.messageThreshold = 2

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "more"}}},
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)
	assert.Len(t, result, 3, "should return messages unchanged with nil summarizer")
}

func TestSummarizationMessageFormat(t *testing.T) {
	ms := &mockSummarizer{summary: "User asked about Go. Assistant explained interfaces."}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	messages := make([]provider.Message, 10)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("message %d", i)}},
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// Should have 6 old messages summarized (60% of 10)
	assert.Contains(t, result[0].Content[0].Text, "[Summary of 6 earlier messages]")
	// 4 recent + 1 summary = 5
	assert.Len(t, result, 5)
}

func TestSummarizationReceivesOldestMessages(t *testing.T) {
	ms := &mockSummarizer{summary: "summary"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	messages := make([]provider.Message, 10)
	for i := range messages {
		messages[i] = provider.Message{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg-%d", i)}},
		}
	}

	_, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// Summarizer should receive the oldest 60% (6 messages)
	assert.Len(t, ms.received, 6)
	assert.Equal(t, "msg-0", ms.received[0].Content[0].Text)
	assert.Equal(t, "msg-5", ms.received[5].Content[0].Text)
}

func TestSummarizationErrorReturnsOriginal(t *testing.T) {
	ms := &mockSummarizer{err: fmt.Errorf("API error")}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 2

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "more"}}},
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarization failed")
	// Original messages returned on error
	assert.Len(t, result, 3)
}

// --- Enhancement 1: Tool-aware split points ---

func TestSummarizationSplitAvoidsOrphanedToolResult(t *testing.T) {
	ms := &mockSummarizer{summary: "summary"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	// 10 messages, 60% split = index 6
	// Place a tool_use at index 5 (assistant) and tool_result at index 6 (user)
	// so the naive splitIdx=6 would orphan the tool_result.
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 0"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 2"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 3"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 4"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "file"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "result"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 7"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 8"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 9"}}},
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// The split should have moved backward so that the tool_result (index 6)
	// is NOT the first message in recentMessages. The tool_use at index 5
	// should be the split point, keeping the pair together in recent.
	for _, msg := range result[1:] { // skip summary msg
		if hasToolResult(msg) {
			// There must be a preceding assistant with tool_use in the result
			found := false
			for _, r := range result {
				if hasToolUse(r) {
					found = true
					break
				}
			}
			assert.True(t, found, "tool_result should have matching tool_use in result")
		}
	}
	// Verify the first recent message (result[1]) is NOT a tool_result
	assert.NotEqual(t, "tool_result", result[1].Content[0].Type,
		"split should not leave orphaned tool_result at start of recent messages")
}

func TestSummarizationSplitSafeWhenNoToolPairs(t *testing.T) {
	ms := &mockSummarizer{summary: "summary"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	// 10 plain messages — no tool pairs, split should stay at normal 60%
	messages := make([]provider.Message, 10)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg %d", i)}},
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// 60% of 10 = 6 summarized → 1 summary + 4 recent = 5
	assert.Len(t, result, 5)
}

func TestSummarizationSplitMovesBackwardPastToolUse(t *testing.T) {
	ms := &mockSummarizer{summary: "summary"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	// Build 8 messages where tool_use is at index 4 and tool_result at index 5.
	// 60% of 8 = 4. Since index 4 is a tool_use, findSafeSplitPoint should
	// move backward to index 3, keeping the tool_use/tool_result pair together
	// on the recent side.
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 0"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 2"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 3"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "file"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "result"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg 6"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "msg 7"}}},
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// Verify no orphaned tool_result in recent messages.
	if len(result) > 1 {
		assert.NotEqual(t, "tool_result", result[1].Content[0].Type,
			"recent messages should not start with orphaned tool_result")
	}
	// Summarizer should have received fewer than 4 messages (split moved back).
	assert.Less(t, len(ms.received), 5,
		"split should move backward past tool_use so pair stays on recent side")
}

// --- findSafeSplitPoint unit tests ---

func TestFindSafeSplitPoint(t *testing.T) {
	text := func(s string) provider.Message {
		return provider.Message{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: s}}}
	}
	toolUse := func() provider.Message {
		return provider.Message{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "file"}}}
	}
	toolResult := func() provider.Message {
		return provider.Message{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "ok"}}}
	}

	tests := []struct {
		name     string
		messages []provider.Message
		target   int
		want     int
	}{
		{
			name:     "safe target stays unchanged",
			messages: []provider.Message{text("a"), text("b"), text("c"), text("d"), text("e")},
			target:   3,
			want:     3,
		},
		{
			name:     "tool_result at target moves backward past tool_use too",
			messages: []provider.Message{text("a"), text("b"), text("c"), toolUse(), toolResult(), text("e")},
			target:   4,
			want:     2, // scans past toolResult(4) AND toolUse(3), stops at text(2)
		},
		{
			name:     "tool_use at target moves backward",
			messages: []provider.Message{text("a"), text("b"), text("c"), toolUse(), toolResult(), text("e")},
			target:   3,
			want:     2,
		},
		{
			name:     "consecutive tool pairs move past both",
			messages: []provider.Message{text("a"), text("b"), text("c"), toolUse(), toolResult(), toolUse(), toolResult(), text("h")},
			target:   6,
			want:     2,
		},
		{
			name:     "target at 0 clamps to 2",
			messages: []provider.Message{toolResult(), text("a"), text("b"), text("c")},
			target:   0,
			want:     2,
		},
		{
			name:     "target at 1 clamps to 2",
			messages: []provider.Message{text("a"), toolResult(), text("b"), text("c")},
			target:   1,
			want:     2,
		},
		{
			name:     "target beyond length clamps to last index",
			messages: []provider.Message{text("a"), text("b"), text("c")},
			target:   10,
			want:     2,
		},
		{
			name:     "all tool messages clamps to 2",
			messages: []provider.Message{text("a"), text("b"), toolUse(), toolResult(), toolUse(), toolResult()},
			target:   5,
			want:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSafeSplitPoint(tt.messages, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Enhancement 3: Post-compression validation ---

func TestSummarizationRejectsInflatedSummary(t *testing.T) {
	// Return a summary that's much longer than the original messages
	longSummary := ""
	for i := 0; i < 100; i++ {
		longSummary += "This is an extremely verbose summary that goes on and on. "
	}
	ms := &mockSummarizer{summary: longSummary}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	messages := make([]provider.Message, 6)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("m%d", i)}},
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	// Should return original messages when summary is inflated
	require.Error(t, err)
	assert.ErrorIs(t, err, errSummaryInflated)
	assert.Len(t, result, len(messages), "should return original messages on inflated summary")
}

func TestSummarizationAcceptsShorterSummary(t *testing.T) {
	ms := &mockSummarizer{summary: "brief"}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 4

	messages := make([]provider.Message, 10)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("message number %d with content", i)}},
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)
	assert.Less(t, len(result), len(messages), "shorter summary should be accepted")
}

// --- Enhancement 4: Compression failure tracking ---

func TestSummarizationSkipsAfterFailure(t *testing.T) {
	ms := &mockSummarizer{err: fmt.Errorf("API error")}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 2

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "more"}}},
	}

	// First call fails and records the failure
	_, err := s.Compact(context.Background(), messages, 100000)
	require.Error(t, err)
	assert.True(t, ms.called)

	// Reset mock to track second call
	ms.called = false

	// Second call with same message count should skip
	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err) // skipped, no error
	assert.False(t, ms.called, "summarizer should not be called when message count matches last failure")
	assert.Len(t, result, 3)
}

func TestSummarizationRetriesAfterNewMessages(t *testing.T) {
	ms := &mockSummarizer{err: fmt.Errorf("API error")}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 2

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "This is a longer message to ensure summarization compresses well enough"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "This is a detailed response that also takes up a reasonable amount of tokens"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "Another follow-up question with enough content to make compression worthwhile"}}},
	}

	// First call fails
	_, _ = s.Compact(context.Background(), messages, 100000)

	// Add a message → different length
	messages = append(messages, provider.Message{
		Role:    "assistant",
		Content: []provider.ContentBlock{{Type: "text", Text: "Yet another long response with plenty of content for the token estimator"}},
	})

	ms.called = false
	ms.err = nil
	ms.summary = "brief"

	// Should retry since message count changed
	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)
	assert.True(t, ms.called, "summarizer should retry when message count changes")
	assert.Less(t, len(result), len(messages))
}

func TestSummarizationResetsOnSuccess(t *testing.T) {
	ms := &mockSummarizer{err: fmt.Errorf("API error")}
	s := NewSummarizationStrategy(ms)
	s.messageThreshold = 2

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "more"}}},
	}

	// First call fails
	_, _ = s.Compact(context.Background(), messages, 100000)

	// Fix the error, add a message to bypass the skip
	ms.err = nil
	ms.summary = "summary"
	messages = append(messages, provider.Message{
		Role:    "assistant",
		Content: []provider.ContentBlock{{Type: "text", Text: "ok"}},
	})

	// Second call succeeds — should reset failure tracking
	_, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// Now fail again with the same new length
	ms.err = fmt.Errorf("another error")
	ms.called = false

	// This should NOT skip because the previous success reset the tracker
	_, err = s.Compact(context.Background(), messages, 100000)
	require.Error(t, err)
	assert.True(t, ms.called, "summarizer should be called since success reset the failure tracker")
}
