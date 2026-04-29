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
		headSize := len(messages) / 3
		if headSize < 1 {
			headSize = 1
		}
		tailStart := headSize + 2
		if tailStart >= len(messages) {
			break
		}
		if hasToolUse(messages[headSize]) {
			tailStart++
			if tailStart >= len(messages) {
				break
			}
		}
		head := messages[:headSize]
		tail := messages[tailStart:]
		messages = make([]agentsdk.Message, 0, len(head)+len(tail))
		messages = append(messages, head...)
		messages = append(messages, tail...)
	}
	return messages, nil
}
