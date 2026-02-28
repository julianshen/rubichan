package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// toolResultClearingStrategy replaces large tool_result content blocks
// with a short placeholder. Only clears results in the oldest portion of
// the conversation to preserve results the agent may still reference.
// When signals are injected, cutoff and threshold adjust dynamically.
type toolResultClearingStrategy struct {
	threshold int                  // minimum byte size to trigger clearing
	signals   *ConversationSignals // optional, injected via SetSignals
}

// NewToolResultClearingStrategy creates a strategy with the default 1KB threshold.
func NewToolResultClearingStrategy() *toolResultClearingStrategy {
	return &toolResultClearingStrategy{threshold: 1024}
}

func (s *toolResultClearingStrategy) Name() string { return "tool_result_clearing" }

// SetSignals injects conversation signals for dynamic threshold adjustment.
func (s *toolResultClearingStrategy) SetSignals(signals ConversationSignals) {
	s.signals = &signals
}

func (s *toolResultClearingStrategy) Compact(_ context.Context, messages []provider.Message, _ int) ([]provider.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	// Default: process oldest 50%.
	cutoffPct := 50
	threshold := s.threshold

	if s.signals != nil {
		// High error density → preserve more context (shrink cutoff to 35%).
		if s.signals.ErrorDensity > 0.3 {
			cutoffPct = 35
		} else if s.signals.ToolCallDensity > 0.6 {
			// High tool density → clear more aggressively (expand cutoff to 65%).
			cutoffPct = 65
		}
		// High tool density → lower threshold to catch smaller results.
		if s.signals.ToolCallDensity > 0.6 {
			threshold = threshold * 3 / 4
		}
	}

	cutoff := len(messages) * cutoffPct / 100

	// Deep-copy only the messages we might modify.
	result := make([]provider.Message, len(messages))
	copy(result, messages)

	for i := 0; i < cutoff; i++ {
		msg := result[i]
		modified := false
		newContent := make([]provider.ContentBlock, len(msg.Content))
		copy(newContent, msg.Content)

		for j, block := range newContent {
			if block.Type == "tool_result" && len(block.Text) >= threshold {
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
