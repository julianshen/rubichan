package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// summarizationStrategy compacts a conversation by summarizing the oldest
// messages into a single summary message. Requires a Summarizer; skips if nil.
type summarizationStrategy struct {
	summarizer       Summarizer
	messageThreshold int // minimum message count to trigger summarization
	ctx              context.Context
}

// NewSummarizationStrategy creates a strategy with the given summarizer and
// default threshold of 20 messages.
func NewSummarizationStrategy(ctx context.Context, s Summarizer) *summarizationStrategy {
	return &summarizationStrategy{
		summarizer:       s,
		messageThreshold: 20,
		ctx:              ctx,
	}
}

func (s *summarizationStrategy) Name() string { return "summarization" }

func (s *summarizationStrategy) Compact(messages []provider.Message, _ int) ([]provider.Message, error) {
	// Skip if no summarizer is configured.
	if s.summarizer == nil {
		return messages, nil
	}

	// Skip if not enough messages to warrant summarization.
	if len(messages) < s.messageThreshold {
		return messages, nil
	}

	// Summarize the oldest 60% of messages.
	splitIdx := len(messages) * 60 / 100
	if splitIdx < 2 {
		splitIdx = 2
	}

	oldMessages := messages[:splitIdx]
	recentMessages := messages[splitIdx:]

	summary, err := s.summarizer.Summarize(s.ctx, oldMessages)
	if err != nil {
		return messages, fmt.Errorf("summarization failed: %w", err)
	}

	// Replace old messages with a single summary message.
	summaryMsg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "text", Text: fmt.Sprintf("[Summary of %d earlier messages]\n%s", len(oldMessages), summary)},
		},
	}

	result := make([]provider.Message, 0, 1+len(recentMessages))
	result = append(result, summaryMsg)
	result = append(result, recentMessages...)

	return result, nil
}
