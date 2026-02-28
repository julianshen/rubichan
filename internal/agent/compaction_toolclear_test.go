package agent

import (
	"context"
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

	result, err := s.Compact(context.Background(), messages, 100000)
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

	result, err := s.Compact(context.Background(), messages, 100000)
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

	result, err := s.Compact(context.Background(), messages, 100000)
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

	result, err := s.Compact(context.Background(), messages, 100000)
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

	result, err := s.Compact(context.Background(), messages, 100000)
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

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// Non-tool_result content should not be modified
	assert.Equal(t, largeText, result[0].Content[0].Text)
}

func TestToolClearingImplementsSignalAware(t *testing.T) {
	s := NewToolResultClearingStrategy()
	var _ SignalAware = s // compile-time check
	assert.NotNil(t, s)
}

func TestToolClearingWithoutSignals(t *testing.T) {
	// Default behavior: 50% cutoff, 1024 threshold — no signals injected.
	s := &toolResultClearingStrategy{threshold: 100}

	largeContent := strings.Repeat("x", 200)
	messages := make([]provider.Message, 20)
	for i := range messages {
		if i == 0 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", Text: largeContent},
			}}
		} else if i%2 == 0 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "text", Text: "msg"},
			}}
		} else {
			messages[i] = provider.Message{Role: "assistant", Content: []provider.ContentBlock{
				{Type: "text", Text: "resp"},
			}}
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// Default cutoff = 50% = 10. Message[0] is in oldest half, should be cleared.
	assert.Contains(t, result[0].Content[0].Text, "[Tool result cleared")
}

func TestToolClearingHighToolDensity(t *testing.T) {
	s := NewToolResultClearingStrategy()
	s.SetSignals(ConversationSignals{ToolCallDensity: 0.7, ErrorDensity: 0.0, MessageCount: 20})

	largeContent := strings.Repeat("x", 2000)
	// 20 messages, tool result at index 12 (past default 50% but within 65%)
	messages := make([]provider.Message, 20)
	for i := range messages {
		if i == 12 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", Text: largeContent},
			}}
		} else if i%2 == 0 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "text", Text: "msg"},
			}}
		} else {
			messages[i] = provider.Message{Role: "assistant", Content: []provider.ContentBlock{
				{Type: "text", Text: "resp"},
			}}
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// With high tool density, cutoff moves to 65%. Index 12 < 13 (65% of 20).
	assert.Contains(t, result[12].Content[0].Text, "[Tool result cleared")
}

func TestToolClearingHighErrorDensity(t *testing.T) {
	s := NewToolResultClearingStrategy()
	s.SetSignals(ConversationSignals{ErrorDensity: 0.4, ToolCallDensity: 0.0, MessageCount: 20})

	largeContent := strings.Repeat("x", 2000)
	// 20 messages, tool result at index 8 (past 35% cutoff=7 but within default 50%)
	messages := make([]provider.Message, 20)
	for i := range messages {
		if i == 8 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", Text: largeContent},
			}}
		} else if i%2 == 0 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "text", Text: "msg"},
			}}
		} else {
			messages[i] = provider.Message{Role: "assistant", Content: []provider.ContentBlock{
				{Type: "text", Text: "resp"},
			}}
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// With high error density, cutoff shrinks to 35%. Index 8 > 7. Not cleared.
	assert.Equal(t, largeContent, result[8].Content[0].Text)
}

func TestToolClearingBothSignals(t *testing.T) {
	s := NewToolResultClearingStrategy()
	s.SetSignals(ConversationSignals{ErrorDensity: 0.4, ToolCallDensity: 0.7, MessageCount: 20})

	largeContent := strings.Repeat("x", 2000)
	// ErrorDensity wins for cutoff → 35%. Tool result at index 8 should NOT be cleared.
	messages := make([]provider.Message, 20)
	for i := range messages {
		if i == 8 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", Text: largeContent},
			}}
		} else if i%2 == 0 {
			messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{
				{Type: "text", Text: "msg"},
			}}
		} else {
			messages[i] = provider.Message{Role: "assistant", Content: []provider.ContentBlock{
				{Type: "text", Text: "resp"},
			}}
		}
	}

	result, err := s.Compact(context.Background(), messages, 100000)
	require.NoError(t, err)

	// ErrorDensity wins — cutoff 35%, threshold still lowered (768).
	// Index 8 > 7 (35% of 20), so not cleared.
	assert.Equal(t, largeContent, result[8].Content[0].Text)
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

	cm.Compact(context.Background(), conv)

	// After compaction, the large tool result should be cleared
	assert.False(t, cm.ExceedsBudget(conv))
}
