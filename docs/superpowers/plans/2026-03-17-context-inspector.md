# Context Inspector Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `/context` and `/compact` slash commands plus ContextBudget in TurnEvent done events.

**Architecture:** Expose existing `ContextManager.Budget()` and `ForceCompact()` through public Agent methods, new slash commands, and a `ContextBudget` field on TurnEvent. Primarily wiring — no new logic.

**Tech Stack:** Go stdlib, existing `agentsdk.ContextBudget`/`CompactResult` types, existing `commands.SlashCommand` interface.

**Spec:** `docs/superpowers/specs/2026-03-17-context-inspector-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `pkg/agentsdk/events.go` | `agentsdk` | Add `ContextBudget` field to `TurnEvent` |
| `internal/agent/agent.go` | `agent` | Add `ContextBudget()`, `ForceCompact()` methods; populate in `makeDoneEvent()` |
| `internal/agent/agent_test.go` | `agent` | Tests for new methods and TurnEvent field |
| `internal/commands/context.go` | `commands` | `/context` and `/compact` commands with formatting |
| `internal/commands/context_test.go` | `commands` | Tests for both commands |

---

## Chunk 1: TurnEvent + Agent Methods + Commands

### Task 1: Add ContextBudget to TurnEvent

**Files:**
- Modify: `pkg/agentsdk/events.go:6-20`

- [ ] **Step 1: Add field to TurnEvent**

In `pkg/agentsdk/events.go`, add after the `SubagentResult` field (line 19):

```go
ContextBudget  *ContextBudget   // populated for done events: per-component context usage breakdown
```

- [ ] **Step 2: Verify build**

Run: `go build ./pkg/agentsdk/`
Expected: PASS (no tests needed — this is a struct field addition)

- [ ] **Step 3: Commit**

```
[BEHAVIORAL] add ContextBudget field to TurnEvent done events
```

---

### Task 2: Add Agent public methods and wire makeDoneEvent

**Files:**
- Modify: `internal/agent/agent.go:882-894` (makeDoneEvent)
- Modify: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestAgentContextBudget(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}
	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "ok"},
		{Type: "stop"},
	}}

	t.Run("returns budget", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)
		budget := a.ContextBudget()
		assert.Equal(t, 100000, budget.Total)
	})

	t.Run("force compact returns result", func(t *testing.T) {
		a := New(mp, tools.NewRegistry(), autoApprove, cfg)
		result, err := a.ForceCompact(context.Background())
		require.NoError(t, err)
		// Empty conversation — nothing to compact
		assert.Equal(t, 0, result.BeforeTokens)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentContextBudget -v`
Expected: FAIL — `ContextBudget` and `ForceCompact` not defined on Agent

- [ ] **Step 3: Write implementation**

Add to `internal/agent/agent.go` (near other public accessor methods like `DiffTracker()`, `Checkpoints()`):

```go
// ContextBudget returns the current context usage breakdown.
func (a *Agent) ContextBudget() agentsdk.ContextBudget {
	return a.context.Budget()
}

// ForceCompact triggers manual compaction and returns before/after metrics.
// The error return is always nil currently — reserved for future strategy errors.
func (a *Agent) ForceCompact(ctx context.Context) (agentsdk.CompactResult, error) {
	result := a.context.ForceCompact(ctx, a.conversation)
	return result, nil
}
```

Update `makeDoneEvent()` (line ~884) to attach the budget:

```go
func (a *Agent) makeDoneEvent(inputTokens, outputTokens int) TurnEvent {
	budget := a.context.Budget()
	event := TurnEvent{
		Type:          "done",
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		ContextBudget: &budget,
	}
	if a.diffTracker != nil {
		event.DiffSummary = a.diffTracker.Summarize()
	}
	return event
}
```

Note: Create the budget before the struct literal, take its address. Keep the existing DiffTracker conditional logic.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentContextBudget -v`
Expected: PASS

- [ ] **Step 5: Run full agent tests**

Run: `go test ./internal/agent/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add Agent ContextBudget/ForceCompact methods and wire TurnEvent
```

---

### Task 3: /context slash command

**Files:**
- Create: `internal/commands/context.go`
- Create: `internal/commands/context_test.go`

- [ ] **Step 1: Write failing tests**

```go
package commands_test

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextCommand(t *testing.T) {
	budget := agentsdk.ContextBudget{
		Total:            100000,
		MaxOutputTokens:  4096,
		SystemPrompt:     2100,
		SkillPrompts:     3400,
		ToolDescriptions: 4200,
		Conversation:     32450,
	}

	cmd := commands.NewContextCommand(func() agentsdk.ContextBudget { return budget })
	assert.Equal(t, "context", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Context Usage:")
	assert.Contains(t, result.Output, "System prompt")
	assert.Contains(t, result.Output, "Skill prompts")
	assert.Contains(t, result.Output, "Tool definitions")
	assert.Contains(t, result.Output, "Conversation")
	assert.Contains(t, result.Output, "Remaining")
}

func TestContextCommandNilCallback(t *testing.T) {
	cmd := commands.NewContextCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}

func TestContextCommandZeroWindow(t *testing.T) {
	budget := agentsdk.ContextBudget{
		Total:           0,
		MaxOutputTokens: 0,
	}
	cmd := commands.NewContextCommand(func() agentsdk.ContextBudget { return budget })
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	// Should not panic on zero effective window
	assert.Contains(t, result.Output, "Context Usage:")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/ -run TestContextCommand -v`
Expected: FAIL — `NewContextCommand` not defined

- [ ] **Step 3: Write implementation**

```go
package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// --- context ---

type contextCommand struct {
	getBudget func() agentsdk.ContextBudget
}

// NewContextCommand creates a command that displays the context window usage breakdown.
func NewContextCommand(getBudget func() agentsdk.ContextBudget) SlashCommand {
	return &contextCommand{getBudget: getBudget}
}

func (c *contextCommand) Name() string        { return "context" }
func (c *contextCommand) Description() string { return "Show context window usage" }
func (c *contextCommand) Arguments() []ArgumentDef { return nil }
func (c *contextCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *contextCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.getBudget == nil {
		return Result{Output: "Context inspection not available."}, nil
	}

	b := c.getBudget()
	ew := b.EffectiveWindow()
	used := b.UsedTokens()
	remaining := b.RemainingTokens()
	if remaining < 0 {
		remaining = 0
	}

	pct := 0
	if ew > 0 {
		pct = int(b.UsedPercentage() * 100)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Context Usage: %s / %s tokens (%d%%)\n", formatNum(used), formatNum(ew), pct)
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	maxBar := 40
	writeRow := func(label string, tokens int) {
		p := 0
		if ew > 0 {
			p = tokens * 100 / ew
		}
		barLen := tokens * maxBar / max(ew, 1)
		if barLen < 0 {
			barLen = 0
		}
		bar := strings.Repeat("█", barLen)
		fmt.Fprintf(&sb, "  %-18s %6s (%3d%%)  %s\n", label, formatNum(tokens), p, bar)
	}

	writeRow("System prompt", b.SystemPrompt)
	writeRow("Skill prompts", b.SkillPrompts)
	writeRow("Tool definitions", b.ToolDescriptions)
	writeRow("Conversation", b.Conversation)

	// Remaining with different fill
	remPct := 0
	if ew > 0 {
		remPct = remaining * 100 / ew
	}
	remBar := strings.Repeat("░", remaining*maxBar/max(ew, 1))
	fmt.Fprintf(&sb, "  %-18s %6s (%3d%%)  %s\n", "Remaining", formatNum(remaining), remPct, remBar)

	return Result{Output: sb.String()}, nil
}

func formatNum(n int) string {
	if n < 0 {
		return fmt.Sprintf("-%s", formatNum(-n))
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%s,%03d", formatNum(n/1000), n%1000)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/commands/ -run TestContextCommand -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add /context slash command with usage breakdown
```

---

### Task 4: /compact slash command

**Files:**
- Modify: `internal/commands/context.go` (add to same file)
- Modify: `internal/commands/context_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestCompactCommand(t *testing.T) {
	result := agentsdk.CompactResult{
		BeforeTokens:   42150,
		AfterTokens:    28300,
		BeforeMsgCount: 45,
		AfterMsgCount:  22,
		StrategiesRun:  []string{"tool_result_clearing", "truncation"},
	}

	cmd := commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
		return result, nil
	})
	assert.Equal(t, "compact", cmd.Name())

	res, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "42,150")
	assert.Contains(t, res.Output, "28,300")
	assert.Contains(t, res.Output, "33%")
	assert.Contains(t, res.Output, "45")
	assert.Contains(t, res.Output, "22")
	assert.Contains(t, res.Output, "tool_result_clearing")
}

func TestCompactCommandNoReduction(t *testing.T) {
	result := agentsdk.CompactResult{
		BeforeTokens:   1000,
		AfterTokens:    1000,
		BeforeMsgCount: 5,
		AfterMsgCount:  5,
	}
	cmd := commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
		return result, nil
	})

	res, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "No compaction needed")
}

func TestCompactCommandNilCallback(t *testing.T) {
	cmd := commands.NewCompactCommand(nil)
	res, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "not available")
}

func TestCompactCommandError(t *testing.T) {
	cmd := commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
		return agentsdk.CompactResult{}, fmt.Errorf("compaction failed")
	})

	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}
```

Note: Add `"fmt"` to test imports for the error test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/ -run TestCompactCommand -v`
Expected: FAIL — `NewCompactCommand` not defined

- [ ] **Step 3: Write implementation**

Add to `internal/commands/context.go`:

```go
// --- compact ---

type compactCommand struct {
	forceCompact func(ctx context.Context) (agentsdk.CompactResult, error)
}

// NewCompactCommand creates a command that triggers manual context compaction.
func NewCompactCommand(forceCompact func(ctx context.Context) (agentsdk.CompactResult, error)) SlashCommand {
	return &compactCommand{forceCompact: forceCompact}
}

func (c *compactCommand) Name() string        { return "compact" }
func (c *compactCommand) Description() string { return "Compact the context window" }
func (c *compactCommand) Arguments() []ArgumentDef { return nil }
func (c *compactCommand) Complete(_ context.Context, _ []string) []Candidate { return nil }

func (c *compactCommand) Execute(ctx context.Context, _ []string) (Result, error) {
	if c.forceCompact == nil {
		return Result{Output: "Compaction not available."}, nil
	}

	result, err := c.forceCompact(ctx)
	if err != nil {
		return Result{}, err
	}

	if result.BeforeTokens == result.AfterTokens {
		return Result{Output: "No compaction needed — context is within budget."}, nil
	}

	reduction := 0
	if result.BeforeTokens > 0 {
		reduction = (result.BeforeTokens - result.AfterTokens) * 100 / result.BeforeTokens
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Compacted: %s → %s tokens (%d%% reduction)\n",
		formatNum(result.BeforeTokens), formatNum(result.AfterTokens), reduction)
	fmt.Fprintf(&sb, "Messages: %d → %d\n", result.BeforeMsgCount, result.AfterMsgCount)
	if len(result.StrategiesRun) > 0 {
		fmt.Fprintf(&sb, "Strategies: %s\n", strings.Join(result.StrategiesRun, ", "))
	}

	return Result{Output: sb.String()}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ -run TestCompactCommand -v`
Expected: PASS

- [ ] **Step 5: Run all commands tests**

Run: `go test ./internal/commands/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add /compact slash command with compaction metrics
```

---

### Task 5: Final integration — full test suite + lint

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Check formatting**

Run: `gofmt -l .`
Expected: No files listed

- [ ] **Step 3: Check coverage for new commands**

Run: `go test -cover ./internal/commands/`
Expected: Reasonable coverage (commands are simple)

- [ ] **Step 4: Verify TurnEvent field works end-to-end**

Run: `go test ./internal/agent/ -run TestAgentContextBudget -v`
Expected: PASS

- [ ] **Step 5: Commit any final fixes**

```
[STRUCTURAL] fix lint and formatting issues
```
