package agent

import "github.com/julianshen/rubichan/internal/provider"

// ContextManager tracks token usage and truncates conversation history
// to stay within a configured budget.
type ContextManager struct {
	budget int
}

// NewContextManager creates a new ContextManager with the given token budget.
func NewContextManager(budget int) *ContextManager {
	return &ContextManager{budget: budget}
}

// EstimateTokens estimates the token count for a conversation using
// a ~4 chars per token heuristic with +10 overhead per content block.
func (cm *ContextManager) EstimateTokens(conv *Conversation) int {
	total := 0

	// System prompt tokens
	total += len(conv.SystemPrompt())/4 + 10

	// Message tokens
	for _, msg := range conv.messages {
		for _, block := range msg.Content {
			chars := len(block.Text) + len(block.ID) + len(block.Name) +
				len(block.ToolUseID) + len(block.Input)
			total += chars/4 + 10
		}
	}

	return total
}

// ExceedsBudget returns true if the estimated token count exceeds the budget.
func (cm *ContextManager) ExceedsBudget(conv *Conversation) bool {
	return cm.EstimateTokens(conv) > cm.budget
}

// Truncate removes the oldest messages until the conversation is within budget.
// It removes in pairs (user+assistant) and skips leading tool_result messages
// to avoid orphaning them from their tool_use. Always keeps at least 2 messages.
func (cm *ContextManager) Truncate(conv *Conversation) {
	for cm.ExceedsBudget(conv) && len(conv.messages) > 2 {
		// Find a safe removal boundary: skip any leading tool_result messages
		// since removing them without their tool_use would corrupt the conversation.
		start := 0
		for start < len(conv.messages) && hasToolResult(conv.messages[start]) {
			start++
		}

		// Remove 2 messages (a user+assistant pair) starting from the safe boundary.
		remove := start + 2
		if remove > len(conv.messages)-2 {
			remove = len(conv.messages) - 2
		}
		if remove <= 0 {
			break
		}
		conv.messages = conv.messages[remove:]
	}
}

// hasToolResult returns true if any content block in the message is a tool_result.
func hasToolResult(msg provider.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
}
