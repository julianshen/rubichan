// Package normalize provides message pre-processing utilities shared
// across LLM provider transformers.
package normalize

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// RemoveEmptyMessages filters out messages with no meaningful content.
// Empty text and empty thinking blocks are removed; messages left with
// no blocks after filtering are dropped entirely.
func RemoveEmptyMessages(msgs []agentsdk.Message) []agentsdk.Message {
	var out []agentsdk.Message
	for _, m := range msgs {
		filtered := filterBlocks(m.Content)
		if len(filtered) == 0 {
			continue
		}
		out = append(out, agentsdk.Message{Role: m.Role, Content: filtered})
	}
	return out
}

func filterBlocks(blocks []agentsdk.ContentBlock) []agentsdk.ContentBlock {
	var out []agentsdk.ContentBlock
	for _, b := range blocks {
		switch b.Type {
		case "text", "thinking":
			if b.Text == "" {
				continue
			}
		}
		out = append(out, b)
	}
	return out
}

// ScrubToolIDs applies a scrubbing function to all tool_use IDs and
// tool_result tool_use_ids in the message list.
func ScrubToolIDs(msgs []agentsdk.Message, scrub func(string) string) []agentsdk.Message {
	out := make([]agentsdk.Message, len(msgs))
	for i, m := range msgs {
		blocks := make([]agentsdk.ContentBlock, len(m.Content))
		copy(blocks, m.Content)
		for j := range blocks {
			switch blocks[j].Type {
			case "tool_use":
				blocks[j].ID = scrub(blocks[j].ID)
			case "tool_result":
				blocks[j].ToolUseID = scrub(blocks[j].ToolUseID)
			}
		}
		out[i] = agentsdk.Message{Role: m.Role, Content: blocks}
	}
	return out
}

// ScrubToolIDChars replaces non-alphanumeric characters (except _ and -)
// with underscores. This satisfies tool ID constraints for providers like
// Anthropic that restrict IDs to [a-zA-Z0-9_-].
func ScrubToolIDChars(id string) string {
	// Short-circuit: scan first to avoid allocation when ID is already clean.
	clean := true
	for _, r := range id {
		if !isToolIDRune(r) {
			clean = false
			break
		}
	}
	if clean {
		return id
	}

	runes := make([]rune, 0, len(id))
	for _, r := range id {
		if isToolIDRune(r) {
			runes = append(runes, r)
		} else {
			runes = append(runes, '_')
		}
	}
	return string(runes)
}

func isToolIDRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}

// ScrubAnthropicToolID is a backward-compatible alias for ScrubToolIDChars.
func ScrubAnthropicToolID(id string) string { return ScrubToolIDChars(id) }

// TruncateToolID truncates a tool ID to maxLen runes.
// A maxLen of 0 means no limit.
func TruncateToolID(id string, maxLen int) string {
	if maxLen <= 0 {
		return id
	}
	runes := []rune(id)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return id
}

// InsertAssistantBetweenToolAndUser inserts a filler assistant message
// between a user message containing tool_result blocks and the next user
// message. Some providers require this to maintain valid message ordering.
func InsertAssistantBetweenToolAndUser(msgs []agentsdk.Message) []agentsdk.Message {
	if len(msgs) < 2 {
		return msgs
	}
	var out []agentsdk.Message
	for i, m := range msgs {
		out = append(out, m)
		if i+1 < len(msgs) && hasToolResult(m) && msgs[i+1].Role == "user" {
			out = append(out, agentsdk.Message{
				Role:    "assistant",
				Content: []agentsdk.ContentBlock{{Type: "text", Text: "I'll continue."}},
			})
		}
	}
	return out
}

func hasToolResult(m agentsdk.Message) bool {
	for _, b := range m.Content {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}
