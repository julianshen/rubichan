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

## Phase 3: Agent Orchestration (from ccgo/Claude Code research)

| # | Plan | File | Priority | Description |
|---|------|------|----------|-------------|
| G | Sibling Abort on Bash Errors | `2026-05-02-sibling-abort-on-shell-errors.md` | High | Cancel sibling concurrent tools when shell errors; prevents wasted work |
| H | Session Memory Compaction | `2026-05-02-session-memory-compaction.md` | High | Smart compaction preserving API invariants (tool_use/tool_result pairs, thinking blocks) |
| I | Query Source-Aware Retry | `2026-05-02-query-source-aware-retry.md` | High | Foreground retries on 529; background tasks fail fast to avoid amplifying overloads |
| J | Agent Definitions | `2026-05-02-agent-definitions.md` | High | Formalize agent modes with tool filtering, custom models, custom prompts |
| K | Subagent Spawning | `2026-05-02-subagent-spawning.md` | Medium | Sync/async/fork child agents with lifecycle tracking |
| L | Stop Hooks | `2026-05-02-stop-hooks.md` | Medium | Three-phase hooks that can block continuation, inject messages, yield attachments |
| M | Result Storage | `2026-05-02-result-storage.md` | Medium | Disk offload for oversized tool results (>50KB) with 200KB/msg budget |
| N | Streaming Tombstone | `2026-05-02-streaming-tombstone.md` | Medium | Tombstone orphaned messages on model fallback to prevent context pollution |
| O | Prefetch Handles | `2026-05-02-prefetch-handles.md` | Low | Async memory/skill loading while model runs |

## Recommended Execution Order (Phase 3)

1. **G** (sibling abort) — prevents wasted work, simple to implement
2. **I** (source-aware retry) — prevents background tasks from amplifying overloads
3. **J** (agent definitions) — structural foundation for K and L
4. **H** (session memory compaction) — major reliability improvement for long conversations
5. **K** (subagent spawning) — enables parallel work decomposition
6. **L** (stop hooks) — extensibility and loop control
7. **M** (result storage) — handles large tool outputs
8. **N** (streaming tombstone) — clean model fallback
9. **O** (prefetch handles) — performance optimization

## Out of Scope (future plans)

- Multi-agent coordination protocols
- Distributed agent execution
- Persistent agent state across restarts
