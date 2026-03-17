# Context Inspector — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-17 · **Status:** Approved
> **Milestone:** 6, Phase 2
> **Parent:** [Spec Amendments](2026-03-16-spec-amendments-design.md), Amendment 5
> **FRs:** FR-1.14, FR-1.15

---

## Overview

Add `/context` and `/compact` slash commands for inspecting context window usage and triggering manual compaction. Additionally, attach the `ContextBudget` breakdown to every "done" `TurnEvent` so all consumers (TUI, headless, external) get context metrics after each turn.

## Existing Infrastructure

The codebase already provides:
- **`ContextManager`** (`internal/agent/context.go`) — tracks token budget, runs compaction strategies
- **`ContextBudget`** (`pkg/agentsdk/compaction.go`) — struct with per-component breakdown (SystemPrompt, SkillPrompts, ToolDescriptions, Conversation) plus `EffectiveWindow()`, `UsedTokens()`, `RemainingTokens()`, `UsedPercentage()`
- **`ForceCompact()`** — runs strategy chain (tool clearing → summarization → truncation), returns `CompactResult` with before/after metrics
- **`CompactContextTool`** (`internal/tools/compact_context.go`) — LLM-callable compaction tool
- **Up to three compaction strategies** — tool result clearing and truncation are always present; summarization is added when a `Summarizer` is configured via `WithSummarizer()`

This feature is primarily wiring — exposing existing functionality through new interfaces.

## Components

### 1. Public Agent Methods (`internal/agent/agent.go`)

Two new methods on `*Agent`:

```go
// ContextBudget returns the current context usage breakdown.
func (a *Agent) ContextBudget() agentsdk.ContextBudget {
    return a.context.Budget()
}

// ForceCompact triggers manual compaction and returns before/after metrics.
// The error return is always nil currently (ForceCompact internally handles
// strategy failures gracefully). Reserved for future use when strategies may
// return actionable errors.
func (a *Agent) ForceCompact(ctx context.Context) (agentsdk.CompactResult, error) {
    result := a.context.ForceCompact(ctx, a.conversation)
    return result, nil
}
```

These are thin wrappers. No new logic — just public exposure of private fields.

### 2. TurnEvent Context Budget (`pkg/agentsdk/events.go`)

Add a `ContextBudget` pointer field to `TurnEvent`:

```go
type TurnEvent struct {
    // ... existing fields ...
    ContextBudget *ContextBudget  // populated on "done" events only; nil otherwise
}
```

In `makeDoneEvent()` (`internal/agent/agent.go`), populate the field:

```go
func (a *Agent) makeDoneEvent(inputTokens, outputTokens int) TurnEvent {
    // ... existing code ...
    budget := a.context.Budget()
    return TurnEvent{
        Type:          "done",
        InputTokens:   inputTokens,
        OutputTokens:  outputTokens,
        DiffSummary:   diffSummary,
        ContextBudget: &budget,
    }
}
```

This gives all TurnEvent consumers (TUI, headless runner, external integrations) the per-component context breakdown after every turn without additional API calls.

### 3. Slash Commands (`internal/commands/context.go`)

#### `/context` Command

Displays the current context window usage breakdown with a visual bar chart.

```go
type contextCommand struct {
    getBudget func() agentsdk.ContextBudget
}

func NewContextCommand(getBudget func() agentsdk.ContextBudget) SlashCommand
```

**Output format:**

```
Context Usage: 42,150 / 95,904 tokens (44%)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  System prompt      2,100 (  2%)  ██
  Skill prompts      3,400 (  4%)  ███
  Tool definitions   4,200 (  4%)  ████
  Conversation      32,450 ( 34%)  ██████████████████████████████████
  Remaining         53,754 ( 56%)  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

The bar width is proportional to the percentage. Each component gets a line. `Remaining` is shown with a different fill character.

**Edge cases:**
- If `getBudget` is nil, output: "Context inspection not available."
- If `EffectiveWindow()` is 0, avoid division by zero.

#### `/compact` Command

Triggers manual compaction and displays before/after metrics.

```go
type compactCommand struct {
    forceCompact func(ctx context.Context) (agentsdk.CompactResult, error)
}

func NewCompactCommand(forceCompact func(ctx context.Context) (agentsdk.CompactResult, error)) SlashCommand
```

**Output format:**

```
Compacted: 42,150 → 28,300 tokens (33% reduction)
Messages: 45 → 22
Strategies: tool_result_clearing, truncation
```

**Edge cases:**
- If `forceCompact` is nil, output: "Compaction not available."
- If no reduction occurred (before == after), output: "No compaction needed — context is within budget."

### 4. Command Registration

In the TUI model initialization (where `/undo`, `/rewind`, etc. are registered), register the new commands:

```go
// After agent is set on the model:
registry.Register(commands.NewContextCommand(func() agentsdk.ContextBudget {
    return agent.ContextBudget()
}))
registry.Register(commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
    return agent.ForceCompact(ctx)
}))
```

This follows the callback injection pattern used by existing commands (e.g., `NewClearCommand(onClear)`, `NewModelCommand(onSwitch)`, `NewDebugVerificationSnapshotCommand(getSnapshot)`). Note: `/undo` and `/rewind` use direct struct injection instead, but the callback approach is more appropriate here since it avoids importing the `agent` package into `commands`.

## Scope Exclusions

- **No focus-directed compaction** — deferred until LLM summarizer is wired in
- **No status bar changes** — `/context` is sufficient for on-demand inspection
- **No configuration changes** — compaction thresholds already configurable
- **No headless-specific formatting** — headless consumers can read `TurnEvent.ContextBudget` directly

## File Summary

| File | Package | Change |
|------|---------|--------|
| `internal/agent/agent.go` | `agent` | Add `ContextBudget()`, `ForceCompact()` methods; populate `ContextBudget` in `makeDoneEvent()` |
| `internal/agent/agent_test.go` | `agent` | Tests for new methods and TurnEvent field |
| `pkg/agentsdk/events.go` | `agentsdk` | Add `ContextBudget *ContextBudget` field to `TurnEvent` |
| `internal/commands/context.go` | `commands` | `/context` and `/compact` slash commands with formatting |
| `internal/commands/context_test.go` | `commands` | Tests for both commands including edge cases |

## Dependencies

- No new external dependencies.
- `internal/commands/context.go` imports `pkg/agentsdk` for `ContextBudget` and `CompactResult` types.
- All other dependencies already exist in the import graph.
