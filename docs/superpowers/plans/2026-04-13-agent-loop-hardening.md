# Agent Loop Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close five production-readiness gaps in Rubichan's agent loop identified by comparing against Claude Code's `query.ts` (see `docs/superpowers/plans/reference_query_ts.md`): typed exit reasons, orphaned tool_result safety net, compaction circuit breaker, per-tool result size budget, and streaming tool execution for concurrency-safe read-only tools.

**Architecture:** Five independent but ordered subsystems. Task 1 (typed exit reasons) is a foundation that later tasks assert on in tests. Task 2 (orphan safety net) and Task 3 (circuit breaker) are small, high-ROI correctness fixes. Task 4 (per-tool result cap) adds a new `MaxResultBytes` capability to the `Tool` interface via an optional extension interface (preserves backward compatibility). Task 5 (streaming tool exec) is the largest change — it adds a `ConcurrencySafe` marker and dispatches safe tools the moment their `input_json_delta` finalizes, while unsafe tools remain queued until stream end.

**Tech Stack:** Go 1.22+, `sourcegraph/conc/pool` for bounded parallelism, existing `provider.StreamEvent` channel model, `modernc.org/sqlite` for persistence (unchanged by this plan).

**Non-Goals:** Model fallback, withholding pattern, Haiku background summarization, token budgets, abort-type distinction. Those are separate plans.

**Invariants preserved:**
- `TurnEvent{Type: "done"}` is always the last event emitted on the channel (existing contract at `agent.go:882-884`).
- Every `AddAssistant` with a `tool_use` block is followed by matching `AddToolResult` calls before the next iteration or loop exit.
- Concurrent `Turn()` calls remain serialized via `a.turnMu`.

---

## File Structure

**New files:**
- `pkg/agentsdk/exit_reason.go` — `TurnExitReason` enum (Task 1)
- `internal/agent/orphan.go` — `synthesizeMissingToolResults` helper (Task 2)
- `internal/agent/orphan_test.go` — unit tests (Task 2)
- `internal/agent/compaction_breaker_test.go` — circuit breaker tests (Task 3)
- `pkg/agentsdk/tool_caps.go` — `ResultCappedTool` and `ConcurrencySafeTool` optional interfaces (Tasks 4 & 5)
- `internal/agent/result_cap.go` — enforcement helper (Task 4)
- `internal/agent/result_cap_test.go` — tests (Task 4)
- `internal/agent/stream_tool_exec.go` — streaming dispatcher (Task 5)
- `internal/agent/stream_tool_exec_test.go` — tests (Task 5)

**Modified files:**
- `pkg/agentsdk/events.go` — add `ExitReason` field to `TurnEvent` (Task 1)
- `internal/agent/agent.go` — `runLoop` emits exit reasons, calls orphan sweeper, uses circuit breaker, dispatches streaming tools (Tasks 1, 2, 3, 5)
- `internal/agent/context.go` — `ContextManager` tracks consecutive compaction failures (Task 3)

---

## Task 1: Typed Exit Reasons

**Why first:** Tasks 2–5 all need to assert in tests that the loop exited for the *correct* reason. Today tests can only assert "did an error event arrive." Ch.5 names this as one of the three reasons async generators beat event emitters; the Go analogue is a final `done` event carrying a typed reason.

**Files:**
- Create: `pkg/agentsdk/exit_reason.go`
- Modify: `pkg/agentsdk/events.go` (add field to `TurnEvent`)
- Modify: `internal/agent/agent.go` (thread reason through every `makeDoneEvent` call site)
- Modify: `internal/agent/agent_test.go` (add one assertion on reason to an existing test)

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/agent_test.go` at the end of the file:

```go
func TestTurnEmitsExitReasonCompleted(t *testing.T) {
	t.Parallel()
	ag, _ := newTestAgentWithStaticReply(t, "done.")
	ch, err := ag.Turn(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	var last TurnEvent
	for ev := range ch {
		last = ev
	}
	if last.Type != "done" {
		t.Fatalf("want last event type=done, got %q", last.Type)
	}
	if last.ExitReason != agentsdk.ExitCompleted {
		t.Fatalf("want ExitCompleted, got %v", last.ExitReason)
	}
}
```

If `newTestAgentWithStaticReply` does not exist, search `agent_test.go` for the equivalent fixture (look for `newTestAgent` + a fake provider returning a single text block) and use it — do not invent a new fixture.

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/agent/ -run TestTurnEmitsExitReasonCompleted
```
Expected: compile error — `agentsdk.ExitCompleted` undefined, `ExitReason` field missing.

- [ ] **Step 3: Create the exit reason enum**

Create `pkg/agentsdk/exit_reason.go`:

```go
package agentsdk

// TurnExitReason enumerates why a turn stopped. Every "done" TurnEvent
// carries exactly one of these. New reasons must be added here and nowhere
// else; callers switch on this value.
type TurnExitReason int

const (
	// ExitUnknown is the zero value. Emitting a done event with this reason
	// is a bug — it means a code path forgot to set a reason.
	ExitUnknown TurnExitReason = iota

	// ExitCompleted: the model returned no tool calls and no error.
	ExitCompleted

	// ExitMaxTurns: the loop hit its maxTurns ceiling.
	ExitMaxTurns

	// ExitCancelled: ctx.Err() was observed (user abort, timeout, etc.).
	ExitCancelled

	// ExitProviderError: the provider returned an unrecoverable error.
	ExitProviderError

	// ExitRateLimited: the rate limiter returned an error from Wait().
	ExitRateLimited

	// ExitSkillActivationFailed: skill runtime could not evaluate triggers.
	ExitSkillActivationFailed

	// ExitTaskComplete: model invoked the task_complete tool.
	ExitTaskComplete

	// ExitNoProgress: maxRepeatedPendingToolRounds reached.
	ExitNoProgress

	// ExitEmptyResponse: model returned no text and no tool calls.
	ExitEmptyResponse

	// ExitCompactionFailed: compaction circuit breaker tripped (Task 3).
	ExitCompactionFailed

	// ExitProtocolViolation: orphaned tool_use blocks detected and not
	// recoverable (reserved for Task 2).
	ExitProtocolViolation

	// ExitPanic: a panic was recovered in Turn's deferred handler.
	ExitPanic
)

// String returns a stable lowercase identifier usable in logs and tests.
func (r TurnExitReason) String() string {
	switch r {
	case ExitCompleted:
		return "completed"
	case ExitMaxTurns:
		return "max_turns"
	case ExitCancelled:
		return "cancelled"
	case ExitProviderError:
		return "provider_error"
	case ExitRateLimited:
		return "rate_limited"
	case ExitSkillActivationFailed:
		return "skill_activation_failed"
	case ExitTaskComplete:
		return "task_complete"
	case ExitNoProgress:
		return "no_progress"
	case ExitEmptyResponse:
		return "empty_response"
	case ExitCompactionFailed:
		return "compaction_failed"
	case ExitProtocolViolation:
		return "protocol_violation"
	case ExitPanic:
		return "panic"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 4: Add ExitReason field to TurnEvent**

Edit `pkg/agentsdk/events.go`. Find the `TurnEvent` struct definition (starts at line 5). Add a new field immediately after `ContextBudget`:

```go
	ContextBudget  *ContextBudget     // populated for done events: per-component context usage breakdown
	ExitReason     TurnExitReason     // populated for done events: why the turn stopped
```

- [ ] **Step 5: Thread ExitReason through makeDoneEvent**

Edit `internal/agent/agent.go:1140`. Change the signature and body:

```go
func (a *Agent) makeDoneEvent(inputTokens, outputTokens int, reason agentsdk.TurnExitReason) TurnEvent {
	budget := a.context.Budget()
	event := TurnEvent{
		Type:          "done",
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		ContextBudget: &budget,
		ExitReason:    reason,
	}
	if a.diffTracker != nil {
		event.DiffSummary = a.diffTracker.Summarize()
	}
	return event
}
```

- [ ] **Step 6: Update every makeDoneEvent call site**

Run `grep -n makeDoneEvent internal/agent/agent.go` — there are calls around lines 884, 1163, 1173, 1233, 1241, 1362, 1411, 1424, 1434, 1442, 1453, plus any missed. For each, supply the right reason. Exact mapping:

| File:line context | Reason |
|---|---|
| Panic recover (`agent.go:884`) | `agentsdk.ExitPanic` |
| Skill activation error (`:1163`) | `agentsdk.ExitSkillActivationFailed` |
| `ctx.Err()` at loop top (`:1173`) | `agentsdk.ExitCancelled` |
| Rate limiter Wait error (`:1233`) | `agentsdk.ExitRateLimited` |
| Provider Stream error (`:1241`) | `agentsdk.ExitProviderError` |
| Stream errored mid-way (`:1362`) | `agentsdk.ExitProviderError` |
| No pending tools — happy path (`:1411`) | `agentsdk.ExitCompleted` |
| No-progress guard (`:1424`) | `agentsdk.ExitNoProgress` |
| `task_complete` tool (`:1434`) | `agentsdk.ExitTaskComplete` |
| Tool exec cancelled (`:1442`) | `agentsdk.ExitCancelled` |
| Max turns exceeded (`:1453`) | `agentsdk.ExitMaxTurns` |

For the `len(blocks) == 0 && len(pendingTools) == 0` case at agent.go:1397, that path currently appends a placeholder and falls through to the no-pending-tools exit at :1411. Add a sibling error-event emission but still exit via `ExitEmptyResponse` *instead of* `ExitCompleted`. Specifically, before the `if len(pendingTools) == 0` block at :1410, add:

```go
		emptyResponseExit := false
		if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].Text == "[empty response from model]" {
			emptyResponseExit = true
		}
```

And change the no-pending-tools exit to:

```go
		if len(pendingTools) == 0 {
			reason := agentsdk.ExitCompleted
			if emptyResponseExit {
				reason = agentsdk.ExitEmptyResponse
			}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens, reason)
			return
		}
```

- [ ] **Step 7: Run the test**

```
go test ./internal/agent/ -run TestTurnEmitsExitReasonCompleted -v
```
Expected: PASS.

- [ ] **Step 8: Run full agent test suite**

```
go test ./internal/agent/...
```
Expected: all PASS. If any test fails because it switched on a `done` event without setting `ExitReason`, those failures are real — fix the call site, not the test.

- [ ] **Step 9: Commit**

```bash
git add pkg/agentsdk/exit_reason.go pkg/agentsdk/events.go internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Add TurnExitReason enum to done events

Every turn now ends with a typed reason (completed, max_turns, cancelled,
provider_error, etc.) instead of an untyped done event. Tests can now
assert on the correct exit path instead of 'no error event arrived'.

Refs: docs/superpowers/plans/2026-04-13-agent-loop-hardening.md Task 1"
```

---

## Task 2: Orphaned Tool Result Safety Net

**Why:** If `provider.Stream` errors partway through emitting a `tool_use` block — or if the context is cancelled between stream end and `executeTools` — the next API request contains an assistant message with a `tool_use` block that has no matching `tool_result`. Anthropic returns 400 ("messages.N.content.M: tool_use ids were found without tool_result blocks"). Ch.5 calls this the protocol safety net; Claude Code fires it in three places (model crash, fallback, abort). Rubichan fires it zero places.

**Files:**
- Create: `internal/agent/orphan.go`
- Create: `internal/agent/orphan_test.go`
- Modify: `internal/agent/agent.go` (call sweeper before every early return after a stream failure)

- [ ] **Step 1: Write the failing test**

Create `internal/agent/orphan_test.go`:

```go
package agent

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
)

func TestSynthesizeMissingToolResultsFillsOrphans(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "text", Text: "using tools"},
		{Type: "tool_use", ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"a"}`)},
		{Type: "tool_use", ID: "call_2", Name: "read_file", Input: json.RawMessage(`{"path":"b"}`)},
	})

	n := synthesizeMissingToolResults(conv, "stream aborted")
	if n != 2 {
		t.Fatalf("want 2 orphans sealed, got %d", n)
	}

	msgs := conv.Messages()
	got := map[string]bool{}
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == "tool_result" {
				got[b.ToolUseID] = true
			}
		}
	}
	if !got["call_1"] || !got["call_2"] {
		t.Fatalf("missing tool_result for orphans: %v", got)
	}
}

func TestSynthesizeMissingToolResultsSkipsIfAlreadyAnswered(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "tool_use", ID: "call_1", Name: "read_file", Input: json.RawMessage(`{}`)},
	})
	conv.AddToolResult("call_1", "ok", false)

	n := synthesizeMissingToolResults(conv, "reason")
	if n != 0 {
		t.Fatalf("want 0 orphans, got %d", n)
	}
}

func TestSynthesizeMissingToolResultsNoAssistantTail(t *testing.T) {
	t.Parallel()
	conv := NewConversation("sys")
	conv.AddUser("hi")

	n := synthesizeMissingToolResults(conv, "reason")
	if n != 0 {
		t.Fatalf("want 0 when last message is user, got %d", n)
	}
}
```

- [ ] **Step 2: Run the test**

```
go test ./internal/agent/ -run TestSynthesizeMissingToolResults -v
```
Expected: compile error — `synthesizeMissingToolResults` undefined.

- [ ] **Step 3: Implement the sweeper**

Create `internal/agent/orphan.go`:

```go
package agent

import "fmt"

// synthesizeMissingToolResults walks the conversation and, for every
// tool_use block in the most recent assistant message that does not have
// a matching tool_result in a subsequent message, appends an error
// tool_result. Returns the number of orphans sealed.
//
// This exists because the Anthropic/OpenAI wire protocol requires every
// tool_use block to be followed by a tool_result. If the stream dies
// between tool_use emission and tool execution, the next API call fails
// with a 400 protocol error. Sealing orphans with a synthetic error
// result keeps the conversation valid for a retry or for resume from
// a persisted snapshot.
//
// Called from every agent-loop exit path that follows a stream which may
// have emitted tool_use blocks: provider stream error, context cancel
// mid-stream, rate-limiter error, and the deferred panic handler.
func synthesizeMissingToolResults(conv *Conversation, reason string) int {
	msgs := conv.Messages()
	if len(msgs) == 0 {
		return 0
	}

	// Find the last assistant message.
	assistantIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			assistantIdx = i
			break
		}
	}
	if assistantIdx == -1 {
		return 0
	}

	// Collect tool_use IDs in that assistant message.
	var pendingIDs []string
	for _, block := range msgs[assistantIdx].Content {
		if block.Type == "tool_use" && block.ID != "" {
			pendingIDs = append(pendingIDs, block.ID)
		}
	}
	if len(pendingIDs) == 0 {
		return 0
	}

	// Collect tool_result IDs that appear after the assistant message.
	answered := map[string]bool{}
	for i := assistantIdx + 1; i < len(msgs); i++ {
		for _, block := range msgs[i].Content {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				answered[block.ToolUseID] = true
			}
		}
	}

	sealed := 0
	for _, id := range pendingIDs {
		if answered[id] {
			continue
		}
		conv.AddToolResult(id,
			fmt.Sprintf("tool execution did not complete: %s", reason),
			true,
		)
		sealed++
	}
	return sealed
}
```

- [ ] **Step 4: Run the test**

```
go test ./internal/agent/ -run TestSynthesizeMissingToolResults -v
```
Expected: all three PASS.

- [ ] **Step 5: Wire the sweeper into runLoop exit paths**

Edit `internal/agent/agent.go`. Every early return in `runLoop` that happens *after* `stream, err := a.provider.Stream(ctx, req)` at line 1238 must call the sweeper before returning. Locations:

1. Stream error at `:1361` (`if streamErr { ... return }`) — before the `ch <-` line, insert:
   ```go
   synthesizeMissingToolResults(a.conversation, "stream error")
   ```
2. Tool exec cancelled at `:1440` — before the `ch <-`, insert:
   ```go
   synthesizeMissingToolResults(a.conversation, "cancelled during tool execution")
   ```
3. Deferred panic handler at `agent.go:869-887` in `Turn()` — after the `a.logger.Error` line, insert:
   ```go
   synthesizeMissingToolResults(a.conversation, "agent panic")
   ```
4. `ctx.Err()` check at `:1171` — this runs *before* the stream, so the previous turn's tool_use blocks (if any) are already matched. Do **not** call the sweeper here; if you do, it will no-op, but a misleading "orphan sealed" log could appear.

Do not wire the sweeper into the `max_turns` exit or the happy-path `ExitCompleted` exit — those paths reach the end of an iteration naturally and all tool_use blocks already have results.

- [ ] **Step 6: Write the integration test**

Add to `internal/agent/orphan_test.go`:

```go
func TestRunLoopStreamErrorLeavesNoOrphans(t *testing.T) {
	t.Parallel()
	// Fake provider that emits a tool_use then an error event.
	fp := &fakeProviderToolThenError{
		toolID:   "call_1",
		toolName: "read_file",
		input:    `{"path":"x"}`,
		errMsg:   "simulated stream failure",
	}
	ag := newTestAgentWithProvider(t, fp)
	ch, err := ag.Turn(context.Background(), "read x")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	for range ch {
	}

	// The conversation must not end with an unmatched tool_use.
	msgs := ag.conversation.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		for _, b := range msgs[i].Content {
			if b.Type != "tool_use" {
				continue
			}
			// Found a tool_use; a tool_result for it must appear after.
			found := false
			for j := i + 1; j < len(msgs); j++ {
				for _, rb := range msgs[j].Content {
					if rb.Type == "tool_result" && rb.ToolUseID == b.ID {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("orphan tool_use %s survived runLoop exit", b.ID)
			}
		}
		break
	}
}
```

Reuse whatever fake provider pattern `agent_test.go` already establishes. If there's no existing "emit tool_use then error" helper, add one minimal shim at the bottom of `orphan_test.go`, not a new file. If the helper already exists, use it.

- [ ] **Step 7: Run the integration test**

```
go test ./internal/agent/ -run TestRunLoopStreamErrorLeavesNoOrphans -v
```
Expected: PASS.

- [ ] **Step 8: Run full package tests**

```
go test ./internal/agent/... ./pkg/agentsdk/...
```
Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/agent/orphan.go internal/agent/orphan_test.go internal/agent/agent.go
git commit -m "[BEHAVIORAL] Seal orphaned tool_use blocks on abnormal loop exit

When Stream errors, context is cancelled mid-tool-exec, or the agent
panics, the last assistant message may contain tool_use blocks with no
matching tool_result. The wire protocol rejects this on the next API
call with a 400. synthesizeMissingToolResults appends error tool_results
for every orphan so the conversation stays valid and resumable.

Refs: docs/superpowers/plans/2026-04-13-agent-loop-hardening.md Task 2"
```

---

## Task 3: Compaction Circuit Breaker

**Why:** Ch.5 documents the production horror: "sessions stuck over the context limit burning 250K API calls per day in an infinite compact-fail-retry loop." Rubichan's `ContextManager.Compact` at `context.go:87` silently swallows strategy errors and moves on; if the summarizer consistently errors and no other strategy shrinks the conversation, the loop will re-attempt compaction every turn forever — and `IsBlocked` at hard block will loop into `ForceCompact` which also silently fails.

Add a consecutive-failure counter to `ContextManager`. When it hits 3, surface `ErrCompactionExhausted`. The agent loop catches this and exits with `ExitCompactionFailed`.

**Files:**
- Modify: `internal/agent/context.go` (add counter + error)
- Create: `internal/agent/compaction_breaker_test.go`
- Modify: `internal/agent/agent.go` (catch error at two Compact call sites, exit with typed reason)

- [ ] **Step 1: Write the failing test**

Create `internal/agent/compaction_breaker_test.go`:

```go
package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// alwaysFailingStrategy returns a non-nil error from every Compact call.
type alwaysFailingStrategy struct{ name string }

func (s *alwaysFailingStrategy) Name() string { return s.name }
func (s *alwaysFailingStrategy) Compact(_ context.Context, msgs []provider.Message, _ int) ([]provider.Message, error) {
	return msgs, errors.New("boom")
}

func TestCompactionCircuitBreakerTripsAfterThreeFailures(t *testing.T) {
	t.Parallel()
	cm := NewContextManager(100, 10) // tiny budget to force compaction
	cm.SetStrategies([]agentsdk.CompactionStrategy{&alwaysFailingStrategy{name: "fail"}})

	conv := NewConversation("system")
	for i := 0; i < 50; i++ {
		conv.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}

	for i := 0; i < 2; i++ {
		err := cm.Compact(context.Background(), conv)
		if err != nil {
			t.Fatalf("attempt %d: want nil, got %v", i+1, err)
		}
	}
	if err := cm.Compact(context.Background(), conv); !errors.Is(err, ErrCompactionExhausted) {
		t.Fatalf("want ErrCompactionExhausted on third failure, got %v", err)
	}
}

func TestCompactionCircuitBreakerResetsOnSuccess(t *testing.T) {
	t.Parallel()
	cm := NewContextManager(100, 10)
	succeedNext := false
	strat := &fakeStrategyWithToggle{succeed: &succeedNext}
	cm.SetStrategies([]agentsdk.CompactionStrategy{strat})

	conv := NewConversation("system")
	for i := 0; i < 50; i++ {
		conv.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}

	_ = cm.Compact(context.Background(), conv)
	_ = cm.Compact(context.Background(), conv)
	succeedNext = true
	if err := cm.Compact(context.Background(), conv); err != nil {
		t.Fatalf("success should reset counter, got %v", err)
	}
	succeedNext = false
	for i := 0; i < 20; i++ {
		conv.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}
	// After reset, three new failures needed to trip.
	_ = cm.Compact(context.Background(), conv)
	_ = cm.Compact(context.Background(), conv)
	if err := cm.Compact(context.Background(), conv); !errors.Is(err, ErrCompactionExhausted) {
		t.Fatalf("want trip after 3 new failures, got %v", err)
	}
}

type fakeStrategyWithToggle struct{ succeed *bool }

func (s *fakeStrategyWithToggle) Name() string { return "toggle" }
func (s *fakeStrategyWithToggle) Compact(_ context.Context, msgs []provider.Message, _ int) ([]provider.Message, error) {
	if *s.succeed {
		// Drop half to register as a shrink.
		if len(msgs) > 2 {
			return msgs[len(msgs)/2:], nil
		}
		return msgs, nil
	}
	return msgs, errors.New("boom")
}
```

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/agent/ -run TestCompactionCircuitBreaker -v
```
Expected: compile error — `ErrCompactionExhausted` undefined, `Compact` currently has no error return.

- [ ] **Step 3: Modify ContextManager**

Edit `internal/agent/context.go`. Add after the existing imports:

```go
import "errors"
```

(Merge into the existing import block.)

At the top of the file, after the `import` block, add:

```go
// ErrCompactionExhausted is returned from Compact/ForceCompact when the
// strategy chain has failed MaxConsecutiveCompactionFailures times in a
// row without reducing token count. The loop must terminate rather than
// retry — infinite compact-fail-retry will burn API budget with no
// progress. Observed in Claude Code as "250K API calls/day" incidents.
var ErrCompactionExhausted = errors.New("compaction failed repeatedly; circuit breaker tripped")

// MaxConsecutiveCompactionFailures is the threshold for the circuit
// breaker. Matches Claude Code's MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES=3.
const MaxConsecutiveCompactionFailures = 3
```

Modify the `ContextManager` struct at line 17 to add the counter:

```go
type ContextManager struct {
	budget               ContextBudget
	compactTrigger       float64
	hardBlock            float64
	strategies           []CompactionStrategy
	consecutiveFailures  int
}
```

Change `Compact`'s signature (line 87) from returning nothing to returning an error. Rewrite the body:

```go
func (cm *ContextManager) Compact(ctx context.Context, conv *Conversation) error {
	if !cm.ShouldCompact(conv) {
		return nil
	}
	systemTokens := len(conv.SystemPrompt())/4 + 10
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	if messageBudget < 0 {
		messageBudget = 0
	}

	signals := ComputeConversationSignals(conv.messages)
	for _, s := range cm.strategies {
		if sa, ok := s.(SignalAware); ok {
			sa.SetSignals(signals)
		}
	}

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

	afterTokens := estimateMessageTokens(conv.messages)
	shrank := afterTokens < beforeTokens

	if anyStrategySucceeded && shrank {
		cm.consecutiveFailures = 0
		return nil
	}

	cm.consecutiveFailures++
	if cm.consecutiveFailures >= MaxConsecutiveCompactionFailures {
		return ErrCompactionExhausted
	}
	return nil
}
```

The key semantic change: a "success" now requires both (a) at least one strategy returning without error and (b) the token count actually dropping. Silent-no-op strategies no longer reset the breaker.

- [ ] **Step 4: Run the tests**

```
go test ./internal/agent/ -run TestCompactionCircuitBreaker -v
```
Expected: PASS.

- [ ] **Step 5: Fix downstream callers of Compact**

`Compact` now returns an error. Find callers:

```
grep -n "\.context\.Compact(\|cm\.Compact(\|\.Compact(ctx, " internal/agent/ -r
```

Three in-package call sites to handle:

1. `agent.go:857` (in `Turn`):
   ```go
   if err := a.context.Compact(ctx, a.conversation); err != nil && errors.Is(err, ErrCompactionExhausted) {
       a.turnMu.Unlock()
       return nil, fmt.Errorf("compaction exhausted before turn start: %w", err)
   }
   ```
   (Return error synchronously — the channel has not been created yet.)

2. `context.go:222` (deprecated `Truncate`): ignore the error — this is a shim.
   ```go
   func (cm *ContextManager) Truncate(conv *Conversation) {
       _ = cm.Compact(context.Background(), conv)
   }
   ```

3. Any test files that call `cm.Compact(...)` — just add `_ =` or `if err := ...`.

- [ ] **Step 6: Wire the breaker into runLoop**

Edit `internal/agent/agent.go` around line 1213 where `IsBlocked` triggers `ForceCompact`. `ForceCompact` does not itself participate in the breaker (it returns a result struct, not an error), so instead add a proactive check at the top of each loop iteration right after `ctx.Err() != nil`:

```go
		if ctx.Err() != nil {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitCancelled)
			return
		}

		if err := a.context.Compact(ctx, a.conversation); err != nil {
			if errors.Is(err, ErrCompactionExhausted) {
				ch <- TurnEvent{Type: "error", Error: err}
				ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitCompactionFailed)
				return
			}
			// Non-breaker errors should not happen today; log and continue.
			a.logger.Warn("compaction returned unexpected error: %v", err)
		}
```

Add `"errors"` to the imports if not already present.

- [ ] **Step 7: Write a runLoop integration test**

Add to `compaction_breaker_test.go`:

```go
func TestRunLoopExitsWithCompactionFailed(t *testing.T) {
	t.Parallel()
	ag := newTestAgentWithStaticReply(t, "ignored")
	// Replace context manager with one that always fails.
	ag.context = NewContextManager(50, 5)
	ag.context.SetStrategies([]agentsdk.CompactionStrategy{&alwaysFailingStrategy{name: "fail"}})
	// Bloat the conversation so ShouldCompact is true.
	for i := 0; i < 100; i++ {
		ag.conversation.AddUser("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}
	// Trip the breaker by pre-failing twice.
	_ = ag.context.Compact(context.Background(), ag.conversation)
	_ = ag.context.Compact(context.Background(), ag.conversation)

	ch, err := ag.Turn(context.Background(), "hello")
	if err != nil {
		// Turn may return error synchronously — that's valid.
		if !errors.Is(err, ErrCompactionExhausted) {
			t.Fatalf("sync error: want ErrCompactionExhausted, got %v", err)
		}
		return
	}
	var last TurnEvent
	for ev := range ch {
		last = ev
	}
	if last.ExitReason != agentsdk.ExitCompactionFailed {
		t.Fatalf("want ExitCompactionFailed, got %v", last.ExitReason)
	}
}
```

- [ ] **Step 8: Run tests**

```
go test ./internal/agent/...
```
Expected: PASS. If pre-existing tests fail because `Compact` now returns an error they ignored, add `_ =` to their call sites — those are structural fixes, not behavioral.

- [ ] **Step 9: Commit**

```bash
git add internal/agent/context.go internal/agent/compaction_breaker_test.go internal/agent/agent.go
git commit -m "[BEHAVIORAL] Add circuit breaker on repeated compaction failure

ContextManager.Compact now returns ErrCompactionExhausted after three
consecutive failures (strategy error or no token reduction). The agent
loop catches this and exits with ExitCompactionFailed instead of
re-attempting compaction every turn forever. Prevents the 'stuck over
context limit burning hundreds of thousands of API calls' failure mode.

Refs: docs/superpowers/plans/2026-04-13-agent-loop-hardening.md Task 3"
```

---

## Task 4: Per-Tool Result Size Budget

**Why:** Ch.5's Layer 0 (`applyToolResultBudget`) enforces per-message size limits on tool results before any compression runs. Rubichan's `shell` tool can currently emit 5 MB of output into a single tool_result; that one result dominates the context window and makes every compaction pass choose between truncating it or leaving everything else. Enforcing a per-tool cap at emission time means the garbage never enters the conversation.

Design: optional extension interface. Tools that care implement `ResultCappedTool`. Tools that don't are exempt (matches Ch.5's "tools without a finite `maxResultSizeChars` are exempted"). Capping is done at `executeSingleTool` boundary, before the result flows to the conversation.

**Files:**
- Create: `pkg/agentsdk/tool_caps.go`
- Create: `internal/agent/result_cap.go`
- Create: `internal/agent/result_cap_test.go`
- Modify: `internal/agent/agent.go` (apply cap in `executeSingleTool`)
- Modify: one concrete tool (`internal/tools/shell/...`) to demonstrate the opt-in

- [ ] **Step 1: Write the failing test**

Create `internal/agent/result_cap_test.go`:

```go
package agent

import (
	"strings"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

type fakeCappedTool struct {
	name string
	cap  int
}

func (t *fakeCappedTool) Name() string                  { return t.name }
func (t *fakeCappedTool) MaxResultBytes() int           { return t.cap }

func TestApplyResultCapBelowCap(t *testing.T) {
	t.Parallel()
	res := agentsdk.ToolResult{Content: "hello"}
	capped := applyResultCap(&fakeCappedTool{name: "x", cap: 100}, res)
	if capped.Content != "hello" {
		t.Fatalf("unexpected content: %q", capped.Content)
	}
}

func TestApplyResultCapAboveCapTruncatesAndMarks(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(&fakeCappedTool{name: "x", cap: 100}, res)
	if len(capped.Content) > 200 {
		t.Fatalf("content not truncated, got %d bytes", len(capped.Content))
	}
	if !strings.Contains(capped.Content, "truncated") {
		t.Fatalf("truncation marker missing from: %q", capped.Content)
	}
	if !strings.HasPrefix(capped.Content, strings.Repeat("a", 50)) {
		t.Fatalf("prefix not preserved")
	}
}

func TestApplyResultCapNilInterfaceIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(nil, res)
	if capped.Content != big {
		t.Fatalf("nil tool should be no-op")
	}
}

func TestApplyResultCapUncappedToolIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	// Tool does not implement ResultCapped — it's a plain Tool.
	capped := applyResultCap(plainTool{}, res)
	if capped.Content != big {
		t.Fatalf("plain tool should be exempt")
	}
}

type plainTool struct{}

func (plainTool) Name() string { return "plain" }
```

- [ ] **Step 2: Run to confirm failure**

```
go test ./internal/agent/ -run TestApplyResultCap -v
```
Expected: compile error — `applyResultCap` and `ResultCapped` interface undefined.

- [ ] **Step 3: Define the interface**

Create `pkg/agentsdk/tool_caps.go`:

```go
package agentsdk

// ResultCapped is an optional extension interface for tools that want
// the agent to truncate their output before it enters the conversation.
//
// Tools that implement this interface report a byte cap; if a result's
// Content exceeds the cap, the agent replaces it with a head+tail slice
// plus a truncation marker. Tools that don't implement this interface
// are exempt — their output flows through unchanged.
//
// Recommended caps:
//   - shell:       64 KB (head + tail of interleaved stdout/stderr)
//   - read_file:   256 KB (large files already have pagination)
//   - grep/search: 64 KB
//   - http_fetch:  128 KB
//
// Claude Code calls this "Layer 0" of context management in query.ts.
// Enforcing size at emission prevents a single tool_result from
// dominating the context window and forcing every subsequent compaction
// pass to trade granular history for one bloated result.
type ResultCapped interface {
	// MaxResultBytes returns the maximum byte length of ToolResult.Content.
	// Return a non-positive value to opt out (treated as exempt).
	MaxResultBytes() int
}
```

- [ ] **Step 4: Implement applyResultCap**

Create `internal/agent/result_cap.go`:

```go
package agent

import (
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// applyResultCap truncates a tool result if the tool implements the
// ResultCapped interface and the content exceeds its declared cap.
// Exempt tools (no interface, or cap <= 0) pass through unchanged.
//
// The truncation strategy preserves both the head (50%) and tail (50%)
// of the content minus space for a truncation marker. This beats head-only
// truncation because tool output often has essential information at both
// ends — shell commands put errors at the tail, file reads put context at
// the head.
func applyResultCap(tool any, res agentsdk.ToolResult) agentsdk.ToolResult {
	capped, ok := tool.(agentsdk.ResultCapped)
	if !ok {
		return res
	}
	max := capped.MaxResultBytes()
	if max <= 0 {
		return res
	}
	if len(res.Content) <= max {
		return res
	}

	marker := fmt.Sprintf("\n\n[... truncated: %d bytes exceeded %d byte cap ...]\n\n",
		len(res.Content), max)
	slice := max - len(marker)
	if slice < 200 {
		// Cap is too small to keep head+tail; just keep head.
		res.Content = res.Content[:max-len(marker)] + marker
		return res
	}
	head := slice / 2
	tail := slice - head
	res.Content = res.Content[:head] + marker + res.Content[len(res.Content)-tail:]
	return res
}
```

- [ ] **Step 5: Run the unit tests**

```
go test ./internal/agent/ -run TestApplyResultCap -v
```
Expected: PASS.

- [ ] **Step 6: Wire cap into executeSingleTool**

Find `executeSingleTool` in `internal/agent/agent.go` (grep for `func (a \*Agent) executeSingleTool`). Locate the point where a `ToolResult` has been returned from the tool and before the result is converted to a `toolExecResult`. Insert:

```go
	// Enforce per-tool result cap (agentsdk.ResultCapped). Exempt tools
	// flow through unchanged.
	if tool != nil {
		result = applyResultCap(tool, result)
	}
```

The exact line depends on the existing body — `tool` is the variable holding the resolved tool instance from the registry, `result` is the `agentsdk.ToolResult`. Add the snippet immediately after the `Execute` call returns and before any conversion to event or result struct.

- [ ] **Step 7: Opt in one real tool as a demonstration**

Find the shell tool. Look for `internal/tools/shell/` and locate the struct that implements `Execute(ctx, input) (ToolResult, error)`. Add:

```go
// MaxResultBytes implements agentsdk.ResultCapped. Shell output is
// frequently large; 64 KB keeps head+tail context while preventing a
// single command from dominating the context window.
func (*Tool) MaxResultBytes() int { return 64 * 1024 }
```

(Replace `Tool` with the actual struct name — likely `shellTool` or `ShellTool`.)

- [ ] **Step 8: Add an integration test**

Add to `result_cap_test.go`:

```go
func TestShellToolReportsCap(t *testing.T) {
	t.Parallel()
	// Create the shell tool via its constructor; any non-nil cap > 0 is fine.
	st := newShellToolForTest(t)
	capped, ok := interface{}(st).(agentsdk.ResultCapped)
	if !ok {
		t.Fatal("shell tool should implement agentsdk.ResultCapped")
	}
	if capped.MaxResultBytes() <= 0 {
		t.Fatal("shell tool cap should be > 0")
	}
}
```

If `newShellToolForTest` doesn't exist, inline the constructor — don't add a new test helper file just for this. If the shell tool's package name differs from what the import path suggests, adjust the import.

- [ ] **Step 9: Run full tests**

```
go test ./internal/agent/... ./pkg/agentsdk/... ./internal/tools/shell/...
```
Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add pkg/agentsdk/tool_caps.go internal/agent/result_cap.go internal/agent/result_cap_test.go internal/agent/agent.go internal/tools/shell/
git commit -m "[BEHAVIORAL] Add per-tool result size cap via ResultCapped interface

Tools that implement agentsdk.ResultCapped report a MaxResultBytes value;
the agent truncates oversize results at emission with a head+tail slice
and a marker. Exempt tools (no interface) pass through unchanged. Shell
tool opts in at 64 KB — previously a chatty command could dump megabytes
into a single tool_result and dominate the context window, forcing every
subsequent compaction to trade granular history for one bloated result.

Refs: docs/superpowers/plans/2026-04-13-agent-loop-hardening.md Task 4"
```

---

## Task 5: Streaming Tool Execution for Concurrency-Safe Tools

**Why:** Today `runLoop` accumulates the entire assistant response (text + tool_use deltas) into `toolInputBuf` and only dispatches tools after `stream` returns EOF at agent.go:1335. Claude Code's `StreamingToolExecutor` dispatches concurrency-safe tools the moment their `tool_use` block finalizes — so by the time the model finishes its trailing text, the `Read` result is already in memory.

For a typical "read file, reason, edit" turn, this is a 1–3 second latency win because the model's post-tool text streams in parallel with disk I/O.

Design:
- New optional marker interface `agentsdk.ConcurrencySafeTool` with method `IsConcurrencySafe() bool`. Read-only tools (`read_file`, `grep`, `glob`, `list_dir`, `code_search`, `http_fetch` GET) implement it; write tools (`write_file`, `patch_file`, `shell`, `edit`) do not.
- New `streamingToolExecutor` type owns an in-flight map keyed by tool_use_id, a results channel, and a `sync.WaitGroup`.
- When `runLoop` finalizes a tool_use block during streaming (existing `finalizeTool` closure at agent.go:1254), it calls `execStream.Dispatch(tool)` if the tool is concurrency-safe, auto-approved, and parallelizable. Non-safe tools are queued via the existing path.
- When the stream ends, `execStream.Drain()` waits for outstanding futures and merges their results into `results []toolExecResult` *in original tool-call order*.
- Tools not streamed are executed by the existing `executeTools` code path. Results are merged by position.
- **Abort semantics**: if `ctx` is cancelled mid-stream, `Drain` waits for in-flight futures but does not dispatch anything new. Any tool that hasn't started gets a synthesized cancellation tool_result via the orphan sweeper from Task 2.

This task depends on Tasks 1 (exit reasons for test assertions) and 2 (orphan sweeper for safe cancellation).

**Files:**
- Modify: `pkg/agentsdk/tool_caps.go` (add `ConcurrencySafeTool` interface, same file as Task 4)
- Create: `internal/agent/stream_tool_exec.go`
- Create: `internal/agent/stream_tool_exec_test.go`
- Modify: `internal/agent/agent.go` (wire executor into `runLoop`)
- Modify: one or two read-only tools to opt in (`read_file`, `grep`)

- [ ] **Step 1: Add the ConcurrencySafeTool interface**

Append to `pkg/agentsdk/tool_caps.go`:

```go
// ConcurrencySafeTool is an optional extension interface. Tools that
// implement it declare themselves safe to execute as soon as their
// tool_use block finalizes during streaming — the agent dispatches them
// without waiting for the full model response.
//
// A tool is concurrency-safe if and only if:
//   1. It has no observable side effects on the filesystem, network,
//      or process state (pure reads).
//   2. Its result depends only on the input and on external state that
//      won't be mutated by another concurrently-dispatched tool.
//   3. Re-ordering it with respect to sibling tool calls in the same
//      response is a no-op as far as the user is concerned.
//
// Tools that return false (or don't implement the interface) are queued
// and executed after the stream completes, in declaration order, via
// the normal executeTools pipeline.
//
// Examples of safe tools: read_file, grep, glob, list_dir, code_search,
// http_get.
//
// Examples of unsafe tools: write_file, patch_file, shell, edit, git,
// database writes, anything with network side effects.
type ConcurrencySafeTool interface {
	IsConcurrencySafe() bool
}
```

- [ ] **Step 2: Write the executor test first**

Create `internal/agent/stream_tool_exec_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

type fakeConcurrencySafeTool struct {
	name       string
	execDelay  time.Duration
	called     atomic.Int32
	returnText string
}

func (t *fakeConcurrencySafeTool) Name() string                 { return t.name }
func (t *fakeConcurrencySafeTool) Description() string          { return "" }
func (t *fakeConcurrencySafeTool) InputSchema() json.RawMessage { return nil }
func (t *fakeConcurrencySafeTool) IsConcurrencySafe() bool      { return true }
func (t *fakeConcurrencySafeTool) Execute(ctx context.Context, _ json.RawMessage) (agentsdk.ToolResult, error) {
	t.called.Add(1)
	select {
	case <-time.After(t.execDelay):
	case <-ctx.Done():
		return agentsdk.ToolResult{}, ctx.Err()
	}
	return agentsdk.ToolResult{Content: t.returnText}, nil
}

func TestStreamingExecutorDispatchesSafeToolsImmediately(t *testing.T) {
	t.Parallel()
	// Two slow concurrency-safe tools. If executed sequentially after
	// stream-end, the turn would take ~200ms. If dispatched during
	// streaming, they run in parallel with wall time ~100ms.
	tool := &fakeConcurrencySafeTool{
		name:       "read_file",
		execDelay:  100 * time.Millisecond,
		returnText: "ok",
	}
	ex := newStreamingToolExecutor(2)
	ctx := context.Background()

	start := time.Now()
	ex.Dispatch(ctx, tool, provider.ToolUseBlock{ID: "a", Name: "read_file", Input: json.RawMessage(`{}`)})
	ex.Dispatch(ctx, tool, provider.ToolUseBlock{ID: "b", Name: "read_file", Input: json.RawMessage(`{}`)})
	results := ex.Drain()
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if elapsed > 180*time.Millisecond {
		t.Fatalf("tools ran sequentially (%v) — expected parallel dispatch", elapsed)
	}
	if tool.called.Load() != 2 {
		t.Fatalf("want 2 invocations, got %d", tool.called.Load())
	}
}

func TestStreamingExecutorPreservesResultOrder(t *testing.T) {
	t.Parallel()
	fast := &fakeConcurrencySafeTool{name: "fast", execDelay: 10 * time.Millisecond, returnText: "fast"}
	slow := &fakeConcurrencySafeTool{name: "slow", execDelay: 80 * time.Millisecond, returnText: "slow"}
	ex := newStreamingToolExecutor(2)
	ctx := context.Background()

	ex.Dispatch(ctx, slow, provider.ToolUseBlock{ID: "first", Name: "slow"})
	ex.Dispatch(ctx, fast, provider.ToolUseBlock{ID: "second", Name: "fast"})
	results := ex.Drain()

	if len(results) != 2 || results[0].toolUseID != "first" || results[1].toolUseID != "second" {
		t.Fatalf("order not preserved: %+v", results)
	}
	if results[0].content != "slow" || results[1].content != "fast" {
		t.Fatalf("content mismatch: %+v", results)
	}
}

func TestStreamingExecutorCancelBlocksNewDispatch(t *testing.T) {
	t.Parallel()
	tool := &fakeConcurrencySafeTool{name: "read_file", execDelay: 50 * time.Millisecond, returnText: "ok"}
	ex := newStreamingToolExecutor(2)
	ctx, cancel := context.WithCancel(context.Background())
	ex.Dispatch(ctx, tool, provider.ToolUseBlock{ID: "a"})
	cancel()
	// Dispatching after cancel should be a no-op (tool still gets a result,
	// but it's an error result synthesized by the executor).
	ex.Dispatch(ctx, tool, provider.ToolUseBlock{ID: "b"})
	results := ex.Drain()
	if len(results) != 2 {
		t.Fatalf("want 2 results (one ok, one cancelled), got %d", len(results))
	}
	// The 'b' dispatch after cancel must be marked as an error.
	var bResult *toolExecResult
	for i := range results {
		if results[i].toolUseID == "b" {
			bResult = &results[i]
		}
	}
	if bResult == nil || !bResult.isError {
		t.Fatalf("dispatch-after-cancel should produce an error result, got %+v", bResult)
	}
}
```

- [ ] **Step 3: Run to confirm failure**

```
go test ./internal/agent/ -run TestStreamingExecutor -v
```
Expected: `newStreamingToolExecutor` undefined.

- [ ] **Step 4: Implement the executor**

Create `internal/agent/stream_tool_exec.go`:

```go
package agent

import (
	"context"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// streamingToolExecutor dispatches concurrency-safe tools as soon as
// their tool_use block finalizes during a streaming model response.
// Results are collected and returned in dispatch order from Drain().
//
// Ordering note: tools are dispatched in stream order (the order the
// model emits tool_use blocks). Drain() returns them in that same
// dispatch order, regardless of execution wall time. This matches the
// protocol requirement that tool_result messages follow their matching
// tool_use in conversation order.
//
// Parallelism is bounded by maxParallel; excess dispatches block in the
// goroutine pool. Callers typically set this to maxParallelTools (8).
type streamingToolExecutor struct {
	sem     chan struct{}
	mu      sync.Mutex
	futures []*toolFuture
	wg      sync.WaitGroup
}

type toolFuture struct {
	toolUseID string
	toolName  string
	done      chan struct{}
	result    toolExecResult
}

func newStreamingToolExecutor(maxParallel int) *streamingToolExecutor {
	if maxParallel < 1 {
		maxParallel = 1
	}
	return &streamingToolExecutor{
		sem: make(chan struct{}, maxParallel),
	}
}

// Dispatch starts execution of a concurrency-safe tool in the background.
// If ctx is already cancelled, the future is completed immediately with
// an error result — the tool is not executed. The caller must still call
// Drain to retrieve the result slot in order.
func (e *streamingToolExecutor) Dispatch(ctx context.Context, tool agentsdk.Tool, tc provider.ToolUseBlock) {
	f := &toolFuture{
		toolUseID: tc.ID,
		toolName:  tc.Name,
		done:      make(chan struct{}),
	}
	e.mu.Lock()
	e.futures = append(e.futures, f)
	e.mu.Unlock()

	if ctx.Err() != nil {
		f.result = toolErrorResult(tc, "context cancelled before tool dispatch")
		close(f.done)
		return
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		// Bound parallelism.
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			f.result = toolErrorResult(tc, "context cancelled while waiting for executor slot")
			close(f.done)
			return
		}
		defer func() { <-e.sem }()

		res, err := tool.Execute(ctx, tc.Input)
		if err != nil {
			f.result = toolErrorResult(tc, err.Error())
			close(f.done)
			return
		}
		res = applyResultCap(tool, res)
		f.result = toolExecResult{
			toolUseID: tc.ID,
			content:   res.Content,
			isError:   res.IsError,
			event:     makeToolResultEvent(tc.ID, tc.Name, res.Content, res.Display(), res.IsError),
		}
		close(f.done)
	}()
}

// Drain waits for every dispatched future to complete and returns
// their results in dispatch order. Safe to call once; subsequent calls
// return an empty slice.
func (e *streamingToolExecutor) Drain() []toolExecResult {
	e.wg.Wait()
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]toolExecResult, 0, len(e.futures))
	for _, f := range e.futures {
		<-f.done
		out = append(out, f.result)
	}
	e.futures = nil
	return out
}

// isStreamingEligible returns true if a tool can be dispatched during
// streaming. Requires the ConcurrencySafeTool marker AND auto-approval.
func isStreamingEligible(tool agentsdk.Tool, result ApprovalResult) bool {
	cs, ok := tool.(agentsdk.ConcurrencySafeTool)
	if !ok || !cs.IsConcurrencySafe() {
		return false
	}
	return result == agentsdk.AutoApproved || result == agentsdk.TrustRuleApproved
}
```

- [ ] **Step 5: Run the tests**

```
go test ./internal/agent/ -run TestStreamingExecutor -v
```
Expected: PASS.

- [ ] **Step 6: Wire the executor into runLoop**

Edit `internal/agent/agent.go`. The goal is to dispatch eligible tools from `finalizeTool` and merge the results in `executeTools`.

First, inside `runLoop` just before the accumulator `var blocks []provider.ContentBlock` at line 1246, add:

```go
		execStream := newStreamingToolExecutor(maxParallelTools)
		streamingDispatched := map[string]bool{}
```

Modify the `finalizeTool` closure at line 1254. After `pendingTools = append(pendingTools, *currentTool)` and before the `blocks = append(...)` line, insert:

```go
			// Dispatch concurrency-safe, auto-approved tools immediately
			// so they run while the model continues streaming post-tool
			// text. The result is collected at stream end.
			if a.approvalChecker != nil {
				approval := a.approvalChecker.CheckApproval(currentTool.Name, currentTool.Input)
				tool, ok := a.tools.Get(currentTool.Name)
				if ok && isStreamingEligible(tool, approval) {
					execStream.Dispatch(ctx, tool, *currentTool)
					streamingDispatched[currentTool.ID] = true
				}
			}
```

(`a.tools.Get` — confirm the exact method name on the registry via `grep -n "func.*Registry.*Get" internal/tools/`. If the method is called something else like `Lookup` or `Tool`, adjust accordingly.)

Then, modify `executeTools` at line 1566 to accept a pre-populated results map from streamed dispatches. The simplest change is a new parameter:

```go
func (a *Agent) executeTools(ctx context.Context, ch chan<- TurnEvent, pendingTools []provider.ToolUseBlock, streamedResults map[string]toolExecResult) bool {
```

At the start of `executeTools`, after the planning step, skip tools already in `streamedResults`:

```go
	// Merge in results that were dispatched during streaming. These tools
	// skip approval/parallelization planning — they already ran.
	results := make([]toolExecResult, len(pendingTools))
	planned := make([]plannedToolCall, 0, len(pendingTools))
	for i, tc := range pendingTools {
		if r, ok := streamedResults[tc.ID]; ok {
			results[i] = r
			// Still emit a tool_call event so the UI sees it.
			ch <- TurnEvent{
				Type: "tool_call",
				ToolCall: &ToolCallEvent{ID: tc.ID, Name: tc.Name, Input: tc.Input},
			}
			continue
		}
		planned = append(planned, plannedToolCall{
			index:          i,
			tc:             tc,
			approvalResult: a.approvalResultForTool(tc),
		})
	}
```

Then replace the existing partition loop to iterate over `planned` instead of `plannedTools` (rename variable). The existing auto-approved / denied / needs-approval partitioning and parallel execution stays as-is. At the end, the existing "emit results in original order" loop still works because we pre-filled `results[i]` for streamed tools.

At the `executeTools` call sites at agent.go:1433 and :1440, pass the drained map:

```go
		streamedResults := map[string]toolExecResult{}
		for _, r := range execStream.Drain() {
			streamedResults[r.toolUseID] = r
		}

		// Check if the model signaled task completion...
		for _, tc := range pendingTools {
			if tc.Name == tools.TaskCompleteName {
				a.executeTools(ctx, ch, pendingTools, streamedResults)
				ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitTaskComplete)
				return
			}
		}

		if cancelled := a.executeTools(ctx, ch, pendingTools, streamedResults); cancelled {
			synthesizeMissingToolResults(a.conversation, "cancelled during tool execution")
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitCancelled)
			return
		}
```

Also: the helper `executePlannedToolsSequential` at :1711 is called only when `approvalChecker == nil`. Pass `nil` as the streamed map and accept it as a parameter:

```go
	return a.executePlannedToolsSequential(ctx, ch, a.planToolCalls(pendingTools), streamedResults)
```

Add an early merge at the top of `executePlannedToolsSequential` matching what `executeTools` does.

- [ ] **Step 7: Opt in read_file and grep**

Find the read_file tool (`grep -rn "func.*Name.*\"read_file\"" internal/tools/`) and add:

```go
// IsConcurrencySafe declares this tool as eligible for streaming dispatch.
// read_file is a pure read with no side effects.
func (*ReadFileTool) IsConcurrencySafe() bool { return true }
```

Do the same for `grep`, `glob`, `list_dir`, and `code_search` if they exist. Each gets one method, ~3 lines. Replace `ReadFileTool` with the actual struct name.

- [ ] **Step 8: Write a runLoop integration test**

Add to `stream_tool_exec_test.go`:

```go
func TestRunLoopStreamsReadFileDuringModelResponse(t *testing.T) {
	t.Parallel()
	// Fake provider that streams: text_delta, tool_use (read_file), 200ms of text_delta.
	// Real read_file would normally take near-zero time; we use a fake that sleeps 100ms
	// during Execute to observe overlap.
	readTool := &fakeConcurrencySafeTool{name: "read_file", execDelay: 100 * time.Millisecond, returnText: "file contents"}
	fp := newStreamingFakeProvider(readTool, 200*time.Millisecond)
	ag := newTestAgentWithProviderAndTool(t, fp, readTool)

	start := time.Now()
	ch, err := ag.Turn(context.Background(), "read it")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	for range ch {
	}
	elapsed := time.Since(start)

	// Without streaming: 200ms stream + 100ms tool = 300ms minimum.
	// With streaming: max(200ms, 100ms) = 200ms. Allow 50ms slack.
	if elapsed > 260*time.Millisecond {
		t.Fatalf("streaming dispatch not effective — elapsed %v", elapsed)
	}
}
```

If `newStreamingFakeProvider` does not exist, implement it inline as a minimal `provider.Provider` whose `Stream` method emits text_delta events, then a tool_use event, then more text_delta events with `time.Sleep` between them, then a stop event.

- [ ] **Step 9: Run tests**

```
go test ./internal/agent/... -race
```
Expected: all PASS, no data races.

- [ ] **Step 10: Run the broader suite**

```
go test ./... -race
```
Expected: all PASS. Any failure in a mode's ACP tests is a wiring issue — the `executeTools` signature changed and every caller needs updating. Fix with `_, _ = ...` only if the caller was previously ignoring the return value (existing pattern).

- [ ] **Step 11: Commit**

```bash
git add pkg/agentsdk/tool_caps.go internal/agent/stream_tool_exec.go internal/agent/stream_tool_exec_test.go internal/agent/agent.go internal/tools/
git commit -m "[BEHAVIORAL] Dispatch concurrency-safe tools during streaming

Tools implementing agentsdk.ConcurrencySafeTool are dispatched the moment
their tool_use block finalizes during a streaming response, running in
parallel with the remaining post-tool text. read_file, grep, glob,
list_dir, and code_search opt in. Write/shell tools remain queued until
stream end. For turns that read a file before reasoning, the file is
already in memory by the time the model finishes its preamble — typical
latency win 1–3 seconds on large-context turns.

Refs: docs/superpowers/plans/2026-04-13-agent-loop-hardening.md Task 5"
```

---

## Verification

After all five tasks, run:

```
go test ./... -race
golangci-lint run ./...
gofmt -l .
```

Expected: all PASS, no output from `gofmt -l`, no lint errors.

Check coverage on touched files:

```
go test ./internal/agent/ ./pkg/agentsdk/ -cover
```

Expected: coverage >= 90% per the project standard.

## Rollout Notes

**Backward compatibility:** All five tasks are additive. `TurnExitReason` adds an enum field (zero value = `ExitUnknown`, which is a bug signal). The orphan sweeper only appends messages — it never modifies or drops. The circuit breaker only trips on repeated failures; healthy sessions are unaffected. `ResultCapped` and `ConcurrencySafeTool` are opt-in extension interfaces; tools that don't implement them are exempt.

**Observability:** After Task 1, consumers should log `event.ExitReason.String()` on every `done` event. That alone will reveal how often the production loop hits `ExitNoProgress`, `ExitEmptyResponse`, or (newly) `ExitCompactionFailed` — useful signal that the old untyped done event was hiding.

**What's deliberately not in this plan:**
- Model fallback and thinking-signature stripping (requires multi-provider coordination)
- Withholding pattern for recoverable errors (requires a buffered error queue inside the loop)
- Haiku background tool-use summarization (requires a dedicated summarizer provider config)
- Token budget with diminishing-returns detection (requires a new `TokenBudget` config type)
- Abort type distinction (hard abort vs submit-interrupt) — requires TUI-side changes too

Each of these is a separate plan. Tasks 1–3 are prerequisites for any of them.
