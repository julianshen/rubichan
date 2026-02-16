package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestContextManagerEstimateTokens(t *testing.T) {
	cm := NewContextManager(100000)
	conv := NewConversation("system prompt")

	// System prompt: "system prompt" = 13 chars / 4 = 3, + 10 overhead = 13
	tokens := cm.EstimateTokens(conv)
	assert.Equal(t, 13, tokens)

	// Add a user message: "hello" = 5 chars / 4 = 1, + 10 overhead = 11
	conv.AddUser("hello")
	tokens = cm.EstimateTokens(conv)
	assert.Equal(t, 24, tokens) // 13 (system) + 11 (user)
}

func TestContextManagerExceedsBudget(t *testing.T) {
	// Very small budget
	cm := NewContextManager(20)
	conv := NewConversation("sys")

	// "sys" = 3 chars / 4 = 0, + 10 = 10
	assert.False(t, cm.ExceedsBudget(conv))

	// Add a message with enough chars to exceed budget
	conv.AddUser("this is a long enough message to exceed the budget")
	assert.True(t, cm.ExceedsBudget(conv))
}

func TestContextManagerExceedsBudgetNotExceeded(t *testing.T) {
	cm := NewContextManager(100000)
	conv := NewConversation("system prompt")
	conv.AddUser("hello")
	assert.False(t, cm.ExceedsBudget(conv))
}

func TestContextManagerTruncate(t *testing.T) {
	cm := NewContextManager(50)
	conv := NewConversation("sys")

	// Add several message pairs to exceed budget
	conv.AddUser("first user message with some content")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "first assistant response"}})
	conv.AddUser("second user message with content")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "second assistant response"}})
	conv.AddUser("third user message")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "third response"}})

	assert.True(t, cm.ExceedsBudget(conv))

	cm.Truncate(conv)

	// After truncation, should be within budget
	assert.False(t, cm.ExceedsBudget(conv))
	// Should keep at least 2 messages
	assert.GreaterOrEqual(t, len(conv.Messages()), 2)
}

func TestContextManagerSmallConversationNoTruncation(t *testing.T) {
	cm := NewContextManager(10) // Very small budget
	conv := NewConversation("system")
	conv.AddUser("hello world this is a very long message that exceeds the budget easily")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response that is also long enough"}})

	// Even though it exceeds budget, with only 2 messages we should keep them
	cm.Truncate(conv)
	assert.Len(t, conv.Messages(), 2, "should keep at least 2 messages")
}
