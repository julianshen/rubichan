# Query Loop: Max Output Tokens Recovery

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the model returns `stop_reason=max_tokens`, inject a continuation message and retry up to 3 times instead of just logging a warning and continuing with a truncated response.

**Architecture:** After stream processing, if `stopReason == "max_tokens"`, inject a synthetic user message asking the model to continue, decrement turn counter, and continue the loop. Track recovery attempts in a local counter to cap at 3.

**Tech Stack:** Go, existing `runLoop` infrastructure

**Depends on:** `2026-04-27-query-loop-error-classifier.md` (for error classification)

---

## File Structure

| File | Responsibility |
|---|---|
| Modify: `internal/agent/agent.go` | Add max_tokens recovery in the stop-reason handling path |

---

### Task 1: Add max_tokens recovery with continuation messages

**Files:**
- Modify: `internal/agent/agent.go` — around line 1546-1549

- [ ] **Step 1: Write the failing test**

```go
func TestRunLoop_MaxTokens_RetriesWithContinuation(t *testing.T) {
	callCount := 0
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		callCount++
		ch := make(chan provider.StreamEvent, 4)
		ch <- provider.StreamEvent{Type: "text_delta", Text: fmt.Sprintf("response_%d", callCount)}
		if callCount < 3 {
			ch <- provider.StreamEvent{Type: "done", StopReason: "max_tokens", InputTokens: 1, OutputTokens: 1}
		} else {
			ch <- provider.StreamEvent{Type: "done", StopReason: "end_turn", InputTokens: 1, OutputTokens: 1}
		}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	ch := agent.Turn(context.Background(), "hello")
	var exitReason agentsdk.TurnExitReason
	var output string
	for evt := range ch {
		if evt.Type == "text_delta" {
			output += evt.Text
		}
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitCompleted, exitReason)
	assert.Equal(t, 3, callCount, "should retry on max_tokens until end_turn")
	assert.Contains(t, output, "response_3")
}

func TestRunLoop_MaxTokens_StopsAfterMaxRecovery(t *testing.T) {
	callCount := 0
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		callCount++
		ch := make(chan provider.StreamEvent, 2)
		ch <- provider.StreamEvent{Type: "text_delta", Text: "truncated"}
		ch <- provider.StreamEvent{Type: "done", StopReason: "max_tokens", InputTokens: 1, OutputTokens: 1}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	// Set low maxTurns so the test finishes
	agent.maxTurns = 10
	ch := agent.Turn(context.Background(), "hello")
	var exitReason agentsdk.TurnExitReason
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitCompleted, exitReason)
	assert.LessOrEqual(t, callCount, 4, "should stop after max recovery attempts + 1")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestRunLoop_MaxTokens -v`
Expected: FAIL — current code does not retry on max_tokens

- [ ] **Step 3: Implement max_tokens recovery**

In `agent.go`, replace the current max_tokens handling (around line 1546-1549):

```go
const maxOutputTokensRecoveryLimit = 3

// Before the for loop, add:
maxTokensRecoveryAttempts := 0
hasEscalatedMaxTokens := false
```

Replace the existing max_tokens warning block with:

```go
if stopReason == agentsdk.StopReasonMaxTokens {
    if !hasEscalatedMaxTokens {
        hasEscalatedMaxTokens = true
        a.logger.Warn("response truncated by output token limit; escalating and retrying")
        a.conversation.AddAssistantMessage(blocks)
        a.conversation.AddUserMessage([]byte("[max_output_tokens escalation] Continuing with increased output limit."))
        turnCount--
        continue
    }
    if maxTokensRecoveryAttempts < maxOutputTokensRecoveryLimit {
        maxTokensRecoveryAttempts++
        a.conversation.AddAssistantMessage(blocks)
        a.conversation.AddUserMessage([]byte(
            fmt.Sprintf("[max_output_tokens recovery attempt %d/%d] Continue your response from where you left off.",
                maxTokensRecoveryAttempts, maxOutputTokensRecoveryLimit)))
        turnCount--
        continue
    }
    a.logger.Warn("response truncated by output token limit after %d recovery attempts", maxTokensRecoveryAttempts)
    // Fall through to normal completion with whatever we have
}
```

Also remove the existing warning-only line:
```go
// DELETE this line:
a.logger.Warn("response truncated by output token limit (consider increasing max_output_tokens in config)")
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Retry on max_tokens stop reason with continuation messages (up to 3 attempts)"
```
