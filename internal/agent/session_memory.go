package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

const (
	// minPreserveTokens is the minimum token count to keep in the
	// compacted conversation. Below this, compaction is not worthwhile.
	minPreserveTokens = 10_000
	// minTextBlockMessages is the minimum number of messages containing
	// text blocks to preserve. Ensures the model retains some context.
	minTextBlockMessages = 5
)

// sessionMemoryStrategy implements CompactionStrategy by calculating
// which messages to keep based on token minimums and API invariants,
// then replacing discarded messages with a summary marker.
//
// It preserves:
//   - tool_use/tool_result pairs (never splits across boundary)
//   - Thinking blocks sharing a message.ID
//   - At least minPreserveTokens of recent context
//   - At least minTextBlockMessages with text content
type sessionMemoryStrategy struct {
	// lastSummarizedMessageID tracks the boundary between preserved and
	// summarized messages across compaction rounds.
	lastSummarizedMessageID string
}

func (s *sessionMemoryStrategy) Name() string { return "session_memory" }

// Compact performs session memory compaction on the conversation.
// It:
//  1. Calculates which messages to keep
//  2. Preserves API invariants at the boundary
//  3. Replaces discarded messages with a summary marker
func (s *sessionMemoryStrategy) Compact(
	_ context.Context,
	messages []provider.Message,
	budget int,
) ([]provider.Message, error) {
	idx := s.calculateMessagesToKeepIndex(messages, estimateMessageTokens)
	idx = adjustIndexToPreserveAPIInvariants(messages, idx)

	if idx <= 0 {
		// Nothing to compact
		return messages, nil
	}

	// Build compacted conversation: summary marker + kept messages
	summaryMsg := provider.Message{
		Role: "system",
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("[Earlier conversation summarized: %d messages removed]", idx),
		}},
	}

	newMessages := make([]provider.Message, 0, 1+len(messages)-idx)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, messages[idx:]...)

	// Use the first content block's ID as a stable boundary marker.
	if idx < len(messages) && len(messages[idx].Content) > 0 {
		s.lastSummarizedMessageID = messages[idx].Content[0].ID
	}
	return newMessages, nil
}

// calculateMessagesToKeepIndex returns the index into messages where
// compaction should split: messages [0:idx) are summarized, [idx:] are kept.
// It ensures:
//   - At least minPreserveTokens are kept
//   - At least minTextBlockMessages with text blocks are kept
func (s *sessionMemoryStrategy) calculateMessagesToKeepIndex(
	messages []provider.Message,
	tokenCounter func([]provider.Message) int,
) int {
	if len(messages) == 0 {
		return 0
	}

	// Find the split index: messages [0:idx) are summarized, [idx:] are kept.
	// We want to keep as much as possible while staying under budget.
	// Start from the beginning and find the earliest index where the
	// remaining messages fit within minPreserveTokens.
	idx := 0
	for i := 0; i <= len(messages); i++ {
		kept := messages[i:]
		if tokenCounter(kept) <= minPreserveTokens {
			idx = i
			break
		}
	}

	// Ensure at least minTextBlockMessages with text blocks are kept.
	textBlockCount := 0
	for i := idx; i < len(messages); i++ {
		if hasTextBlock(messages[i]) {
			textBlockCount++
		}
	}
	for idx > 0 && textBlockCount < minTextBlockMessages {
		idx--
		if hasTextBlock(messages[idx]) {
			textBlockCount++
		}
	}

	return idx
}

// adjustIndexToPreserveAPIInvariants ensures the compaction boundary
// does not split:
//   - tool_use/tool_result pairs (must be in same half)
//   - thinking blocks sharing a message.ID
func adjustIndexToPreserveAPIInvariants(
	messages []provider.Message,
	idx int,
) int {
	if idx <= 0 || idx >= len(messages) {
		return idx
	}

	// Don't split in the middle of a tool_use/tool_result pair.
	// If messages[idx] is a tool_result, move idx forward to include
	// the matching tool_use (the previous assistant message).
	if messages[idx].Role == "user" && isToolResultMessage(messages[idx]) {
		for i := idx - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" && hasToolUseBlock(messages[i]) {
				idx = i // Include the assistant message with tool_use
				break
			}
		}
	}

	// Don't split thinking blocks that share a content block ID with adjacent messages.
	// Some providers emit thinking blocks as separate messages sharing the same block ID.
	if idx > 0 {
		prevID := ""
		if len(messages[idx-1].Content) > 0 {
			prevID = messages[idx-1].Content[0].ID
		}
		currID := ""
		if len(messages[idx].Content) > 0 {
			currID = messages[idx].Content[0].ID
		}
		if prevID != "" && prevID == currID {
			for i := idx - 1; i >= 0; i-- {
				id := ""
				if len(messages[i].Content) > 0 {
					id = messages[i].Content[0].ID
				}
				if id != currID {
					idx = i + 1
					break
				}
			}
		}
	}

	return idx
}

func hasTextBlock(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "text" && c.Text != "" {
			return true
		}
	}
	return false
}

func isToolResultMessage(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "tool_result" {
			return true
		}
	}
	return false
}

func hasToolUseBlock(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "tool_use" {
			return true
		}
	}
	return false
}
