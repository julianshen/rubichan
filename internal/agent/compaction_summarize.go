package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// errSummaryInflated is returned when the summarized result is not smaller
// than the original messages, indicating the summary was too verbose.
var errSummaryInflated = errors.New("summarized result is not smaller than original")

// summarizationStrategy compacts a conversation by summarizing the oldest
// messages into a single summary message. Requires a Summarizer; skips if nil.
type summarizationStrategy struct {
	summarizer       Summarizer
	messageThreshold int // minimum message count to trigger summarization
	lastFailedLen    int // message count at last failure, for skip-on-retry
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

func (s *summarizationStrategy) Compact(ctx context.Context, messages []provider.Message, _ int) ([]provider.Message, error) {
	// Skip if no summarizer is configured.
	if s.summarizer == nil {
		return messages, nil
	}

	// Skip if not enough messages to warrant summarization.
	if len(messages) < s.messageThreshold {
		return messages, nil
	}

	// Enhancement 4: Skip if previous attempt failed on the same message count.
	if s.lastFailedLen > 0 && len(messages) == s.lastFailedLen {
		return messages, nil
	}

	// Summarize the oldest 60% of messages.
	splitIdx := len(messages) * 60 / 100
	if splitIdx < 2 {
		splitIdx = 2
	}

	// Enhancement 1: Adjust split point to avoid orphaning tool_result blocks.
	splitIdx = findSafeSplitPoint(messages, splitIdx)

	oldMessages := messages[:splitIdx]
	recentMessages := messages[splitIdx:]

	summary, err := s.summarizer.Summarize(ctx, oldMessages)
	if err != nil {
		s.lastFailedLen = len(messages)
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

	// Enhancement 3: Reject if summary is not actually smaller.
	if estimateMessageTokens(result) >= estimateMessageTokens(messages) {
		s.lastFailedLen = len(messages)
		return messages, errSummaryInflated
	}

	// Enhancement 4: Reset failure tracking on success.
	s.lastFailedLen = 0

	return result, nil
}

// findSafeSplitPoint adjusts a target split index so that the split does not
// break a tool_use/tool_result pair. If messages[target] is a tool_result, it
// scans backward past it. If messages[target] is a tool_use, it also scans
// backward so the tool_use and its following tool_result stay together on the
// recent side. Returns at least 2 to ensure minimum summarizable messages.
func findSafeSplitPoint(messages []provider.Message, target int) int {
	if target >= len(messages) {
		target = len(messages) - 1
	}

	// Scan backward past tool_result or tool_use at the split boundary
	// to keep tool pairs together on the same side.
	for target > 2 && (hasToolResult(messages[target]) || hasToolUse(messages[target])) {
		target--
	}

	if target < 2 {
		return 2
	}
	return target
}
