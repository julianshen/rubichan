package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestContextManagerEstimateTokens(t *testing.T) {
	cm := NewContextManager(100000, 0)
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
	cm := NewContextManager(20, 0)
	conv := NewConversation("sys")

	// "sys" = 3 chars / 4 = 0, + 10 = 10
	assert.False(t, cm.ExceedsBudget(conv))

	// Add a message with enough chars to exceed budget
	conv.AddUser("this is a long enough message to exceed the budget")
	assert.True(t, cm.ExceedsBudget(conv))
}

func TestContextManagerExceedsBudgetNotExceeded(t *testing.T) {
	cm := NewContextManager(100000, 0)
	conv := NewConversation("system prompt")
	conv.AddUser("hello")
	assert.False(t, cm.ExceedsBudget(conv))
}

func TestContextManagerTruncate(t *testing.T) {
	cm := NewContextManager(50, 0)
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
	cm := NewContextManager(10, 0) // Very small budget
	conv := NewConversation("system")
	conv.AddUser("hello world this is a very long message that exceeds the budget easily")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response that is also long enough"}})

	// Even though it exceeds budget, with only 2 messages we should keep them
	cm.Truncate(conv)
	assert.Len(t, conv.Messages(), 2, "should keep at least 2 messages")
}

func TestContextManagerTruncateSkipsLeadingToolResult(t *testing.T) {
	cm := NewContextManager(30, 0)
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
	cm := NewContextManager(5, 0) // Very small budget
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

func TestContextBudgetUsedTokens(t *testing.T) {
	b := ContextBudget{
		Total:            100000,
		MaxOutputTokens:  4096,
		SystemPrompt:     500,
		SkillPrompts:     200,
		ToolDescriptions: 3000,
		Conversation:     10000,
	}
	assert.Equal(t, 13700, b.UsedTokens())
}

func TestContextBudgetRemainingTokens(t *testing.T) {
	b := ContextBudget{
		Total:            100000,
		MaxOutputTokens:  4096,
		SystemPrompt:     500,
		SkillPrompts:     200,
		ToolDescriptions: 3000,
		Conversation:     10000,
	}
	assert.Equal(t, 82204, b.RemainingTokens()) // 95904 - 13700
}

func TestContextBudgetUsedPercentage(t *testing.T) {
	b := ContextBudget{
		Total:           100000,
		MaxOutputTokens: 0,
		SystemPrompt:    50000,
		Conversation:    50000,
	}
	assert.InDelta(t, 1.0, b.UsedPercentage(), 0.001)
}

func TestContextBudgetUsedPercentageZeroWindow(t *testing.T) {
	b := ContextBudget{Total: 0, MaxOutputTokens: 0}
	assert.Equal(t, 1.0, b.UsedPercentage())
}

func TestNewContextManagerWithBudget(t *testing.T) {
	cm := NewContextManager(100000, 4096)
	assert.NotNil(t, cm)
}

func TestContextManagerShouldCompactAt95Percent(t *testing.T) {
	cm := NewContextManager(1000, 100) // effective window = 900, threshold = 855
	conv := NewConversation("")

	// System prompt "" contributes 10 tokens overhead.
	// makeStringOfTokens(840) => 840 tokens => total = 10 + 840 = 850 < 855
	conv.AddUser(makeStringOfTokens(840))
	assert.False(t, cm.ShouldCompact(conv), "below 95%% should not trigger")

	conv.Clear()
	// makeStringOfTokens(850) => 850 tokens => total = 10 + 850 = 860 > 855
	conv.AddUser(makeStringOfTokens(850))
	assert.True(t, cm.ShouldCompact(conv), "above 95%% should trigger")
}

func TestContextManagerIsBlocked(t *testing.T) {
	cm := NewContextManager(1000, 100) // effective window = 900, threshold = 882
	conv := NewConversation("")

	// System prompt "" contributes 10 tokens overhead.
	// makeStringOfTokens(870) => 870 tokens => total = 10 + 870 = 880 < 882
	conv.AddUser(makeStringOfTokens(870))
	assert.False(t, cm.IsBlocked(conv), "below 98%% should not block")

	conv.Clear()
	// makeStringOfTokens(880) => 880 tokens => total = 10 + 880 = 890 > 882
	conv.AddUser(makeStringOfTokens(880))
	assert.True(t, cm.IsBlocked(conv), "above 98%% should block")
}

func TestContextManagerMeasureUsage(t *testing.T) {
	cm := NewContextManager(100000, 4096)

	systemPrompt := "You are a helpful assistant"
	conv := NewConversation(systemPrompt)
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi there"}})

	toolDefs := []provider.ToolDef{
		{Name: "shell", Description: "Execute shell commands", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	skillPrompt := "## Skill\nDo stuff"

	cm.MeasureUsage(conv, systemPrompt, skillPrompt, toolDefs)

	assert.Greater(t, cm.budget.SystemPrompt, 0)
	assert.Greater(t, cm.budget.SkillPrompts, 0)
	assert.Greater(t, cm.budget.ToolDescriptions, 0)
	assert.Greater(t, cm.budget.Conversation, 0)
}

// makeStringOfTokens returns a string that estimates to approximately n tokens.
func makeStringOfTokens(n int) string {
	chars := (n - 10) * 4
	if chars < 0 {
		chars = 0
	}
	return strings.Repeat("a", chars)
}
