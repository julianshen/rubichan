# Query Loop Improvements: Plan Index

> **Goal:** Bring rubichan's query loop to parity with Claude Code (ccgo) on error recovery, context management, and resilience.

## Dependency Graph

```
Plan A: Error Classifier          (no deps, structural)
  ↓
Plan B: Reactive Compaction        (depends on A)
  ↓
Plan C: Max Tokens Recovery        (depends on A)
  ↓
Plan D: LoopState Extraction       (no deps, structural)
  ↓
Plan E: Generation Counter         (no deps, structural)
  ↓
Plan F: Model Fallback             (depends on A)
```

Plans D and E can be executed in any order. Plans B, C, F depend on A.

## Plan Files

| # | Plan | File | Est. Tasks |
|---|------|------|-----------|
| A | Error Classifier | `2026-04-27-query-loop-error-classifier.md` | 2 |
| B | Reactive Compaction | `2026-04-27-query-loop-reactive-compaction.md` | 3 |
| C | Max Tokens Recovery | `2026-04-27-query-loop-max-tokens-recovery.md` | 1 |
| D | LoopState Extraction | `2026-04-27-query-loop-loopstate.md` | 2 |
| E | Generation Counter | `2026-04-27-query-loop-generation-counter.md` | 1 |
| F | Model Fallback | `2026-04-27-query-loop-model-fallback.md` | 2 |

## Recommended Execution Order

1. **A** (error classifier) — foundation for B, C, F
2. **D** (loopState) — structural, enables cleaner integration of B/C
3. **B** (reactive compaction) — highest impact reliability fix
4. **C** (max tokens recovery) — prevents truncated responses
5. **E** (generation counter) — prevents stale cleanup corruption
6. **F** (model fallback) — resilience against provider overload

## Completed (since index created)

| # | Plan | PR | Description |
|---|------|-----|-------------|
| — | Retry jitter | #250 | 0-25% jitter in TurnRetry and DoWithRetry |
| — | Diminishing returns | #251 | Exit when 4+ turns produce <500 output tokens |
| — | Budget nudge | #252 | Inject budget awareness message at 70-95% usage |
| — | ContinueReason | #253 | Structured enum for loop observability |
| — | Review fixes | #254 | Nudge dedup, negative delta clamp, ContinueReason logging |
| — | Slot reservation | #255 | 8k→64k max_output_tokens escalation |
| — | Head/tail snip | #256 | Preserve first 1/3 + last 2/3 during compaction |
| — | InputConcurrencySafe | #257 | Per-invocation concurrency safety check |

## Completed (all query loop improvements done)

| # | Plan | PR | Description |
|---|------|-----|-------------|
| — | Write-barrier executor | #259 | Streaming executor with Barrier primitive |
| — | Error withholding | #263 | Multi-stage recovery with withheld error buffer |
| — | Permission modes | #264 | plan, auto, fullAuto, bypass modes |

## Tool System Improvements (from ccgo/Claude Code research)

| # | Plan | File | Priority | Description |
|---|------|------|----------|-------------|
| T1 | Tool Batching | `2026-05-02-tool-batching.md` | High | `partitionToolCalls` algorithm: group adjacent safe tools, parallelize safe batches, serialize unsafe |
| T2 | Per-Tool Result Budgets | `2026-05-02-per-tool-result-budgets.md` | High | Per-tool `MaxResultChars()` + aggregate 200K/msg budget enforcement |
| T3 | File Read Caching | `2026-05-02-file-read-caching.md` | High | `FileReadCache` with mtime/size invalidation, avoids redundant I/O |
| T4 | Hook System Expansion | `2026-05-02-hook-system-expansion.md` | Medium | 27 lifecycle events, HTTP hooks, prompt hooks, fail-open design |
| T5 | LLM Permission Classifier | `2026-05-02-llm-permission-classifier.md` | Medium | `YOLOClassifier` two-stage safety check for `ModeAuto` |

## Out of Scope (future plans)

- Async memory/skill prefetch — requires deeper skill runtime changes
- Stop hooks with continuation control — requires hook system redesign
- Streaming tombstone pattern — requires SDK message layer changes
