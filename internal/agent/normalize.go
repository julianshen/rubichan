package agent

import "github.com/julianshen/rubichan/internal/provider"

// NormalizeMessages cleans up conversation messages before sending to the LLM.
// It removes orphaned tool_use blocks (those without a matching tool_result)
// and merges consecutive assistant messages.
func NormalizeMessages(messages []provider.Message) []provider.Message {
	messages = removeOrphanedToolCalls(messages)
	messages = mergeConsecutiveAssistant(messages)
	return messages
}

// removeOrphanedToolCalls removes tool_use content blocks from assistant
// messages when no subsequent user message contains a matching tool_result.
// This happens after compaction strips tool results, leaving dangling tool_use
// blocks that confuse the LLM.
func removeOrphanedToolCalls(messages []provider.Message) []provider.Message {
	// Collect all tool_result IDs from user messages.
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				resultIDs[block.ToolUseID] = true
			}
		}
	}

	// Remove tool_use blocks without matching results.
	var out []provider.Message
	for _, msg := range messages {
		if msg.Role != "assistant" {
			out = append(out, msg)
			continue
		}
		var filtered []provider.ContentBlock
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ID != "" && !resultIDs[block.ID] {
				continue // orphaned — skip
			}
			filtered = append(filtered, block)
		}
		if len(filtered) > 0 {
			out = append(out, provider.Message{Role: msg.Role, Content: filtered})
		}
	}
	return out
}

// mergeConsecutiveAssistant merges adjacent assistant messages into a single
// message. Some providers reject conversations with consecutive messages of the
// same role, and compaction can produce such sequences.
func mergeConsecutiveAssistant(messages []provider.Message) []provider.Message {
	if len(messages) <= 1 {
		return messages
	}
	var out []provider.Message
	for _, msg := range messages {
		if len(out) > 0 && out[len(out)-1].Role == msg.Role && msg.Role == "assistant" {
			// Merge content blocks into the previous message.
			out[len(out)-1].Content = append(out[len(out)-1].Content, msg.Content...)
		} else {
			out = append(out, msg)
		}
	}
	return out
}
