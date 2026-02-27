package agent

import "github.com/julianshen/rubichan/internal/provider"

// CompactionStrategy defines a strategy for reducing conversation size.
// Strategies are run in order from lightest to heaviest; the chain stops
// once the conversation fits within the token budget.
type CompactionStrategy interface {
	Name() string
	Compact(messages []provider.Message, budget int) ([]provider.Message, error)
}

// ContextManager tracks token usage and compacts conversation history
// to stay within a configured budget using a chain of strategies.
type ContextManager struct {
	budget     int
	strategies []CompactionStrategy
}

// NewContextManager creates a new ContextManager with the given token budget.
// The default strategy chain contains only truncation.
func NewContextManager(budget int) *ContextManager {
	return &ContextManager{
		budget: budget,
		strategies: []CompactionStrategy{
			NewToolResultClearingStrategy(),
			&truncateStrategy{},
		},
	}
}

// SetStrategies replaces the compaction strategy chain.
func (cm *ContextManager) SetStrategies(strategies []CompactionStrategy) {
	cm.strategies = strategies
}

// Compact runs the compaction strategy chain until the conversation fits
// within the token budget. Strategies are tried in order; the chain stops
// as soon as the conversation is under budget.
func (cm *ContextManager) Compact(conv *Conversation) {
	if !cm.ExceedsBudget(conv) {
		return
	}
	// Subtract system prompt overhead so strategies only need to fit messages.
	systemTokens := len(conv.SystemPrompt())/4 + 10
	messageBudget := cm.budget - systemTokens
	if messageBudget < 0 {
		messageBudget = 0
	}
	for _, s := range cm.strategies {
		if !cm.ExceedsBudget(conv) {
			return
		}
		result, err := s.Compact(conv.messages, messageBudget)
		if err != nil {
			continue
		}
		conv.messages = result
	}
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
// Deprecated: use Compact() which runs the full strategy chain.
func (cm *ContextManager) Truncate(conv *Conversation) {
	cm.Compact(conv)
}

// truncateStrategy is the last-resort compaction strategy that removes
// the oldest message pairs to fit within the token budget.
type truncateStrategy struct{}

func (s *truncateStrategy) Name() string { return "truncate" }

func (s *truncateStrategy) Compact(messages []provider.Message, budget int) ([]provider.Message, error) {
	// Estimate tokens inline using the same heuristic as ContextManager.
	estimateTokens := func(msgs []provider.Message) int {
		total := 0
		for _, msg := range msgs {
			for _, block := range msg.Content {
				chars := len(block.Text) + len(block.ID) + len(block.Name) +
					len(block.ToolUseID) + len(block.Input)
				total += chars/4 + 10
			}
		}
		return total
	}

	for estimateTokens(messages) > budget && len(messages) > 2 {
		start := 0
		for start < len(messages) && hasToolResult(messages[start]) {
			start++
		}

		remove := start + 2
		if remove > len(messages)-2 {
			remove = len(messages) - 2
		}
		if remove <= 0 {
			break
		}
		messages = messages[remove:]
	}
	return messages, nil
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
