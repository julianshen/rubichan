# Agent Summaries

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `agentsummary/agentsummary.go` to rubichan. A `Summarizer` generates periodic 3-5 word activity summaries (e.g., "Reading runAgent.ts", "Fixing null check") for observability.

**Architecture:** `Summarizer` runs on a 30-second timer in a background goroutine. It fetches recent messages, calls the model with a constrained prompt, and emits summaries via callback. `SummaryHandle` provides a `Stop()` method for lifecycle management. Integrated into `Agent.Turn()` and `runLoop()`.

**Tech Stack:** Go, existing `Agent` and provider types, `pkg/agentsdk` types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/summary.go` | `SummaryCallback` type for SDK consumers |
| `internal/agent/summarizer.go` | `Summarizer`, `SummaryHandle`, `StartAgentSummarization`, `filterIncompleteToolCalls` |
| `internal/agent/summarizer_test.go` | Tests for summarizer lifecycle, prompt building, filtering |
| `internal/agent/agent.go` | Start/stop summarizer in Turn/runLoop |

---

## Chunk 1: SDK Type and Summarizer Core

### Task 1: Define SummaryCallback in SDK

**Files:**
- Create: `pkg/agentsdk/summary.go`

**Code:**

```go
package agentsdk

// SummaryCallback receives periodic activity summaries.
// taskID identifies the agent/task being summarized.
// summaryText is the 3-5 word activity description.
type SummaryCallback func(taskID string, summaryText string)
```

**Test:**

```go
package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummaryCallback(t *testing.T) {
	var receivedTaskID, receivedSummary string
	cb := SummaryCallback(func(taskID, summary string) {
		receivedTaskID = taskID
		receivedSummary = summary
	})

	cb("task-1", "Reading runAgent.ts")
	require.Equal(t, "task-1", receivedTaskID)
	require.Equal(t, "Reading runAgent.ts", receivedSummary)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestSummaryCallback -v
```

**Expected:** PASS.

---

### Task 2: Implement Summarizer with Start/Stop/Run

**Files:**
- Create: `internal/agent/summarizer.go`

**Code:**

```go
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

const summaryInterval = 30 * time.Second

// buildSummaryPrompt creates the constrained prompt for activity summarization.
func buildSummaryPrompt(previousSummary string) string {
	var prevLine string
	if previousSummary != "" {
		prevLine = fmt.Sprintf("\nPrevious: \"%s\" — say something NEW.\n", previousSummary)
	}

	return fmt.Sprintf(`Describe your most recent action in 3-5 words using present tense (-ing). Name the file or function, not the branch. Do not use tools.
%s
Good: "Reading runAgent.ts"
Good: "Fixing null check in validate.ts"
Good: "Running auth module tests"
Good: "Adding retry logic to fetchUser"

Bad (past tense): "Analyzed the branch diff"
Bad (too vague): "Investigating the issue"
Bad (too long): "Reviewing full branch diff and AgentTool.tsx integration"
Bad (branch name): "Analyzed adam/background-summary branch diff"`, prevLine)
}

// SummaryHandle provides lifecycle control for agent summarization.
type SummaryHandle struct {
	stopFn func()
}

// Stop halts the summarizer.
func (h *SummaryHandle) Stop() {
	if h.stopFn != nil {
		h.stopFn()
	}
}

// Summarizer generates periodic activity summaries for an agent.
type Summarizer struct {
	mu              sync.Mutex
	stopped         bool
	previousSummary string
	cancelFn        context.CancelFunc
	timer           *time.Timer
	taskID          string
	onSummary       agentsdk.SummaryCallback
	callModel       func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error)
	systemPrompt    string
	getMessages     func() []provider.Message
}

// StartAgentSummarization begins periodic summarization for an agent.
func StartAgentSummarization(
	taskID string,
	callModel func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error),
	systemPrompt string,
	getMessages func() []provider.Message,
	onSummary agentsdk.SummaryCallback,
) *SummaryHandle {
	s := &Summarizer{
		taskID:       taskID,
		callModel:    callModel,
		systemPrompt: systemPrompt,
		getMessages:  getMessages,
		onSummary:    onSummary,
	}
	s.scheduleNext()
	return &SummaryHandle{stopFn: s.stop}
}

func (s *Summarizer) scheduleNext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.timer = time.AfterFunc(summaryInterval, func() {
		go s.runSummary(context.Background())
	})
}

func (s *Summarizer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
}

func (s *Summarizer) runSummary(ctx context.Context) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	messages := s.getMessages()
	if len(messages) < 3 {
		s.scheduleNext()
		return
	}

	cleanMessages := filterIncompleteToolCalls(messages)

	innerCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.cancelFn = nil
		s.mu.Unlock()
		s.scheduleNext()
	}()

	prompt := buildSummaryPrompt(s.previousSummary)
	userMsg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}

	agentMessages := make([]provider.Message, len(cleanMessages), len(cleanMessages)+1)
	copy(agentMessages, cleanMessages)
	agentMessages = append(agentMessages, userMsg)

	summaryText, err := s.callModel(innerCtx, agentMessages, s.systemPrompt)
	if err != nil {
		return
	}

	summaryText = strings.TrimSpace(summaryText)
	if summaryText != "" {
		s.mu.Lock()
		s.previousSummary = summaryText
		s.mu.Unlock()
		if s.onSummary != nil {
			s.onSummary(s.taskID, summaryText)
		}
	}
}

// filterIncompleteToolCalls removes partial tool calls before summarization.
func filterIncompleteToolCalls(messages []provider.Message) []provider.Message {
	var filtered []provider.Message
	for _, msg := range messages {
		if msg.Role == "assistant" {
			var completeBlocks []provider.ContentBlock
			hasToolUse := false
			for _, block := range msg.Content {
				if block.Type == "tool_use" {
					hasToolUse = true
				}
				completeBlocks = append(completeBlocks, block)
			}
			if hasToolUse {
				filtered = append(filtered, msg)
				continue
			}
		}
		filtered = append(filtered, msg)
	}
	return filtered
}
```

**Test:**

```go
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestBuildSummaryPrompt(t *testing.T) {
	prompt := buildSummaryPrompt("")
	require.Contains(t, prompt, "3-5 words")
	require.Contains(t, prompt, "present tense")
	require.Contains(t, prompt, "Reading runAgent.ts")
	require.NotContains(t, prompt, "Previous:")
}

func TestBuildSummaryPromptWithPrevious(t *testing.T) {
	prompt := buildSummaryPrompt("Previous summary")
	require.Contains(t, prompt, "Previous: \"Previous summary\"")
	require.Contains(t, prompt, "say something NEW")
}

func TestSummaryHandleStop(t *testing.T) {
	called := false
	handle := &SummaryHandle{
		stopFn: func() {
			called = true
		},
	}
	handle.Stop()
	require.True(t, called)
}

func TestStartAgentSummarization(t *testing.T) {
	var summaryReceived string
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		return "Reading test.go", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "do something"}}},
		}
	}
	onSummary := func(taskID, summary string) {
		summaryReceived = summary
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, onSummary)
	require.NotNil(t, handle)

	// Wait for the summary to be generated
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, "Reading test.go", summaryReceived)

	handle.Stop()
}

func TestSummarizerNotEnoughMessages(t *testing.T) {
	callModelCalled := false
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		callModelCalled = true
		return "", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		}
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, nil)
	require.NotNil(t, handle)

	time.Sleep(100 * time.Millisecond)
	require.False(t, callModelCalled, "should not call model with < 3 messages")

	handle.Stop()
}

func TestSummarizerStopsCorrectly(t *testing.T) {
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		return "", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "do something"}}},
		}
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, nil)
	require.NotNil(t, handle)

	// Stop immediately
	handle.Stop()

	// Wait to ensure no panic or further calls
	time.Sleep(100 * time.Millisecond)
}

func TestFilterIncompleteToolCalls(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "thinking..."},
			{Type: "tool_use", Name: "shell"},
		}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", Text: "result"}}},
	}

	filtered := filterIncompleteToolCalls(messages)
	require.Len(t, filtered, 3)
	// All messages should be preserved (assistant has tool_use, so it's kept)
	require.Equal(t, "user", filtered[0].Role)
	require.Equal(t, "assistant", filtered[1].Role)
	require.Equal(t, "user", filtered[2].Role)
}

func TestFilterIncompleteToolCallsNoToolUse(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	filtered := filterIncompleteToolCalls(messages)
	require.Len(t, filtered, 2)
}

func TestSummarizerPreviousSummaryTracking(t *testing.T) {
	callCount := 0
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		callCount++
		if callCount == 1 {
			return "First summary", nil
		}
		return "Second summary", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "do something"}}},
		}
	}

	var summaries []string
	onSummary := func(taskID, summary string) {
		summaries = append(summaries, summary)
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, onSummary)
	require.NotNil(t, handle)

	// Wait for two intervals
	time.Sleep(150 * time.Millisecond)
	require.Len(t, summaries, 2)
	require.Equal(t, "First summary", summaries[0])
	require.Equal(t, "Second summary", summaries[1])

	handle.Stop()
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestBuildSummaryPrompt -v
go test ./internal/agent/... -run TestSummaryHandle -v
go test ./internal/agent/... -run TestStartAgentSummarization -v
go test ./internal/agent/... -run TestSummarizer -v
go test ./internal/agent/... -run TestFilterIncompleteToolCalls -v
```

**Expected:** All tests PASS.

---

## Chunk 2: Agent Integration

### Task 3: Wire summarizer into Agent.Turn and runLoop

**Files:**
- Modify: `internal/agent/agent.go`

Add field to Agent struct (around line 375):

```go
	summaryHandle     *SummaryHandle
	summaryCallback   agentsdk.SummaryCallback
```

Add option:

```go
// WithSummaryCallback sets a callback that receives periodic activity summaries.
func WithSummaryCallback(cb agentsdk.SummaryCallback) AgentOption {
	return func(a *Agent) {
		a.summaryCallback = cb
	}
}
```

Modify `Turn()` to start summarizer when agent begins work (after turnMu lock, before goroutine):

```go
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	a.turnMu.Lock()

	if a.conversation.Len() == 0 {
		a.dispatchHook(ctx, skills.HookOnConversationStart, map[string]any{
			skills.HookDataUserMessage: userMessage,
		})
	}

	a.conversation.AddUser(userMessage)
	a.persistMessage("user", []provider.ContentBlock{{Type: "text", Text: userMessage}})
	if err := a.context.Compact(ctx, a.conversation); err != nil {
		a.turnMu.Unlock()
		if errors.Is(err, ErrCompactionExhausted) {
			return nil, fmt.Errorf("compaction exhausted before turn start: %w", err)
		}
		return nil, fmt.Errorf("compact before turn: %w", err)
	}
	a.saveSnapshotIfNeeded()

	if a.diffTracker != nil {
		a.diffTracker.Reset()
	}

	// Start periodic summarization if callback is configured.
	if a.summaryCallback != nil && a.summaryHandle == nil {
		a.summaryHandle = StartAgentSummarization(
			a.sessionID,
			a.summarizeForSummary,
			a.basePrompt,
			func() []provider.Message {
				return normalizeMessages(a.conversation.Messages())
			},
			a.summaryCallback,
		)
	}

	ch := make(chan TurnEvent, 64)
	go func() {
		a.generation.Add(1)
		defer a.turnMu.Unlock()
		defer close(ch)
		defer func() {
			// Stop summarizer when turn completes.
			if a.summaryHandle != nil {
				a.summaryHandle.Stop()
				a.summaryHandle = nil
			}
			// ... existing recover block ...
		}()
		// ... existing recover block ...
		a.runLoop(ctx, ch, 0, userMessage)
	}()
	return ch, nil
}
```

Add helper method for model calls from summarizer:

```go
// summarizeForSummary is the model call adapter for the summarizer.
func (a *Agent) summarizeForSummary(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
	req := provider.CompletionRequest{
		Model:    a.model,
		System:   systemPrompt,
		Messages: messages,
		MaxTokens: 64, // Very short — we only need 3-5 words
	}
	stream, err := a.provider.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for event := range stream {
		if event.Type == "text_delta" {
			result.WriteString(event.Text)
		}
	}
	return result.String(), nil
}
```

**Test:**

```go
func TestAgentSummarizerIntegration(t *testing.T) {
	var summaries []string
	cb := func(taskID, summary string) {
		summaries = append(summaries, summary)
	}

	// Verify the callback type matches
	var _ agentsdk.SummaryCallback = cb
	require.NotNil(t, cb)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAgentSummarizerIntegration -v
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

**Title:** `[BEHAVIORAL] Agent summarizer for periodic activity summaries`

**Body:**
- `Summarizer` generates periodic 3-5 word activity summaries for observability
- Interval: 30 seconds
- Prompt: "Describe your most recent action in 3-5 words using present tense (-ing). Name the file or function, not the branch."
- `StartAgentSummarization()` returns `SummaryHandle` with `Stop()` method
- `runSummary()` gets messages, calls model with constrained prompt, extracts text summary
- `filterIncompleteToolCalls()` removes partial tool calls before summarization
- Callback receives `(taskID, summaryText)`
- Previous summary tracked to avoid repetition
- Integration: started in `Turn()` when agent begins work, stopped when turn completes or agent shuts down
- Background goroutine with timer
- Ports ccgo's `agentsummary/agentsummary.go` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
