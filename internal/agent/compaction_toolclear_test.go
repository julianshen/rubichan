package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolResultClearingStrategyName(t *testing.T) {
	s := &toolResultClearingStrategy{}
	assert.Equal(t, "tool_result_clearing", s.Name())
}

func TestToolResultClearingLargeResultsCleared(t *testing.T) {
	s := &toolResultClearingStrategy{threshold: 100}

	largeContent := strings.Repeat("x", 200)
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: largeContent}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "next"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "response"}}},
	}

	result, err := s.Compact(messages, 100000)
	require.NoError(t, err)

	// The first message's tool_result should be cleared
	assert.Contains(t, result[0].Content[0].Text, "[Tool result cleared")
	assert.NotEqual(t, largeContent, result[0].Content[0].Text)
}

func TestToolResultClearingSmallResultsPreserved(t *testing.T) {
	s := &toolResultClearingStrategy{threshold: 100}

	smallContent := "short result"
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: smallContent}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "next"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "response"}}},
	}

	result, err := s.Compact(messages, 100000)
	require.NoError(t, err)

	// Small results should be preserved
	assert.Equal(t, smallContent, result[0].Content[0].Text)
}

func TestToolResultClearingRecentResultsUntouched(t *testing.T) {
	s := &toolResultClearingStrategy{threshold: 100}

	largeContent := strings.Repeat("x", 200)
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "first"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		// Recent messages (in the newest 50%)
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: largeContent}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "got it"}}},
	}

	result, err := s.Compact(messages, 100000)
	require.NoError(t, err)

	// Recent tool results should be preserved even if large
	assert.Equal(t, largeContent, result[2].Content[0].Text)
}

func TestToolResultClearingThresholdConfigurable(t *testing.T) {
	s := &toolResultClearingStrategy{threshold: 50}

	content60 := strings.Repeat("a", 60) // Over 50 threshold
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: content60}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "next"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "response"}}},
	}

	result, err := s.Compact(messages, 100000)
	require.NoError(t, err)

	assert.Contains(t, result[0].Content[0].Text, "[Tool result cleared")
}

func TestToolResultClearingBytesCountAccurate(t *testing.T) {
	s := &toolResultClearingStrategy{threshold: 100}

	content := strings.Repeat("z", 500)
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: content}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "next"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "response"}}},
	}

	result, err := s.Compact(messages, 100000)
	require.NoError(t, err)

	expected := fmt.Sprintf("[Tool result cleared — was %d bytes]", 500)
	assert.Equal(t, expected, result[0].Content[0].Text)
}

func TestToolResultClearingDefaultThreshold(t *testing.T) {
	s := NewToolResultClearingStrategy()
	assert.Equal(t, 1024, s.threshold)
}

func TestToolResultClearingNonToolResultsUntouched(t *testing.T) {
	s := &toolResultClearingStrategy{threshold: 10}

	largeText := strings.Repeat("x", 200)
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: largeText}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "next"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "response"}}},
	}

	result, err := s.Compact(messages, 100000)
	require.NoError(t, err)

	// Non-tool_result content should not be modified
	assert.Equal(t, largeText, result[0].Content[0].Text)
}

func TestToolResultClearingIntegrationWithCompact(t *testing.T) {
	cm := NewContextManager(80)
	cm.SetStrategies([]CompactionStrategy{
		NewToolResultClearingStrategy(),
		&truncateStrategy{},
	})

	largeContent := strings.Repeat("x", 2000)
	conv := NewConversation("s")
	conv.messages = append(conv.messages, provider.Message{
		Role:    "user",
		Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: largeContent}},
	})
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "ok"}})
	conv.AddUser("next")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response"}})

	require.True(t, cm.ExceedsBudget(conv))

	cm.Compact(conv)

	// After compaction, the large tool result should be cleared
	assert.False(t, cm.ExceedsBudget(conv))
}
