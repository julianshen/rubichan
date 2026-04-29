package agent

import (
	"context"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

type headTailSnipStrategy struct{}

func NewHeadTailSnipStrategy() agentsdk.CompactionStrategy {
	return &headTailSnipStrategy{}
}

func (s *headTailSnipStrategy) Name() string { return "head_tail_snip" }

func (s *headTailSnipStrategy) Compact(_ context.Context, messages []agentsdk.Message, budget int) ([]agentsdk.Message, error) {
	for estimateMessageTokens(messages) > budget && len(messages) > 4 {
		prevLen := len(messages)
		cutStart := len(messages) / 3
		cutEnd := cutStart + 2

		if cutEnd >= len(messages) {
			break
		}
		if hasToolUse(messages[cutStart]) || hasToolResult(messages[cutStart]) {
			cutEnd++
		}
		if cutEnd >= len(messages) {
			break
		}

		messages = append(messages[:cutStart:cutStart], messages[cutEnd:]...)

		if len(messages) == prevLen {
			break
		}
	}
	return messages, nil
}
