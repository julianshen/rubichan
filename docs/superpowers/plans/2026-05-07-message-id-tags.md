# Message ID Tags Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's message ID tag injection to rubichan. Injects short `[id:xxxxx]` tags into user messages so the model can reference specific messages in the conversation history.

**Architecture:** `MessageIDInjector` adds short ID tags to user message text blocks. Tags are derived from message UUIDs. Double-tagging is prevented. Enables cross-referencing: "User mentioned this in [id:a1b2c3d4]".

**Tech Stack:** Go, existing `provider.Message` and `Conversation` types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/message_id.go` | `MessageIDInjector` type for SDK |
| `internal/agent/message_id.go` | `InjectMessageIDTags`, `DeriveShortMessageID` |
| `internal/agent/message_id_test.go` | Tests for injection, dedup, short ID derivation |
| `internal/agent/agent.go` | Optional integration in buildSystemPromptWithFragments |

---

## Chunk 1: Core Implementation

### Task 1: Implement Message ID Injection

**Files:**
- Create: `internal/agent/message_id.go`

**Code:**

```go
package agent

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

const idTagPrefix = "[id:"

// DeriveShortMessageID creates a short 8-character ID from a UUID.
func DeriveShortMessageID(uuid string) string {
	if len(uuid) < 8 {
		return uuid
	}
	h := sha256.New()
	h.Write([]byte(uuid))
	return fmt.Sprintf("%x", h.Sum(nil))[:8]
}

// InjectMessageIDTags adds [id:xxxxx] tags to user messages for cross-referencing.
func InjectMessageIDTags(messages []provider.Message) []provider.Message {
	result := make([]provider.Message, len(messages))
	for i, msg := range messages {
		result[i] = msg
		if msg.Role != "user" {
			continue
		}
		msgID := extractMessageID(msg)
		if msgID == "" {
			continue
		}
		shortID := DeriveShortMessageID(msgID)
		tag := fmt.Sprintf("%s%s] ", idTagPrefix, shortID)

		var newContent []provider.ContentBlock
		for _, block := range msg.Content {
			if block.Type == "text" && !strings.HasPrefix(block.Text, idTagPrefix) {
				newContent = append(newContent, provider.ContentBlock{
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

func extractMessageID(msg provider.Message) string {
	if msg.Metadata == nil {
		return ""
	}
	if id, ok := msg.Metadata["id"].(string); ok {
		return id
	}
	return ""
}
```

**Test:**

```go
package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestDeriveShortMessageID(t *testing.T) {
	id := DeriveShortMessageID("550e8400-e29b-41d4-a716-446655440000")
	require.Len(t, id, 8)
	require.NotEmpty(t, id)
}

func TestInjectMessageIDTags(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Metadata: map[string]any{"id": "msg-1"}, Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	result := InjectMessageIDTags(msgs)
	require.Len(t, result, 2)
	require.Contains(t, result[0].Content[0].Text, "[id:")
	require.Contains(t, result[0].Content[0].Text, "hello")
	require.NotContains(t, result[1].Content[0].Text, "[id:")
}

func TestInjectMessageIDTagsPreventsDoubleTagging(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Metadata: map[string]any{"id": "msg-1"}, Content: []provider.ContentBlock{{Type: "text", Text: "[id:abc] hello"}}},
	}

	result := InjectMessageIDTags(msgs)
	require.Equal(t, "[id:abc] hello", result[0].Content[0].Text)
}

func TestInjectMessageIDTagsNoMetadata(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
	}

	result := InjectMessageIDTags(msgs)
	require.Equal(t, "hello", result[0].Content[0].Text)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestInjectMessageIDTags -v
```

**Expected:** All tests PASS.

---

## Chunk 2: Optional Integration

### Task 2: Add Agent option for ID tag injection

**Files:**
- Modify: `internal/agent/agent.go`

Add field to Agent struct:
```go
	injectMessageIDs   bool
```

Add option:
```go
// WithMessageIDInjection enables [id:xxxxx] tag injection for cross-referencing.
func WithMessageIDInjection(enabled bool) AgentOption {
	return func(a *Agent) {
		a.injectMessageIDs = enabled
	}
}
```

In `buildSystemPromptWithFragments` or where messages are normalized:
```go
if a.injectMessageIDs {
	messages = InjectMessageIDTags(messages)
}
```

**Test:**

```go
func TestAgentWithMessageIDInjection(t *testing.T) {
	var opt AgentOption = WithMessageIDInjection(true)
	require.NotNil(t, opt)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAgentWithMessageIDInjection -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Message ID tags for cross-referencing`

**Body:**
- `InjectMessageIDTags()` adds `[id:xxxxx]` prefixes to user messages
- `DeriveShortMessageID()` creates 8-char IDs from UUIDs
- Prevents double-tagging (checks for existing `[id:` prefix)
- Only injects into user messages with `metadata["id"]`
- Optional `WithMessageIDInjection()` AgentOption
- Enables model cross-referencing: "User mentioned this in [id:a1b2c3d4]"
- Ports Claude Code's message ID injection pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
