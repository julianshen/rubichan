# Session Memory Compaction

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement smart conversation compaction that preserves API invariants (tool_use/tool_result pairs, thinking blocks) while summarizing older context. Replaces naive truncation with structural awareness.

**Architecture:** Port Claude Code's `sessionMemoryCompact.ts` and `compact.ts` patterns. A `SessionMemoryCompactor` calculates which messages to keep based on token minimums and API invariants, then summarizes the rest via a forked agent call.

**Tech Stack:** Go, existing `ContextManager` and `Conversation` types, provider streaming.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/agent/session_memory.go` | `SessionMemoryCompactor`, `calculateMessagesToKeepIndex`, `adjustIndexToPreserveAPIInvariants` |
| `internal/agent/session_memory_test.go` | Unit tests for index calculation and invariant preservation |
| `internal/agent/compaction.go` | Wire `SessionMemoryCompactor` into existing compaction pipeline |

---

## Chunk 1: Message Index Calculator

### Task 1: Define compaction parameters and index calculator

**Files:**
- Create: `internal/agent/session_memory.go`

**Code:**

```go
package agent

import (
	"github.com/julianshen/rubichan/internal/provider"
)

const (
	// Minimum tokens to preserve in compacted conversation
	minPreserveTokens = 10_000
	// Minimum text-block messages to preserve
	minTextBlockMessages = 5
	// Maximum tokens for summary generation
	maxSummaryTokens = 40_000
	// Post-compact token budget for restoration
	postCompactTokenBudget = 50_000
	// Maximum files to restore after compaction
	postCompactMaxFilesToRestore = 5
)

// SessionMemoryCompactor performs smart conversation compaction that
// preserves API invariants (tool_use/tool_result pairs, thinking blocks).
type SessionMemoryCompactor struct {
	// lastSummarizedMessageID tracks the boundary between preserved and
	// summarized messages across compaction rounds.
	lastSummarizedMessageID string
}

// calculateMessagesToKeepIndex returns the index into messages where
// compaction should split: messages [0:idx) are summarized, [idx:] are kept.
// It ensures:
//   - At least minPreserveTokens are kept
//   - At least minTextBlockMessages with text blocks are kept
//   - No tool_use/tool_result pairs are split
//   - No thinking blocks sharing message.ID are split
func (c *SessionMemoryCompactor) calculateMessagesToKeepIndex(
	messages []provider.Message,
	tokenCounter func([]provider.Message) int,
) int {
	if len(messages) == 0 {
		return 0
	}
	
	// Start from last summarized boundary if available
	startIdx := 0
	if c.lastSummarizedMessageID != "" {
		for i, m := range messages {
			if m.ID == c.lastSummarizedMessageID {
				startIdx = i + 1
				break
			}
		}
	}
	
	// Expand backwards to meet minimum tokens
	totalTokens := tokenCounter(messages[startIdx:])
	idx := startIdx
	for idx > 0 && totalTokens < minPreserveTokens {
		idx--
		totalTokens += tokenCounter(messages[idx:idx+1])
	}
	
	// Expand backwards to meet minimum text-block messages
	textBlockCount := 0
	for i := idx; i < len(messages); i++ {
		if hasTextBlock(messages[i]) {
			textBlockCount++
		}
	}
	for idx > 0 && textBlockCount < minTextBlockMessages {
		idx--
		if hasTextBlock(messages[idx]) {
			textBlockCount++
		}
	}
	
	return idx
}

func hasTextBlock(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "text" && c.Text != "" {
			return true
		}
	}
	return false
}
```

**Test:**

```go
func TestCalculateMessagesToKeepIndex(t *testing.T) {
	// 10 messages, each 1000 tokens
	messages := make([]provider.Message, 10)
	for i := range messages {
		messages[i] = provider.Message{ID: fmt.Sprintf("m%d", i), Role: "user"}
	}
	
	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int {
		return len(msgs) * 1000
	}
	
	idx := c.calculateMessagesToKeepIndex(messages, counter)
	// Should keep at least minPreserveTokens (10_000) = 10 messages
	// But we only have 10, so idx = 0
	require.Equal(t, 0, idx)
	
	// 20 messages, each 1000 tokens
	messages = make([]provider.Message, 20)
	for i := range messages {
		messages[i] = provider.Message{ID: fmt.Sprintf("m%d", i), Role: "user"}
	}
	idx = c.calculateMessagesToKeepIndex(messages, counter)
	// Should keep 10 messages (10_000 tokens), summarize first 10
	require.Equal(t, 10, idx)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestCalculateMessagesToKeepIndex -v
```

**Expected:** Test fails — compactor not yet wired.

---

### Task 2: API invariant preservation

**Files:**
- Modify: `internal/agent/session_memory.go`

**Code:**

```go
// adjustIndexToPreserveAPIInvariants ensures the compaction boundary
// does not split:
//   - tool_use/tool_result pairs (must be in same half)
//   - thinking blocks sharing a message.ID
//   - assistant messages with tool_use blocks from their user prompt
func adjustIndexToPreserveAPIInvariants(
	messages []provider.Message,
	idx int,
) int {
	if idx <= 0 || idx >= len(messages) {
		return idx
	}
	
	// Don't split in the middle of a tool_use/tool_result pair
	// If messages[idx] is a tool_result, move idx forward to include the tool_use
	if messages[idx].Role == "user" && isToolResultMessage(messages[idx]) {
		// Find the matching tool_use (should be the previous assistant message)
		for i := idx - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" && hasToolUseBlock(messages[i]) {
				idx = i // Include the assistant message with tool_use
				break
			}
		}
	}
	
	// Don't split thinking blocks that share message.ID with adjacent messages
	if idx > 0 && messages[idx-1].ID == messages[idx].ID {
		// Find the start of this thinking block
		for i := idx - 1; i >= 0; i-- {
			if messages[i].ID != messages[idx].ID {
				idx = i + 1
				break
			}
		}
	}
	
	return idx
}

func isToolResultMessage(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "tool_result" {
			return true
		}
	}
	return false
}

func hasToolUseBlock(m provider.Message) bool {
	for _, c := range m.Content {
		if c.Type == "tool_use" {
			return true
		}
	}
	return false
}
```

**Test:**

```go
func TestAdjustIndexPreservesToolPairs(t *testing.T) {
	messages := []provider.Message{
		{ID: "m0", Role: "user"},
		{ID: "m1", Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use"}}},
		{ID: "m2", Role: "user", Content: []provider.ContentBlock{{Type: "tool_result"}}},
		{ID: "m3", Role: "assistant"},
	}
	
	// Trying to split at idx=2 (between tool_use and tool_result)
	idx := adjustIndexToPreserveAPIInvariants(messages, 2)
	// Should move to idx=1 to include the assistant with tool_use
	require.Equal(t, 1, idx)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAdjustIndexPreservesToolPairs -v
```

**Expected:** PASS.

---

## Chunk 2: Compaction Pipeline

### Task 3: Implement compactConversation with summary generation

**Files:**
- Modify: `internal/agent/session_memory.go`

**Code:**

```go
// Compact performs session memory compaction on the conversation.
// It:
//   1. Calculates which messages to keep
//   2. Preserves API invariants at the boundary
//   3. Summarizes discarded messages via a compact agent call
//   4. Replaces discarded messages with summary + boundary marker
func (c *SessionMemoryCompactor) Compact(
	ctx context.Context,
	conv *conversation.Conversation,
	summarizer func(ctx context.Context, messages []provider.Message) (string, error),
) error {
	messages := conv.Messages()
	idx := c.calculateMessagesToKeepIndex(messages, estimateTokens)
	idx = adjustIndexToPreserveAPIInvariants(messages, idx)
	
	if idx <= 0 {
		// Nothing to compact
		return nil
	}
	
	// Summarize messages to be discarded
	summary, err := summarizer(ctx, messages[:idx])
	if err != nil {
		return fmt.Errorf("summarize messages: %w", err)
	}
	
	// Build compacted conversation: summary marker + kept messages
	summaryMsg := provider.Message{
		Role: "system",
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("[Earlier conversation summarized]: %s", summary),
		}},
	}
	
	newMessages := append([]provider.Message{summaryMsg}, messages[idx:]...)
	conv.ReplaceMessages(newMessages)
	
	c.lastSummarizedMessageID = messages[idx].ID
	return nil
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestCompact -v
```

**Expected:** Test passes with mock summarizer.

---

### Task 4: Wire into ContextManager.Compact

**Files:**
- Modify: `internal/agent/context.go`

**Code:**

```go
func (cm *ContextManager) Compact(ctx context.Context, conv *conversation.Conversation) error {
	// Try session memory compaction first
	if cm.sessionCompactor != nil {
		if err := cm.sessionCompactor.Compact(ctx, conv, cm.summarizeMessages); err == nil {
			return nil
		}
	}
	
	// Fall back to existing compaction strategies
	// ... existing logic ...
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestContextManagerCompact -v
```

**Expected:** Existing tests pass + new compaction tests pass.

---

## Validation Commands

```bash
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Session memory compaction with API invariant preservation`

**Body:**
- Smart conversation compaction that preserves tool_use/tool_result pairs and thinking blocks
- Calculates optimal split point based on token minimums and text-block message counts
- Summarizes discarded messages via compact agent call
- Falls back to existing compaction if session memory compaction fails
- Ports Claude Code's `sessionMemoryCompact.ts:232-314` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
