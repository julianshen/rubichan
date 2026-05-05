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

func (s *headTailSnipStrategy) Snip(messages []agentsdk.Message, budget int) agentsdk.SnipResult {
	if len(messages) <= 4 {
		return agentsdk.SnipResult{Messages: messages}
	}

	beforeTokens := estimateTokens(messages)
	if beforeTokens <= budget {
		return agentsdk.SnipResult{Messages: messages}
	}

	cutStart := len(messages) / 3
	if cutStart < 1 {
		cutStart = 1
	}
	cutEnd := cutStart + 2
	if cutEnd >= len(messages) {
		return agentsdk.SnipResult{Messages: messages}
	}

	// Don't split tool_use/tool_result pairs at either boundary.
	// If a message at the cut boundary contains tool content, shift the boundary
	// outward to include the full pair.
	for cutStart > 0 && hasBlockType(messages[cutStart], agentsdk.BlockTypeToolUse, agentsdk.BlockTypeToolResult) {
		cutStart--
		cutEnd--
	}
	for cutEnd < len(messages) && hasBlockType(messages[cutEnd], agentsdk.BlockTypeToolUse, agentsdk.BlockTypeToolResult) {
		cutEnd++
	}
	if cutEnd >= len(messages) || cutStart < 0 {
		return agentsdk.SnipResult{Messages: messages}
	}

	head := messages[:cutStart]
	tail := messages[cutEnd:]

	var snippedUUIDs []string
	for i := cutStart; i < cutEnd && i < len(messages); i++ {
		if id, ok := messages[i].Metadata["uuid"].(string); ok && id != "" {
			snippedUUIDs = append(snippedUUIDs, id)
		}
	}

	afterTokens := estimateTokens(head) + estimateTokens(tail)
	tokensFreed := beforeTokens - afterTokens
	if tokensFreed < 0 {
		tokensFreed = 0
	}

	boundaryText := fmt.Sprintf("[Context snipped: %d older messages removed to save context space]", len(snippedUUIDs))
	boundaryMsg := agentsdk.Message{
		Role: "system",
		Content: []agentsdk.ContentBlock{{
			Type: agentsdk.BlockTypeText,
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

func InjectMessageIDTags(messages []agentsdk.Message) []agentsdk.Message {
	result := make([]agentsdk.Message, len(messages))
	copy(result, messages)
	for i, msg := range result {
		if msg.Role != "user" {
			continue
		}
		uuid := ""
		if id, ok := msg.Metadata["uuid"].(string); ok {
			uuid = id
		}
		if uuid == "" {
			continue
		}
		tag := fmt.Sprintf("[id:%s] ", deriveShortMessageID(uuid))
		newContent := make([]agentsdk.ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == agentsdk.BlockTypeText && !strings.HasPrefix(block.Text, "[id:") {
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

func hasBlockType(msg agentsdk.Message, types ...string) bool {
	for _, block := range msg.Content {
		for _, t := range types {
			if block.Type == t {
				return true
			}
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
