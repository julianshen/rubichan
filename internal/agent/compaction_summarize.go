package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// summarizationStrategy compacts a conversation by summarizing the oldest
// messages into a single summary message. Requires a Summarizer; skips if nil.
// When signals are injected, the split ratio adjusts dynamically.
type summarizationStrategy struct {
	summarizer       Summarizer
	messageThreshold int                  // minimum message count to trigger summarization
	signals          *ConversationSignals // optional, injected via SetSignals
}

// NewSummarizationStrategy creates a strategy with the given summarizer and
// default threshold of 20 messages.
func NewSummarizationStrategy(s Summarizer) *summarizationStrategy {
	return &summarizationStrategy{
		summarizer:       s,
		messageThreshold: 20,
	}
}

func (s *summarizationStrategy) Name() string { return "summarization" }

// SetSignals injects conversation signals for dynamic split adjustment.
func (s *summarizationStrategy) SetSignals(signals ConversationSignals) {
	s.signals = &signals
}

func (s *summarizationStrategy) Compact(ctx context.Context, messages []provider.Message, _ int) ([]provider.Message, error) {
	// Skip if no summarizer is configured.
	if s.summarizer == nil {
		return messages, nil
	}

	// Skip if not enough messages to warrant summarization.
	if len(messages) < s.messageThreshold {
		return messages, nil
	}

	// Default: summarize oldest 60%.
	splitPct := 60
	if s.signals != nil {
		// High error density → preserve more recent (shrink summarized portion).
		if s.signals.ErrorDensity > 0.3 {
			splitPct = 45
		} else if s.signals.MessageCount > s.messageThreshold*2 {
			// Very long conversation → compress more aggressively.
			splitPct = 70
		}
	}

	splitIdx := len(messages) * splitPct / 100
	if splitIdx < 2 {
		splitIdx = 2
	}

	oldMessages := messages[:splitIdx]
	recentMessages := messages[splitIdx:]

	summary, err := s.summarizer.Summarize(ctx, oldMessages)
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
