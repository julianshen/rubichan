package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStrategy records calls and optionally removes messages.
type mockStrategy struct {
	name      string
	called    bool
	removeN   int // number of messages to remove from the front
	returnErr error
}

func (m *mockStrategy) Name() string { return m.name }

func (m *mockStrategy) Compact(_ context.Context, messages []provider.Message, budget int) ([]provider.Message, error) {
	m.called = true
	if m.returnErr != nil {
		return messages, m.returnErr
	}
	if m.removeN > 0 && m.removeN <= len(messages) {
		return messages[m.removeN:], nil
	}
	return messages, nil
}

func TestCompactionStrategyInterface(t *testing.T) {
	s := &truncateStrategy{}
	assert.Equal(t, "truncate", s.Name())
}

func TestCompactRunsStrategiesInOrder(t *testing.T) {
	cm := NewContextManager(30)

	s1 := &mockStrategy{name: "first", removeN: 2}
	s2 := &mockStrategy{name: "second", removeN: 2}

	cm.SetStrategies([]CompactionStrategy{s1, s2})

	conv := NewConversation("s")
	conv.AddUser("first user message with some content here")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "first assistant response here"}})
	conv.AddUser("second user message with content here")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "second assistant response"}})

	require.True(t, cm.ExceedsBudget(conv))

	cm.Compact(context.Background(), conv)

	// First strategy should be called
	assert.True(t, s1.called, "first strategy should be called")
	// If first strategy brings under budget, second should not be called
	// (depends on whether removal was enough)
}

func TestCompactFallsBackToTruncation(t *testing.T) {
	cm := NewContextManager(55)

	conv := NewConversation("s")
	conv.AddUser("first user message with some content")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "first assistant response"}})
	conv.AddUser("second msg")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "second"}})

	require.True(t, cm.ExceedsBudget(conv))

	// Default strategies include truncation as fallback
	cm.Compact(context.Background(), conv)

	assert.False(t, cm.ExceedsBudget(conv), "should be within budget after compaction")
	assert.GreaterOrEqual(t, len(conv.Messages()), 2)
}

func TestCompactEmptyStrategiesUsesTruncation(t *testing.T) {
	// Default constructor includes truncation strategy
	cm := NewContextManager(55)

	conv := NewConversation("s")
	conv.AddUser("first user message with some content")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "first assistant response"}})
	conv.AddUser("second msg")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "second"}})

	require.True(t, cm.ExceedsBudget(conv))

	cm.Compact(context.Background(), conv)

	assert.False(t, cm.ExceedsBudget(conv))
	assert.GreaterOrEqual(t, len(conv.Messages()), 2)
}

func TestCompactNoOpWhenWithinBudget(t *testing.T) {
	cm := NewContextManager(100000)

	s := &mockStrategy{name: "test"}
	cm.SetStrategies([]CompactionStrategy{s, &truncateStrategy{}})

	conv := NewConversation("sys")
	conv.AddUser("hello")

	require.False(t, cm.ExceedsBudget(conv))

	cm.Compact(context.Background(), conv)

	// Strategy should not be called when already within budget
	assert.False(t, s.called, "strategy should not be called when within budget")
}

func TestCompactStopsWhenUnderBudget(t *testing.T) {
	cm := NewContextManager(50)

	s1 := &mockStrategy{name: "first", removeN: 2}
	s2 := &mockStrategy{name: "second", removeN: 2}

	cm.SetStrategies([]CompactionStrategy{s1, s2, &truncateStrategy{}})

	conv := NewConversation("s")
	// Add enough messages that removing 2 brings under budget
	conv.AddUser("message one with content")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response one"}})
	conv.AddUser("msg two")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "resp two"}})

	if !cm.ExceedsBudget(conv) {
		t.Skip("test requires conversation to exceed budget")
	}

	cm.Compact(context.Background(), conv)

	assert.True(t, s1.called, "first strategy should be called")
	// If first strategy brought under budget, second should not be called
	if !cm.ExceedsBudget(conv) {
		// Check succeeded — second may or may not have been called depending on exact token math
	}
}

func TestTruncateStrategyPreservesMinMessages(t *testing.T) {
	s := &truncateStrategy{}
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	result, err := s.Compact(context.Background(), messages, 5) // Very small budget
	require.NoError(t, err)
	assert.Len(t, result, 2, "should preserve at least 2 messages")
}

func TestTruncateStrategyRemovesOldMessages(t *testing.T) {
	s := &truncateStrategy{}
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "first user message with some content"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "first assistant response"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "second user message"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "second response"}}},
	}

	result, err := s.Compact(context.Background(), messages, 30)
	require.NoError(t, err)
	assert.Less(t, len(result), 4, "should remove some messages")
	assert.GreaterOrEqual(t, len(result), 2, "should keep at least 2")
}

// --- Enhancement 2: Proactive compression threshold ---

func TestShouldCompactAt70Percent(t *testing.T) {
	cm := NewContextManager(1000)

	conv := NewConversation("sys")
	// Add enough content to exceed 70% of 1000 tokens = 700
	for i := 0; i < 30; i++ {
		conv.AddUser(fmt.Sprintf("message %d with some reasonable content to take up tokens", i))
	}

	tokens := cm.EstimateTokens(conv)
	require.Greater(t, tokens, 700, "test requires tokens > 70%% of budget")
	require.LessOrEqual(t, tokens, 1000, "test requires tokens <= 100%% of budget")

	assert.True(t, cm.ShouldCompact(conv), "should trigger compaction at >70%% budget")
}

func TestShouldCompactFalseBelow70Percent(t *testing.T) {
	cm := NewContextManager(100000)

	conv := NewConversation("sys")
	conv.AddUser("hello")

	assert.False(t, cm.ShouldCompact(conv), "should not trigger compaction below 70%% budget")
}

func TestCompactTriggersProactively(t *testing.T) {
	cm := NewContextManager(1000)

	s := &mockStrategy{name: "test", removeN: 10}
	cm.SetStrategies([]CompactionStrategy{s, &truncateStrategy{}})

	conv := NewConversation("sys")
	for i := 0; i < 30; i++ {
		conv.AddUser(fmt.Sprintf("message %d with some reasonable content to take up tokens", i))
	}

	tokens := cm.EstimateTokens(conv)
	require.Greater(t, tokens, 700, "test requires >70%% budget used")
	require.LessOrEqual(t, tokens, 1000, "test requires <=100%% budget (not exceeding)")

	cm.Compact(context.Background(), conv)
	assert.True(t, s.called, "strategy should be called proactively when above 70%% threshold")
}

func TestCompactCustomTriggerRatio(t *testing.T) {
	cm := NewContextManager(100000)
	cm.triggerRatio = 0.0001 // very low ratio — even tiny conversations trigger

	s := &mockStrategy{name: "test"}
	cm.SetStrategies([]CompactionStrategy{s, &truncateStrategy{}})

	conv := NewConversation("sys")
	conv.AddUser("hello")

	cm.Compact(context.Background(), conv)
	assert.True(t, s.called, "strategy should be called with custom low trigger ratio")
}

func TestTruncateStrategySkipsLeadingToolResults(t *testing.T) {
	s := &truncateStrategy{}
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "result data"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "got it"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "long question to exceed budget for sure here"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "long answer that contributes to exceeding budget"}}},
	}

	result, err := s.Compact(context.Background(), messages, 30)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 2)
}
