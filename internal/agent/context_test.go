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

func TestContextManagerTruncateSkipsLeadingToolResult(t *testing.T) {
	cm := NewContextManager(30)
	conv := NewConversation("s")

	// Leading tool_result message — should not be removed since it would
	// orphan it from its tool_use.
	conv.messages = append(conv.messages, provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: "result data"},
		},
	})
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "got it"}})
	conv.AddUser("next question which is long enough to blow the budget completely over the limit")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "long answer that also contributes to exceeding the token budget significantly"}})

	cm.Truncate(conv)

	// Should have removed the pair after the tool_result, keeping at least 2.
	assert.GreaterOrEqual(t, len(conv.Messages()), 2)
}

func TestContextManagerTruncateAllToolResults(t *testing.T) {
	cm := NewContextManager(5) // Very small budget
	conv := NewConversation("s")

	// All messages are tool_results — truncation should break out
	// rather than looping forever.
	conv.messages = []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "r1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t2", Text: "r2"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t3", Text: "r3 with lots of extra text to exceed budget"}}},
	}

	// Should not infinite loop — break when remove <= 0.
	cm.Truncate(conv)
	assert.GreaterOrEqual(t, len(conv.Messages()), 2)
}

func TestHasToolResult(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: "data"},
		},
	}
	assert.True(t, hasToolResult(msg))

	msg2 := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "text", Text: "hello"},
		},
	}
	assert.False(t, hasToolResult(msg2))
}

func TestContextBudgetEffectiveWindow(t *testing.T) {
	b := ContextBudget{Total: 100000, MaxOutputTokens: 4096}
	assert.Equal(t, 95904, b.EffectiveWindow())
}

func TestContextBudgetEffectiveWindowZeroOutput(t *testing.T) {
	b := ContextBudget{Total: 100000, MaxOutputTokens: 0}
	assert.Equal(t, 100000, b.EffectiveWindow())
}
