# Streaming Tombstone Pattern

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When switching to a fallback model mid-conversation, "tombstone" orphaned messages so they're not sent to the new model. Prevents context pollution from partial responses.

**Architecture:** Port Claude Code's tombstone pattern from `query.ts` fallback path. A `TombstoneMessage` replaces the content of orphaned messages with a marker. The provider layer skips tombstoned messages when building the request.

**Tech Stack:** Go, existing `Conversation` and `provider.Message` types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/tombstone.go` | `TombstoneMessage`, `IsTombstoned` |
| `internal/conversation/tombstone.go` | `Tombstone`, `TombstoneRange` |
| `internal/provider/message.go` | Skip tombstoned messages in request building |

---

## Chunk 1: Tombstone Types

### Task 1: Define TombstoneMessage

**Files:**
- Create: `pkg/agentsdk/tombstone.go`

**Code:**

```go
package agentsdk

// TombstoneMarker is the text content of a tombstoned message.
const TombstoneMarker = "[This message was tombstoned due to model fallback]"

// IsTombstoned checks if a message content is a tombstone marker.
func IsTombstoned(content string) bool {
	return content == TombstoneMarker
}

// TombstoneReason explains why a message was tombstoned.
type TombstoneReason string

const (
	// TombstoneReasonModelFallback means the message was orphaned
	// when switching to a fallback model.
	TombstoneReasonModelFallback TombstoneReason = "model_fallback"
	// TombstoneReasonStreamError means the message was partial due
	// to a stream error.
	TombstoneReasonStreamError TombstoneReason = "stream_error"
	// TombstoneReasonUserAbort means the user aborted mid-stream.
	TombstoneReasonUserAbort TombstoneReason = "user_abort"
)
```

**Test:**

```go
func TestIsTombstoned(t *testing.T) {
	require.True(t, IsTombstoned(TombstoneMarker))
	require.False(t, IsTombstoned("normal message"))
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestIsTombstoned -v
```

**Expected:** PASS.

---

## Chunk 2: Conversation Tombstoning

### Task 2: Implement Tombstone on Conversation

**Files:**
- Create: `internal/conversation/tombstone.go`

**Code:**

```go
package conversation

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Tombstone replaces messages in the range [startIdx:endIdx] with
// tombstone markers. The messages are preserved in the conversation
// for history but skipped when building API requests.
func (c *Conversation) Tombstone(startIdx, endIdx int, reason agentsdk.TombstoneReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(c.messages) {
		endIdx = len(c.messages)
	}
	if startIdx >= endIdx {
		return
	}
	
	for i := startIdx; i < endIdx; i++ {
		c.messages[i].Content = []ContentBlock{{
			Type: "text",
			Text: agentsdk.TombstoneMarker,
		}}
		c.messages[i].Metadata = map[string]any{
			"tombstoned": true,
			"reason":     reason,
		}
	}
}

// TombstoneSinceLastAssistant tombstones all messages since the last
// complete assistant response. Used when model fallback occurs mid-stream.
func (c *Conversation) TombstoneSinceLastAssistant(reason agentsdk.TombstoneReason) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Find the last complete assistant message
	lastAssistantIdx := -1
	for i := len(c.messages) - 1; i >= 0; i-- {
		if c.messages[i].Role == "assistant" && !c.isTombstoned(i) {
			lastAssistantIdx = i
			break
		}
	}
	
	if lastAssistantIdx < 0 {
		// No complete assistant message — tombstone everything
		c.Tombstone(0, len(c.messages), reason)
		return len(c.messages)
	}
	
	// Tombstone messages after the last assistant
	startIdx := lastAssistantIdx + 1
	c.Tombstone(startIdx, len(c.messages), reason)
	return len(c.messages) - startIdx
}

func (c *Conversation) isTombstoned(idx int) bool {
	if idx < 0 || idx >= len(c.messages) {
		return false
	}
	if len(c.messages[idx].Content) == 0 {
		return false
	}
	return agentsdk.IsTombstoned(c.messages[idx].Content[0].Text)
}
```

**Test:**

```go
func TestConversationTombstone(t *testing.T) {
	conv := New()
	conv.AddUser("Hello")
	conv.AddAssistant([]ContentBlock{{Type: "text", Text: "Hi"}})
	conv.AddUser("Do something")
	
	// Tombstone the last user message
	conv.Tombstone(2, 3, agentsdk.TombstoneReasonModelFallback)
	
	msgs := conv.Messages()
	require.Equal(t, 3, len(msgs))
	require.False(t, agentsdk.IsTombstoned(msgs[0].Content[0].Text))
	require.False(t, agentsdk.IsTombstoned(msgs[1].Content[0].Text))
	require.True(t, agentsdk.IsTombstoned(msgs[2].Content[0].Text))
}

func TestTombstoneSinceLastAssistant(t *testing.T) {
	conv := New()
	conv.AddUser("Hello")
	conv.AddAssistant([]ContentBlock{{Type: "text", Text: "Hi"}})
	conv.AddUser("Do something")
	conv.AddAssistant([]ContentBlock{{Type: "text", Text: "Working..."}}) // partial
	
	count := conv.TombstoneSinceLastAssistant(agentsdk.TombstoneReasonModelFallback)
	require.Equal(t, 0, count) // Last assistant is complete, nothing to tombstone
	
	// Now add a partial user message
	conv.AddUser("More")
	count = conv.TombstoneSinceLastAssistant(agentsdk.TombstoneReasonModelFallback)
	require.Equal(t, 1, count)
}
```

**Command:**
```bash
go test ./internal/conversation/... -run TestConversationTombstone -v
```

**Expected:** Tests fail — tombstone not yet in Conversation.

---

## Chunk 3: Provider Integration

### Task 3: Skip tombstoned messages in provider requests

**Files:**
- Modify: `internal/provider/message.go` (or equivalent)

**Code:**

```go
// FilterTombstoned removes tombstoned messages from a slice.
// Preserves non-tombstoned messages in order.
func FilterTombstoned(messages []Message) []Message {
	var out []Message
	for _, m := range messages {
		if !isTombstonedMessage(m) {
			out = append(out, m)
		}
	}
	return out
}

func isTombstonedMessage(m Message) bool {
	if len(m.Content) == 0 {
		return false
	}
	return agentsdk.IsTombstoned(m.Content[0].Text)
}
```

In each provider's request builder:
```go
func (p *Provider) buildRequest(conv []Message) CompletionRequest {
	// Filter tombstoned messages before sending
	activeMessages := provider.FilterTombstoned(conv)
	
	return CompletionRequest{
		Messages: activeMessages,
		// ... other fields ...
	}
}
```

**Command:**
```bash
go test ./internal/provider/... -run TestFilterTombstoned -v
```

**Expected:** PASS.

---

## Chunk 4: Agent Integration

### Task 4: Tombstone on model fallback

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

```go
// In the model fallback path:
if class == errorclass.ClassModelOverloaded && a.fallbackModel != "" {
	// ... existing fallback logic ...
	
	// Tombstone orphaned messages from the failed attempt
	tombstonedCount := a.conversation.TombstoneSinceLastAssistant(agentsdk.TombstoneReasonModelFallback)
	if tombstonedCount > 0 {
		a.logger.Warn("tombstoned %d orphaned messages before fallback", tombstonedCount)
	}
	
	// Strip thinking blocks and retry with fallback
	fallbackReq.Messages = stripThinkingBlocks(req.Messages)
	// ... rest of fallback ...
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestModelFallback -v
```

**Expected:** Existing tests pass + tombstone behavior verified.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/conversation/...
go test ./internal/provider/...
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Streaming tombstone pattern for model fallback`

**Body:**
- Tombstone orphaned messages when switching to fallback model
- `TombstoneSinceLastAssistant` finds partial messages since last complete response
- Provider layer skips tombstoned messages when building requests
- Prevents context pollution from partial responses
- Ports Claude Code's tombstone pattern from `query.ts` fallback path

**Commit prefix:** `[BEHAVIORAL]`
