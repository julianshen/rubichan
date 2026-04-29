# Query Loop: Error Classifier Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract a typed error classifier that maps raw provider errors to structured categories, enabling targeted recovery strategies in subsequent plans.

**Architecture:** A new `internal/agent/errorclass/` package with a `Classify` function that inspects error messages and returns a typed `ErrorClass` enum. The classifier is used by the loop to decide recovery strategy. No behavioral changes yet — purely structural.

**Tech Stack:** Go, standard library string matching

---

## File Structure

| File | Responsibility |
|---|---|
| Create: `internal/agent/errorclass/classifier.go` | `ErrorClass` enum + `Classify(error)` function |
| Create: `internal/agent/errorclass/classifier_test.go` | Tests for all error categories |
| Modify: `internal/agent/turnretry.go` | Use `errorclass.IsRetryable` alongside existing `isRetryableProviderError` |

---

### Task 1: Define ErrorClass enum and Classify function

**Files:**
- Create: `internal/agent/errorclass/classifier.go`
- Create: `internal/agent/errorclass/classifier_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/errorclass/classifier_test.go
package errorclass

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyPromptTooLong(t *testing.T) {
	cases := []string{
		"prompt is too long: 204857 tokens > 200000 maximum",
		"Error: context_length_exceeded",
		"Request too large: 413",
		"too many tokens in request",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassPromptTooLong, Classify(fmt.Errorf(msg)))
		})
	}
}

func TestClassifyModelOverloaded(t *testing.T) {
	cases := []string{
		"Overloaded",
		"APIError 529: ",
		"ServiceUnavailable 503",
		"insufficient capacity",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassModelOverloaded, Classify(fmt.Errorf(msg)))
		})
	}
}

func TestClassifyMaxOutputTokens(t *testing.T) {
	cases := []string{
		"max_output_tokens exceeded",
		"Max output tokens exceeded: response was truncated",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassMaxOutputTokens, Classify(fmt.Errorf(msg)))
		})
	}
}

func TestClassifyMediaSize(t *testing.T) {
	cases := []string{
		"media_size_error: image too large",
		"file too large for processing",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.Equal(t, ClassMediaSize, Classify(fmt.Errorf(msg)))
		})
	}
}

func TestClassifyUnknown(t *testing.T) {
	assert.Equal(t, ClassUnknown, Classify(errors.New("something unexpected")))
	assert.Equal(t, ClassUnknown, Classify(nil))
}

func TestClassifyIsRetryable(t *testing.T) {
	assert.True(t, IsRetryable(ClassModelOverloaded))
	assert.False(t, IsRetryable(ClassPromptTooLong))
	assert.False(t, IsRetryable(ClassUnknown))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/errorclass/... -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// internal/agent/errorclass/classifier.go
package errorclass

import "strings"

type ErrorClass int

const (
	ClassUnknown         ErrorClass = iota
	ClassPromptTooLong
	ClassModelOverloaded
	ClassMaxOutputTokens
	ClassMediaSize
)

func Classify(err error) ErrorClass {
	if err == nil {
		return ClassUnknown
	}
	msg := err.Error()
	msgLower := strings.ToLower(msg)

	if isPromptTooLong(msg, msgLower) {
		return ClassPromptTooLong
	}
	if isMaxOutputTokens(msg, msgLower) {
		return ClassMaxOutputTokens
	}
	if isMediaSize(msgLower) {
		return ClassMediaSize
	}
	if isModelOverloaded(msg, msgLower) {
		return ClassModelOverloaded
	}
	return ClassUnknown
}

func IsRetryable(class ErrorClass) bool {
	return class == ClassModelOverloaded
}

func isPromptTooLong(msg, msgLower string) bool {
	return strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "413") ||
		strings.Contains(msgLower, "too many tokens")
}

func isModelOverloaded(msg, msgLower string) bool {
	return strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "529") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msgLower, "capacity")
}

func isMaxOutputTokens(msg, msgLower string) bool {
	return strings.Contains(msg, "max_output_tokens") ||
		strings.Contains(msgLower, "max output tokens exceeded")
}

func isMediaSize(msgLower string) bool {
	return strings.Contains(msgLower, "media_size_error") ||
		strings.Contains(msgLower, "image too large") ||
		strings.Contains(msgLower, "file too large")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/errorclass/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/errorclass/classifier.go internal/agent/errorclass/classifier_test.go
git commit -m "[STRUCTURAL] Add error classifier for provider error categorization"
```

---

### Task 2: Wire classifier into runLoop (read-only integration)

**Files:**
- Modify: `internal/agent/agent.go` — import and log classified error in the provider error path

- [ ] **Step 1: Write the failing test**

Add a test in `internal/agent/agent_test.go` that verifies a `prompt_too_long` error from the provider results in `ExitProviderError` (current behavior) and the error is logged. This is a characterization test to lock in behavior before we change it.

```go
func TestRunLoop_PromptTooLong_ExitsWithProviderError(t *testing.T) {
	prov := &providerErrorMock{err: fmt.Errorf("prompt is too long: 300000 tokens")}
	agent := newTestAgentWithProvider(prov)
	ch := agent.Turn(context.Background(), "hello")
	var exitReason agentsdk.TurnExitReason
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitProviderError, exitReason)
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/agent/... -run TestRunLoop_PromptTooLong -v`
Expected: PASS (this documents current behavior)

- [ ] **Step 3: Add import and classified logging in the error path**

In `agent.go`, at the provider error handling site (around line 1408-1411), add a log line using the classifier:

```go
import "github.com/julianshen/rubichan/internal/agent/errorclass"

// In the err != nil branch after TurnRetry:
if err != nil {
    a.logger.Warn("provider error classified as %s: %v", errorclass.Classify(err), err)
    // ... existing emit + return unchanged
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS — no behavioral change, just additional logging

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Log classified error category on provider failure"
```
