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
