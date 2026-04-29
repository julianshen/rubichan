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

## Out of Scope (future plans)

- Async memory/skill prefetch — requires deeper skill runtime changes
- Stop hooks with continuation control — requires hook system redesign
- Token budget diminishing-returns detection — requires budget tracker refactor
- Streaming tombstone pattern — requires SDK message layer changes
