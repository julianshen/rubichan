package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

type headTailSnipStrategy struct{}

func NewHeadTailSnipStrategy() agentsdk.CompactionStrategy {
	return &headTailSnipStrategy{}
}

func (s *headTailSnipStrategy) Name() string { return "head_tail_snip" }

func (s *headTailSnipStrategy) Compact(_ context.Context, messages []agentsdk.Message, budget int) ([]agentsdk.Message, error) {
	result := s.Snip(messages, budget)
	return result.Messages, nil
}

// Snip performs head-tail compaction and returns detailed result.
func (s *headTailSnipStrategy) Snip(messages []agentsdk.Message, budget int) agentsdk.SnipResult {
	if len(messages) <= 4 {
		return agentsdk.SnipResult{Messages: messages}
	}

	beforeTokens := estimateTokens(messages)
	if beforeTokens <= budget {
		return agentsdk.SnipResult{Messages: messages}
	}

	// Determine how many messages to keep: preserve head (1/3) + tail (2/3)
	cutStart := len(messages) / 3
	if cutStart < 1 {
		cutStart = 1
	}
	cutEnd := cutStart + 2
	if cutEnd >= len(messages) {
		return agentsdk.SnipResult{Messages: messages}
	}

	// Don't split tool_use/tool_result pairs
	if cutEnd < len(messages) && (hasToolUseMsg(messages[cutStart]) || hasToolResultMsg(messages[cutStart])) {
		cutEnd++
	}
	if cutEnd >= len(messages) {
		return agentsdk.SnipResult{Messages: messages}
	}

	head := messages[:cutStart]
	tail := messages[cutEnd:]

	// Collect snipped UUIDs
	var snippedUUIDs []string
	for i := cutStart; i < cutEnd && i < len(messages); i++ {
		if id, ok := messages[i].Metadata["uuid"].(string); ok && id != "" {
			snippedUUIDs = append(snippedUUIDs, id)
		}
	}

	// Calculate tokens freed
	afterTokens := estimateTokens(append(head, tail...))
	tokensFreed := beforeTokens - afterTokens
	if tokensFreed < 0 {
		tokensFreed = 0
	}

	// Build boundary marker message
	boundaryText := fmt.Sprintf("[Context snipped: %d older messages removed to save context space]", len(snippedUUIDs))
	boundaryMsg := agentsdk.Message{
		Role: "system",
		Content: []agentsdk.ContentBlock{{
			Type: "text",
			Text: boundaryText,
		}},
	}

	result := make([]agentsdk.Message, 0, len(head)+1+len(tail))
	result = append(result, head...)
	result = append(result, boundaryMsg)
	result = append(result, tail...)

	return agentsdk.SnipResult{
		Messages:     result,
		TokensFreed:  tokensFreed,
		BoundaryMsg:  &boundaryMsg,
		SnippedUUIDs: snippedUUIDs,
	}
}

// InjectMessageIDTags adds [id:xxxx] tags to user message text blocks for cross-referencing.
func InjectMessageIDTags(messages []agentsdk.Message) []agentsdk.Message {
	result := make([]agentsdk.Message, len(messages))
	for i, msg := range messages {
		result[i] = msg
		if msg.Role != "user" {
			continue
		}
		uuid := ""
		if msg.Metadata != nil {
			if id, ok := msg.Metadata["uuid"].(string); ok {
				uuid = id
			}
		}
		if uuid == "" {
			continue
		}
		tag := fmt.Sprintf("[id:%s] ", deriveShortMessageID(uuid))
		var newContent []agentsdk.ContentBlock
		for _, block := range msg.Content {
			if block.Type == "text" && !strings.HasPrefix(block.Text, "[id:") {
				newContent = append(newContent, agentsdk.ContentBlock{
					Type: block.Type,
					Text: tag + block.Text,
				})
			} else {
				newContent = append(newContent, block)
			}
		}
		result[i].Content = newContent
	}
	return result
}

func deriveShortMessageID(uuid string) string {
	if len(uuid) < 8 {
		return uuid
	}
	return uuid[:8]
}

func hasToolUseMsg(msg agentsdk.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

func hasToolResultMsg(msg agentsdk.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
}

func estimateTokens(msgs []agentsdk.Message) int {
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
