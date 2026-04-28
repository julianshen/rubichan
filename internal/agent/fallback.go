package agent

import "github.com/julianshen/rubichan/pkg/agentsdk"

func stripThinkingBlocks(messages []agentsdk.Message) []agentsdk.Message {
	result := make([]agentsdk.Message, 0, len(messages))
	for _, msg := range messages {
		var filtered []agentsdk.ContentBlock
		for _, block := range msg.Content {
			if block.Type != "thinking" && block.Type != "redacted_thinking" {
				filtered = append(filtered, block)
			}
		}
		if len(filtered) == 0 && len(msg.Content) > 0 {
			continue
		}
		stripped := msg
		stripped.Content = filtered
		result = append(result, stripped)
	}
	return result
}
