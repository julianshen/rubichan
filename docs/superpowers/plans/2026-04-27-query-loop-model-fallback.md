# Query Loop: Model Fallback with Thinking Block Stripping

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the primary model returns an overloaded error, retry with a configured fallback model after stripping thinking blocks to reduce context size.

**Architecture:** Add a `FallbackModel` field to `Agent`. When `TurnRetry` exhausts all attempts with an overloaded error, strip thinking/reasoning blocks from messages and retry once with the fallback model via `TurnRetry`.

**Tech Stack:** Go, existing `TurnRetry` infrastructure

**Depends on:** `2026-04-27-query-loop-error-classifier.md`

---

## File Structure

| File | Responsibility |
|---|---|
| Create: `internal/agent/fallback.go` | `stripThinkingBlocks()`, `executeWithFallback()` |
| Create: `internal/agent/fallback_test.go` | Tests for thinking block stripping |
| Modify: `internal/agent/agent.go` | Wire fallback into runLoop error path |
| Modify: `internal/agent/agent.go` | Add `WithFallbackModel` option |
| Modify: `internal/agent/agent_options_test.go` | Test new option |

---

### Task 1: Implement stripThinkingBlocks

**Files:**
- Create: `internal/agent/fallback.go`
- Create: `internal/agent/fallback_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/fallback_test.go
package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestStripThinkingBlocks(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "text", Text: "hello"},
		}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "thinking", Text: "let me think"},
			{Type: "text", Text: "answer"},
		}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "redacted_thinking"},
			{Type: "text", Text: "more"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 3, len(stripped), "should preserve non-thinking messages")
	assert.Equal(t, 1, len(stripped[1].Content), "assistant msg should have thinking removed")
	assert.Equal(t, "text", stripped[1].Content[0].Type)
}

func TestStripThinkingBlocks_RemovesAllThinking(t *testing.T) {
	msgs := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "thinking", Text: "deep thought"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 0, len(stripped), "message with only thinking should be removed")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestStripThinking -v`
Expected: FAIL — `stripThinkingBlocks` undefined

- [ ] **Step 3: Write implementation**

```go
// internal/agent/fallback.go
package agent

import "github.com/julianshen/rubichan/internal/provider"

func stripThinkingBlocks(messages []provider.Message) []provider.Message {
	result := make([]provider.Message, 0, len(messages))
	for _, msg := range messages {
		var filtered []provider.ContentBlock
		for _, block := range msg.Content {
			if block.Type != "thinking" && block.Type != "redacted_thinking" {
				filtered = append(filtered, block)
			}
		}
		if len(filtered) == 0 && len(msg.Content) > 0 {
			continue
		}
		stripped := msg
		stripped.Content = filtered
		result = append(result, stripped)
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestStripThinking -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/fallback.go internal/agent/fallback_test.go
git commit -m "[STRUCTURAL] Add stripThinkingBlocks for model fallback context reduction"
```

---

### Task 2: Add WithFallbackModel option and wire into runLoop

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add fallback model field and option**

Add to Agent struct:

```go
fallbackModel string
```

Add option function:

```go
func WithFallbackModel(model string) AgentOption {
	return func(a *Agent) {
		a.fallbackModel = model
	}
}
```

- [ ] **Step 2: Wire into runLoop error path**

After the `TurnRetry` error handling, if the error is classified as `ClassModelOverloaded` and `a.fallbackModel != ""`:

```go
if class == errorclass.ClassModelOverloaded && a.fallbackModel != "" {
    a.logger.Warn("primary model overloaded; retrying with fallback model %s", a.fallbackModel)
    fallbackReq := req
    fallbackReq.Model = a.fallbackModel
    fallbackReq.Messages = stripThinkingBlocks(req.Messages)
    stream, fallbackErr := TurnRetry(ctx, retryCfg, func(ctx context.Context) (<-chan provider.StreamEvent, error) {
        return a.provider.Stream(ctx, fallbackReq)
    }, onRetry)
    if fallbackErr == nil {
        // Use the fallback stream instead of returning error
        goto processStream
    }
    a.logger.Warn("fallback model also failed: %v", fallbackErr)
}
```

Note: The `goto` pattern avoids restructuring the entire loop. Alternatively, extract the stream processing into a helper function and call it from both paths.

- [ ] **Step 3: Write integration test**

```go
func TestRunLoop_ModelOverloaded_FallsBack(t *testing.T) {
	callCount := 0
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("overloaded: server capacity reached")
		}
		ch := make(chan provider.StreamEvent, 2)
		ch <- provider.StreamEvent{Type: "text_delta", Text: "fallback response"}
		ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	agent.fallbackModel = "claude-haiku-4"
	ch := agent.Turn(context.Background(), "hello")
	var output string
	var exitReason agentsdk.TurnExitReason
	for evt := range ch {
		if evt.Type == "text_delta" {
			output += evt.Text
		}
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitCompleted, exitReason)
	assert.Contains(t, output, "fallback response")
	assert.Equal(t, 2, callCount, "should have called provider twice (primary + fallback)")
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Add model fallback on overloaded errors with thinking block stripping"
```
