package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

const (
	// minPreserveTokens is the minimum token count to keep in the
	// conversation after compaction. Older messages beyond this are
	// candidates for summarization.
	minPreserveTokens = 10_000
	// minTextBlockMessages is the minimum number of messages containing
	// text blocks to preserve. Ensures the model retains enough context
	// for coherent continuation.
	minTextBlockMessages = 5
)

// SessionMemoryCompactor performs smart conversation compaction that
// preserves API invariants (tool_use/tool_result pairs, thinking blocks).
// It replaces the naive summarization strategy with structural awareness
// of message relationships.
type SessionMemoryCompactor struct {
	// lastSummarizedCount tracks how many messages were summarized in the
	// last compaction round. On subsequent compactions, this count is used
	// to skip the summary message (at index 0) and resume from the first
	// original message. This avoids double-summarization and index-shift
	// bugs that would occur with raw index tracking.
	lastSummarizedCount int
}

// calculateMessagesToKeepIndex returns the index into messages where
// compaction should split: messages [0:idx) are summarized, [idx:] are kept.
// It ensures:
//   - At least minPreserveTokens are kept
//   - At least minTextBlockMessages with text blocks are kept
//
// The algorithm starts from the last summarized boundary (if any) and
// expands forward until both minimums are satisfied.
func (c *SessionMemoryCompactor) calculateMessagesToKeepIndex(
	messages []provider.Message,
	tokenCounter func([]provider.Message) int,
) int {
	if len(messages) == 0 {
		return 0
	}

	// Start from last summarized boundary if available.
	// Skip the summary message (index 0) and any previously-summarized
	// messages. lastSummarizedCount is the number of original messages
	// summarized in the last round, so startIdx = 1 + lastSummarizedCount
	// skips the summary message and resumes from the first kept message.
	startIdx := 0
	if c.lastSummarizedCount > 0 {
		startIdx = 1 + c.lastSummarizedCount
	}

	// Calculate total tokens from startIdx onward.
	totalTokens := tokenCounter(messages[startIdx:])

	// If total tokens from startIdx are already under the minimum,
	// we need to keep all messages from startIdx onward.
	// Only summarize if we have enough tokens to make it worthwhile.
	if totalTokens < minPreserveTokens {
		return startIdx
	}

	// We have enough tokens. Find the split point where kept messages
	// have at most minPreserveTokens. Work forward from startIdx,
	// using a sliding token count to avoid O(n²) repeated scans.
	idx := startIdx
	keptTokens := totalTokens
	for idx < len(messages) {
		removed := tokenCounter(messages[idx : idx+1])
		keptTokens -= removed
		idx++
		if keptTokens <= minPreserveTokens {
			break
		}
	}

	// Ensure at least minTextBlockMessages are kept.
	textBlockCount := 0
	for i := idx; i < len(messages); i++ {
		if hasTextBlock(messages[i]) {
			textBlockCount++
		}
	}
	for idx < len(messages) && textBlockCount < minTextBlockMessages {
		if hasTextBlock(messages[idx]) {
			textBlockCount++
		}
		idx++
	}

	return idx
}

// hasTextBlock reports whether a message contains a non-empty text block.
func hasTextBlock(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "text" && c.Text != "" {
			return true
		}
	}
	return false
}

// adjustIndexToPreserveAPIInvariants ensures the compaction boundary
// does not split tool_use/tool_result pairs or thinking blocks sharing
// a content block ID. It scans backward from the proposed split point
// to find a safe boundary.
func adjustIndexToPreserveAPIInvariants(
	messages []provider.Message,
	idx int,
) int {
	if idx <= 0 || idx >= len(messages) {
		return idx
	}

	// Don't split in the middle of a tool_use/tool_result pair.
	// If messages[idx] is a tool_result, move idx backward to include
	// the matching tool_use (the previous assistant message).
	if messages[idx].Role == "user" && hasToolResult(messages[idx]) {
		for i := idx - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" && hasToolUse(messages[i]) {
				idx = i // Include the assistant message with tool_use.
				break
			}
		}
	}

	// Don't split thinking blocks that share a content block ID with adjacent
	// messages. Thinking blocks from the same model response may share IDs
	// across content blocks. We detect this by checking if adjacent messages
	// at the boundary have thinking blocks with matching IDs.
	if idx > 0 {
		boundaryIDs := make(map[string]bool)
		for _, c := range messages[idx].Content {
			if c.Type == "thinking" && c.ID != "" {
				boundaryIDs[c.ID] = true
			}
		}
		if len(boundaryIDs) > 0 {
			for i := idx - 1; i >= 0; i-- {
				hasMatch := false
				for _, c := range messages[i].Content {
					if c.Type == "thinking" && c.ID != "" && boundaryIDs[c.ID] {
						hasMatch = true
						break
					}
				}
				if !hasMatch {
					idx = i + 1
					break
				}
				// All messages scanned and all match — move to start.
				if i == 0 {
					idx = 0
					break
				}
			}
		}
	}

	return idx
}

// Compact performs session memory compaction on the conversation.
// Returns nil on success or when no compaction is needed.
func (c *SessionMemoryCompactor) Compact(
	ctx context.Context,
	conv *Conversation,
	summarizer func(ctx context.Context, messages []provider.Message) (string, error),
) error {
	messages := conv.Messages()
	result, err := c.compactMessages(ctx, messages, summarizer)
	if err != nil {
		return err
	}
	// Only update conversation if compaction actually occurred.
	// compactMessages returns the original slice when idx <= 0 (no compaction).
	// When compaction occurs, it returns a new slice with a summary prepended.
	// We detect compaction by checking if the first message is a summary.
	if len(result) > 0 && len(messages) > 0 && result[0].Content[0].Text != messages[0].Content[0].Text {
		conv.LoadFromMessages(result)
	}
	return nil
}

// sessionMemoryCompactionStrategy adapts SessionMemoryCompactor to the
// CompactionStrategy interface. It provides smarter compaction than the
// legacy summarizationStrategy by preserving API invariants.
type sessionMemoryCompactionStrategy struct {
	compactor  *SessionMemoryCompactor
	summarizer Summarizer
}

// NewSessionMemoryCompactionStrategy creates a CompactionStrategy that uses
// SessionMemoryCompactor for smart conversation compaction.
func NewSessionMemoryCompactionStrategy(s Summarizer) CompactionStrategy {
	return &sessionMemoryCompactionStrategy{
		compactor:  &SessionMemoryCompactor{},
		summarizer: s,
	}
}

func (s *sessionMemoryCompactionStrategy) Name() string { return "session_memory" }

func (s *sessionMemoryCompactionStrategy) Compact(ctx context.Context, messages []provider.Message, _ int) ([]provider.Message, error) {
	if s.summarizer == nil {
		return messages, nil
	}

	summarizerFn := func(ctx context.Context, msgs []provider.Message) (string, error) {
		return s.summarizer.Summarize(ctx, msgs)
	}

	return s.compactor.compactMessages(ctx, messages, summarizerFn)
}

// compactMessages runs compaction on a message slice without allocating
// a Conversation wrapper. Returns the compacted slice or an error.
func (c *SessionMemoryCompactor) compactMessages(
	ctx context.Context,
	messages []provider.Message,
	summarizer func(ctx context.Context, messages []provider.Message) (string, error),
) ([]provider.Message, error) {
	if err := ctx.Err(); err != nil {
		return messages, err
	}

	idx := c.calculateMessagesToKeepIndex(messages, estimateMessageTokens)
	idx = adjustIndexToPreserveAPIInvariants(messages, idx)

	if idx <= 0 {
		return messages, nil
	}

	// Recover from panics in the summarizer so a single bad summarizer
	// does not crash the entire agent. This matches the pattern used in
	// executeSingleTool.
	var summary string
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("summarizer panicked: %v", r)
			}
		}()
		summary, err = summarizer(ctx, messages[:idx])
	}()
	if err != nil {
		return messages, fmt.Errorf("summarize messages: %w", err)
	}

	summaryMsg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("[Summary of %d earlier messages]\n%s", idx, summary),
		}},
	}

	result := make([]provider.Message, 0, 1+len(messages)-idx)
	result = append(result, summaryMsg)
	result = append(result, messages[idx:]...)

	c.lastSummarizedCount = idx
	return result, nil
}
