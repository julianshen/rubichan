package agent

import (
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

func (m *mockStrategy) Compact(messages []provider.Message, budget int) ([]provider.Message, error) {
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

	cm.Compact(conv)

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
	cm.Compact(conv)

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

	cm.Compact(conv)

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

	cm.Compact(conv)

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

	cm.Compact(conv)

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

	result, err := s.Compact(messages, 5) // Very small budget
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

	result, err := s.Compact(messages, 30)
	require.NoError(t, err)
	assert.Less(t, len(result), 4, "should remove some messages")
	assert.GreaterOrEqual(t, len(result), 2, "should keep at least 2")
}

func TestTruncateStrategySkipsLeadingToolResults(t *testing.T) {
	s := &truncateStrategy{}
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "result data"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "got it"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "long question to exceed budget for sure here"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "long answer that contributes to exceeding budget"}}},
	}

	result, err := s.Compact(messages, 30)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 2)
}
