package agent

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

// Truncate removes the oldest 2 messages (user+assistant pair) at a time
// until the conversation is within budget. Always keeps at least 2 messages.
func (cm *ContextManager) Truncate(conv *Conversation) {
	for cm.ExceedsBudget(conv) && len(conv.messages) > 2 {
		// Remove the oldest 2 messages (a user+assistant pair)
		remove := 2
		if len(conv.messages)-remove < 2 {
			remove = len(conv.messages) - 2
		}
		if remove <= 0 {
			break
		}
		conv.messages = conv.messages[remove:]
	}
}
