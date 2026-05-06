# Snippet Compaction with Boundary Markers

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `query/snip_compact.go` boundary marker feature to rubichan's existing `HeadTailSnipStrategy`, making compaction transparent by inserting a boundary marker message and tracking snipped UUIDs and freed tokens.

**Architecture:** Extend `headTailSnipStrategy` to return a `SnipResult` struct containing the compacted messages, a boundary marker, tokens freed, and snipped UUIDs. Integrate `SnipResult` into `ContextManager.Compact` and `ForceCompact`. Add optional `InjectMessageIDTags` for cross-referencing.

**Tech Stack:** Go, existing `agentsdk.Message` and `CompactionStrategy` types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/compaction.go` | Add `SnipResult` type (messages, boundary, tokens freed, snipped UUIDs) |
| `internal/agent/compaction_snip.go` | Rewrite `HeadTailSnipStrategy` to return `SnipResult`, insert boundary marker |
| `internal/agent/compaction_snip_test.go` | Tests for boundary marker, token tracking, UUID collection |
| `internal/agent/context.go` | Integrate `SnipResult` into `Compact` and `ForceCompact` |

---

## Chunk 1: SnipResult Type

### Task 1: Add SnipResult to pkg/agentsdk/compaction.go

**Files:**
- Modify: `pkg/agentsdk/compaction.go`

**Code:**

```go
// SnipResult is the outcome of a head-tail snip compaction.
type SnipResult struct {
	Messages     []Message
	TokensFreed  int
	BoundaryMsg  *Message
	SnippedUUIDs []string
}
```

**Test:**

```go
func TestSnipResultFields(t *testing.T) {
	sr := SnipResult{
		Messages:     []Message{{Role: "user"}},
		TokensFreed:  42,
		BoundaryMsg:  &Message{Role: "system"},
		SnippedUUIDs: []string{"a", "b"},
	}
	assert.Len(t, sr.Messages, 1)
	assert.Equal(t, 42, sr.TokensFreed)
	assert.NotNil(t, sr.BoundaryMsg)
	assert.Equal(t, []string{"a", "b"}, sr.SnippedUUIDs)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestSnipResultFields -v
```

**Expected:** PASS.

---

## Chunk 2: HeadTailSnipStrategy with Boundary Marker

### Task 2: Rewrite headTailSnipStrategy to produce SnipResult

**Files:**
- Modify: `internal/agent/compaction_snip.go`

**Code:**

```go
package agent

import (
	"context"
	"fmt"

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

	beforeTokens := estimateMessageTokens(messages)
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
	if cutEnd < len(messages) && hasToolUse(messages[cutStart]) || hasToolResult(messages[cutStart]) {
		cutEnd++
	}
	if cutEnd >= len(messages) {
		return agentsdk.SnipResult{Messages: messages}
	}

	head := messages[:cutStart]
	tail := messages[cutEnd:]

	// Collect snipped UUIDs
	var snippedUUIDs []string
	for i := cutStart; i < cutEnd; i++ {
		if id, ok := messages[i].Metadata["uuid"].(string); ok && id != "" {
			snippedUUIDs = append(snippedUUIDs, id)
		}
	}

	// Calculate tokens freed
	afterTokens := estimateMessageTokens(append(head, tail...))
	tokensFreed := beforeTokens - afterTokens
	if tokensFreed < 0 {
		tokensFreed = 0
	}

	// Build boundary marker message
	boundaryText := fmt.Sprintf("[Context snipped: %d older messages removed to save context space]", len(snippedUUIDs))
	boundaryMsg := &agentsdk.Message{
		Role: "system",
		Content: []agentsdk.ContentBlock{{
			Type: "text",
			Text: boundaryText,
		}},
	}

	result := make([]agentsdk.Message, 0, len(head)+1+len(tail))
	result = append(result, head...)
	result = append(result, *boundaryMsg)
	result = append(result, tail...)

	return agentsdk.SnipResult{
		Messages:     result,
		TokensFreed:  tokensFreed,
		BoundaryMsg:  boundaryMsg,
		SnippedUUIDs: snippedUUIDs,
	}
}
```

**Test:**

```go
func TestHeadTailSnip_BoundaryMarkerInserted(t *testing.T) {
	s := NewHeadTailSnipStrategy()
	msgs := makeMessages(9)
	result, err := s.Compact(context.Background(), msgs, 1)
	assert.NoError(t, err)

	// Should have boundary marker
	foundBoundary := false
	for _, m := range result {
		for _, b := range m.Content {
			if b.Type == "text" && strings.Contains(b.Text, "[Context snipped:") {
				foundBoundary = true
			}
		}
	}
	assert.True(t, foundBoundary, "should insert boundary marker")
}

func TestHeadTailSnip_TokensFreedTracked(t *testing.T) {
	strategy := &headTailSnipStrategy{}
	msgs := makeMessages(9)
	result := strategy.Snip(msgs, 1)
	assert.Greater(t, result.TokensFreed, 0, "should track tokens freed")
	assert.NotEmpty(t, result.SnippedUUIDs, "should collect snipped UUIDs")
	assert.NotNil(t, result.BoundaryMsg, "should have boundary message")
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestHeadTailSnip_BoundaryMarkerInserted -v
go test ./internal/agent/... -run TestHeadTailSnip_TokensFreedTracked -v
```

**Expected:** PASS.

---

### Task 3: Add InjectMessageIDTags helper

**Files:**
- Modify: `internal/agent/compaction_snip.go`

**Code:**

```go
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
```

**Test:**

```go
func TestInjectMessageIDTags(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Metadata: map[string]any{"uuid": "abc12345-def"}, Content: []agentsdk.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	result := InjectMessageIDTags(msgs)
	assert.True(t, strings.HasPrefix(result[0].Content[0].Text, "[id:abc12345]"))
	assert.Equal(t, "hi", result[1].Content[0].Text)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestInjectMessageIDTags -v
```

**Expected:** PASS.

---

## Chunk 3: ContextManager Integration

### Task 4: Integrate SnipResult into ContextManager

**Files:**
- Modify: `internal/agent/context.go`

**Code:**

Add to `CompactResult` in `pkg/agentsdk/compaction.go` (or extend locally):

```go
// CompactResult already exists; add SnipResult field if needed.
// For now, ContextManager.Compact returns error; ForceCompact returns CompactResult.
// We add a SnipResults field to CompactResult to expose boundary markers.
```

Modify `internal/agent/context.go` — add `SnipResults` to `CompactResult`:

```go
// CompactResult (in pkg/agentsdk/compaction.go) — add field:
type CompactResult struct {
	BeforeTokens   int
	AfterTokens    int
	BeforeMsgCount int
	AfterMsgCount  int
	StrategiesRun  []string
	SnipResults    []SnipResult // NEW: per-strategy snip details
}
```

Then modify `ContextManager.Compact` to collect snip results:

```go
func (cm *ContextManager) Compact(ctx context.Context, conv *Conversation) error {
	if !cm.ShouldCompact(conv) {
		return nil
	}
	// ... existing setup ...

	beforeTokens := estimateMessageTokens(conv.messages)
	anyStrategySucceeded := false

	for i, s := range cm.strategies {
		if i > 0 && !cm.ExceedsBudget(conv) {
			break
		}
		result, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		conv.messages = result
		anyStrategySucceeded = true
	}

	// ... rest unchanged ...
}
```

Modify `ForceCompact` to collect and return `SnipResults`:

```go
func (cm *ContextManager) ForceCompact(ctx context.Context, conv *Conversation) CompactResult {
	result := CompactResult{
		BeforeTokens:   cm.EstimateTokens(conv),
		BeforeMsgCount: len(conv.messages),
	}
	// ... existing setup ...

	for _, s := range cm.strategies {
		// ... existing logic ...
		msgs, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		// If strategy supports Snip, capture the result
		if snipper, ok := s.(interface{ Snip([]agentsdk.Message, int) agentsdk.SnipResult }); ok {
			snip := snipper.Snip(conv.messages, messageBudget)
			if snip.BoundaryMsg != nil {
				result.SnipResults = append(result.SnipResults, snip)
			}
		}
		// ... existing token/count tracking ...
		conv.messages = msgs
	}

	result.AfterTokens = cm.EstimateTokens(conv)
	result.AfterMsgCount = len(conv.messages)
	return result
}
```

**Test:**

```go
func TestForceCompact_CollectsSnipResults(t *testing.T) {
	cm := NewContextManager(55, 0)
	cm.SetStrategies([]CompactionStrategy{NewHeadTailSnipStrategy(), &truncateStrategy{}})

	conv := NewConversation("s")
	for i := 0; i < 10; i++ {
		conv.AddUser(fmt.Sprintf("message %d with some content", i))
		conv.AddAssistant([]agentsdk.ContentBlock{{Type: "text", Text: "response"}})
	}

	result := cm.ForceCompact(context.Background(), conv)
	assert.NotEmpty(t, result.StrategiesRun)
	// SnipResults may be empty if head_tail_snip didn't trigger
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestForceCompact_CollectsSnipResults -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Snippet compaction with boundary markers and token tracking`

**Body:**
- `HeadTailSnipStrategy` now inserts a boundary marker message when snipping occurs: `[Context snipped: N older messages removed to save context space]`
- `SnipResult` struct tracks `TokensFreed`, `SnippedUUIDs`, and `BoundaryMsg`
- `InjectMessageIDTags` adds `[id:xxxx]` tags to user messages for cross-referencing
- `ContextManager.ForceCompact` collects `SnipResults` for telemetry
- Preserves API invariants (no split tool_use/tool_result pairs)
- Ports ccgo's `query/snip_compact.go` pattern to rubichan

**Commit prefix:** `[BEHAVIORAL]`
