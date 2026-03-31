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
// Returns the original slice unmodified when no orphans are found.
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

	// Quick check: any orphans exist?
	hasOrphans := false
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ID != "" && !resultIDs[block.ID] {
				hasOrphans = true
				break
			}
		}
		if hasOrphans {
			break
		}
	}
	if !hasOrphans {
		return messages
	}

	// Remove orphaned tool_use blocks.
	out := make([]provider.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "assistant" {
			out = append(out, msg)
			continue
		}
		var filtered []provider.ContentBlock
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ID != "" && !resultIDs[block.ID] {
				continue
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
// message. Returns the original slice when no merging is needed.
func mergeConsecutiveAssistant(messages []provider.Message) []provider.Message {
	if len(messages) <= 1 {
		return messages
	}

	// Quick check: any consecutive assistants?
	needsMerge := false
	for i := 1; i < len(messages); i++ {
		if messages[i].Role == "assistant" && messages[i-1].Role == "assistant" {
			needsMerge = true
			break
		}
	}
	if !needsMerge {
		return messages
	}

	out := make([]provider.Message, 0, len(messages))
	for _, msg := range messages {
		if len(out) > 0 && out[len(out)-1].Role == msg.Role && msg.Role == "assistant" {
			out[len(out)-1].Content = append(out[len(out)-1].Content, msg.Content...)
		} else {
			out = append(out, msg)
		}
	}
	return out
}
