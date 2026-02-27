package agent

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// toolResultClearingStrategy replaces large tool_result content blocks
// with a short placeholder. Only clears results in the oldest 50% of
// the conversation to preserve results the agent may still reference.
type toolResultClearingStrategy struct {
	threshold int // minimum byte size to trigger clearing
}

// NewToolResultClearingStrategy creates a strategy with the default 1KB threshold.
func NewToolResultClearingStrategy() *toolResultClearingStrategy {
	return &toolResultClearingStrategy{threshold: 1024}
}

func (s *toolResultClearingStrategy) Name() string { return "tool_result_clearing" }

func (s *toolResultClearingStrategy) Compact(messages []provider.Message, _ int) ([]provider.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	// Only process the oldest 50% of messages.
	cutoff := len(messages) / 2

	// Deep-copy only the messages we might modify.
	result := make([]provider.Message, len(messages))
	copy(result, messages)

	for i := 0; i < cutoff; i++ {
		msg := result[i]
		modified := false
		newContent := make([]provider.ContentBlock, len(msg.Content))
		copy(newContent, msg.Content)

		for j, block := range newContent {
			if block.Type == "tool_result" && len(block.Text) >= s.threshold {
				newContent[j].Text = fmt.Sprintf("[Tool result cleared — was %d bytes]", len(block.Text))
				modified = true
			}
		}

		if modified {
			result[i] = provider.Message{
				Role:    msg.Role,
				Content: newContent,
			}
		}
	}

	return result, nil
}
