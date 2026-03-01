# Context Management Enhancements — Design Document

**Date:** 2026-03-01
**Status:** Approved
**Approach:** Extend existing ContextManager (Approach A)
**Reference:** Claude Code context management research (deepwiki.com/anthropics/claude-code/3.3)

## Motivation

Our existing context management has a solid `CompactionStrategy` chain (tool clearing → LLM summarization → truncation) with `ConversationSignals` for dynamic adjustment. However, research into Claude Code's battle-tested implementation reveals 8 gaps that limit context efficiency, session resilience, and multi-provider cost optimization.

This design addresses all 8 enhancements as composable layers around the existing `ContextManager`, each independently testable and shippable as a separate PR.

---

## Enhancement 1: Component-Level Token Budget

**Problem:** Single `budget int` provides no visibility into *what* consumes context.

**Solution:** New `ContextBudget` struct replaces the scalar budget.

```go
// ContextBudget tracks token usage by component category.
type ContextBudget struct {
    Total            int // configured max (from config.Agent.ContextBudget)
    MaxOutputTokens  int // reserved for LLM response (e.g., 4096)

    // Measured usage (updated before each LLM call):
    SystemPrompt     int // base prompt + AGENT.md + memories
    SkillPrompts     int // active skill prompt fragments
    ToolDescriptions int // tool defs sent to LLM (grows with MCP/skills)
    Conversation     int // messages + tool results
}

func (b *ContextBudget) EffectiveWindow() int     // Total - MaxOutputTokens
func (b *ContextBudget) UsedTokens() int           // sum of all components
func (b *ContextBudget) RemainingTokens() int      // EffectiveWindow - UsedTokens
func (b *ContextBudget) UsedPercentage() float64   // UsedTokens / EffectiveWindow
```

**Changes to ContextManager:**
- Replace `budget int` with `budget ContextBudget`
- Add `MeasureUsage(conv, systemPrompt, toolDefs)` — populates all component fields
- `ShouldCompact` and `ExceedsBudget` use `EffectiveWindow()` instead of raw budget

**Files:** `internal/agent/context.go`, `internal/agent/context_test.go`

---

## Enhancement 2: Large Tool Result Disk Offloading

**Problem:** A single `git log` or build output can consume 30%+ of context.

**Solution:** `ResultStore` offloads large tool results to SQLite, keeping compact references in context. A new `read_result` tool lets the LLM retrieve portions on demand.

**New file:** `internal/agent/resultstore.go`

```go
type ResultStore struct {
    store     *store.Store
    sessionID string
    threshold int // bytes; default 4096
}

func (rs *ResultStore) OffloadResult(toolName, toolUseID, content string) (string, error)
func (rs *ResultStore) Retrieve(refID string) (string, error)
```

**New SQLite table:**

```sql
CREATE TABLE IF NOT EXISTS tool_result_blobs (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tool_name  TEXT NOT NULL,
    content    BLOB NOT NULL,
    byte_size  INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_blobs_session ON tool_result_blobs(session_id);
```

**Context replacement format:**

```
[Tool result stored — 48,231 bytes from "shell_exec".
First 200 chars: go: downloading github.com/stretchr/testify v1.9.0...]
Use the "read_result" tool with ref_id="abc-123" to read specific portions.
```

**New tool:** `ReadResultTool` — input `{ "ref_id": "...", "offset": 0, "limit": 4096 }`

**Integration point:** After `tool.Execute()` in `executeSingleTool`, before adding to conversation.

**Files:** `internal/agent/resultstore.go`, `internal/agent/resultstore_test.go`, `internal/tools/read_result.go`, `internal/tools/read_result_test.go`, `internal/store/store.go` (new table)

---

## Enhancement 3: Compaction Threshold & Output Token Reservation

**Problem:** Current `triggerRatio: 0.7` wastes 30% of context as unused headroom. No output token reservation risks API errors.

**Solution:** Two-tier threshold with output token reservation.

```
effective_window = Total - MaxOutputTokens
compact_at       = 0.95 * effective_window
block_at         = 0.98 * effective_window
```

**Changes to ContextManager:**

```go
type ContextManager struct {
    budget         ContextBudget
    compactTrigger float64 // 0.95
    hardBlock      float64 // 0.98
    strategies     []CompactionStrategy
}
```

**New method:** `IsBlocked()` — returns true at 98%. Agent loop uses this to force compaction before accepting the next user message.

**Migration:**
- `ShouldCompact`: `0.7 * budget` → `0.95 * EffectiveWindow()`
- New `IsBlocked`: `0.98 * EffectiveWindow()`
- `ExceedsBudget`: `UsedTokens() > EffectiveWindow()` (true overflow)

**Files:** `internal/agent/context.go`, `internal/agent/context_test.go`

---

## Enhancement 4: Prompt Caching Architecture

**Problem:** `buildSystemPrompt()` is monolithic string concatenation. Dynamic content at any position invalidates provider-level caching.

**Solution:** `PromptBuilder` assembles sections in cacheable-first order with explicit breakpoint hints.

**New file:** `internal/agent/prompt.go`

```go
type PromptSection struct {
    Name      string
    Content   string
    Cacheable bool
}

type PromptBuilder struct {
    sections []PromptSection
}

func (pb *PromptBuilder) Build() (prompt string, breakpoints []int)
```

**Section ordering (static → dynamic):**

| Order | Section | Cacheable | Changes when? |
|-------|---------|-----------|---------------|
| 1 | Base system instructions | Yes | Never |
| 2 | AGENT.md project rules | Yes | On file edit |
| 3 | Cross-session memories | Yes | On session start |
| 4 | Active skill prompt fragments | Sometimes | On skill activate/deactivate |
| 5 | Scratchpad notes | No | Every turn potentially |
| 6 | Context usage status | No | Every turn |

Cache breakpoints inserted after sections 1–3.

**Provider-level support:** New `CacheBreakpoints []int` field on `CompletionRequest`.
- Anthropic: translates to `cache_control` blocks
- OpenAI: ignores (auto-caches by prefix)
- Ollama: ignores

**Files:** `internal/agent/prompt.go`, `internal/agent/prompt_test.go`, `internal/provider/types.go`, `internal/provider/anthropic/` (cache_control mapping)

---

## Enhancement 5: MCP Tool Description Deferral

**Problem:** Many MCP servers can push tool descriptions past 10% of context.

**Solution:** `DeferralManager` holds back tool descriptions that exceed a budget threshold. A `tool_search` meta-tool lets the LLM discover deferred tools.

**New file:** `internal/tools/deferral.go`

```go
type DeferralManager struct {
    registry           *Registry
    budgetThresholdPct float64          // default 0.10
    deferredTools      map[string]ToolDef
}

func (dm *DeferralManager) SelectForContext(msgs []provider.Message, effectiveWindow int) (active []provider.ToolDef, deferredCount int)
func (dm *DeferralManager) Search(query string) []ToolDef
```

**Prioritization (what gets deferred first):**
1. Built-in tools — never deferred
2. MCP tools — deferred first (largest schemas, external)
3. Skill-registered tools — deferred second
4. Within category: LRU — least recently used deferred first

**New tool:** `ToolSearchTool` — input `{ "query": "xcode build" }`. Returns matching names + descriptions + schemas. Matched tools promoted to active set for current turn.

**System note when tools are deferred:**
```
[N tools deferred to save context. Use tool_search to find them.]
```

**Files:** `internal/tools/deferral.go`, `internal/tools/deferral_test.go`, `internal/tools/tool_search.go`, `internal/tools/tool_search_test.go`

---

## Enhancement 6: Compacted Session Resume

**Problem:** Resuming a previously-compacted session loads full pre-compaction history, immediately re-exceeding the context limit.

**Solution:** Dual-write pattern — full transcript for audit, compacted snapshot for resume.

**New SQLite table:**

```sql
CREATE TABLE IF NOT EXISTS session_snapshots (
    session_id  TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    messages    TEXT NOT NULL,
    token_count INTEGER NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

**New store methods:**

```go
func (s *Store) SaveSnapshot(sessionID string, msgs []provider.Message, tokenCount int) error
func (s *Store) GetSnapshot(sessionID string) ([]provider.Message, error)
```

**Resume logic change in `agent.go` `New()`:** Prefer snapshot if available, fall back to full message history for never-compacted sessions.

**Snapshot saved:** After every successful compaction that actually reduces messages.

**Files:** `internal/store/store.go` (new table + methods), `internal/store/store_test.go`, `internal/agent/agent.go` (resume logic)

---

## Enhancement 7: `/compact` Command + `compact_context` Tool

**Problem:** No way to proactively compress context before large tasks. Headless mode has no user to trigger compaction manually.

**Solution:** Two entry points for the same operation.

**New public method on Agent:**

```go
func (a *Agent) ForceCompact(ctx context.Context) CompactResult

type CompactResult struct {
    BeforeTokens   int
    AfterTokens    int
    BeforeMsgCount int
    AfterMsgCount  int
    StrategiesRun  []string
}
```

**Slash command:** `/compact` in interactive TUI → calls `agent.ForceCompact()` → displays result.

**Built-in tool:** `CompactContextTool` — input `{}` → calls `agent.ForceCompact()` → returns summary string. Available in all modes.

**Context awareness:** When usage exceeds 80%, inject a non-cacheable note into the system prompt:
```
[Context: 83% used. You can call compact_context to free space before continuing with large operations.]
```

**Files:** `internal/agent/agent.go` (ForceCompact), `internal/tools/compact_context.go`, `internal/tools/compact_context_test.go`, `internal/tui/` (slash command handler)

---

## Enhancement 8: Integration Wiring

**Modified `agent.go` `New()` initialization** adds: `promptBuilder`, `resultStore`, `deferral`.

**Modified `runLoop` per-turn flow:**

```
1. Build system prompt via PromptBuilder           (Section 4)
2. Measure component usage via ContextBudget       (Section 1)
3. Check IsBlocked (98%) → ForceCompact + snapshot (Sections 3, 6, 7)
4. Check ShouldCompact (95%) → chain + snapshot    (Sections 3, 6)
5. Select tools via DeferralManager                (Section 5)
6. Build CompletionRequest with breakpoints        (Section 4)
7. Stream LLM response
8. Execute tools → offload large results           (Section 2)
9. Persist messages + update snapshot if compacted
```

**New config fields** (`config.Agent`):

```toml
[agent]
context_budget = 100000
max_output_tokens = 4096
compact_trigger = 0.95
hard_block = 0.98
result_offload_threshold = 4096
tool_deferral_threshold = 0.10
```

All new fields have sensible defaults. Existing configs work unchanged.

**Graceful degradation:** Every subsystem falls back to pre-enhancement behavior on failure:
- ResultStore fails → results stay inline
- DeferralManager fails → all tools loaded
- PromptBuilder fails → string concatenation fallback
- Snapshot save fails → resume loads full history

---

## PR Sequence

Each enhancement is an independent PR, ordered by dependency:

| PR | Enhancement | Depends on | Estimated effort |
|----|-------------|-----------|-----------------|
| 1 | Component-level budget (Section 1) + Threshold (Section 3) | None | 1 day |
| 2 | Prompt caching (Section 4) | PR 1 (uses ContextBudget) | 1.5 days |
| 3 | Result offloading (Section 2) | PR 1 (uses EffectiveWindow) | 2 days |
| 4 | Tool deferral (Section 5) | PR 1 (uses EffectiveWindow) | 1.5 days |
| 5 | Compacted session resume (Section 6) | None | 1 day |
| 6 | /compact + compact_context (Section 7) | PR 1 | 1 day |
| 7 | Integration wiring (Section 8) | PRs 1–6 | 1 day |

**Total estimated effort:** ~9 days

PRs 2, 3, 4 can proceed in parallel after PR 1 merges. PR 5 has no dependencies and can start immediately.
