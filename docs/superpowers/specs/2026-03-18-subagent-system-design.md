# Subagent System Enhancements — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-18 · **Status:** Approved
> **Milestone:** 6, Phase 6
> **Parent:** [Spec Amendments](2026-03-16-spec-amendments-design.md), Amendment 1
> **FRs:** FR-7.1, FR-7.2, FR-7.3, FR-7.4

---

## Overview

Enhance the existing subagent system with: (1) a shared rate limiter preventing API rate limit hits from parallel subagents, (2) `SpawnParallel` for concurrent subagent execution, (3) per-subagent context budget override, and (4) configuration for max concurrency and rate limits.

## Existing Infrastructure

The codebase already provides substantial subagent support:
- **`DefaultSubagentSpawner`** (`internal/agent/subagent.go`) — creates child agents with filtered tools, snapshotted skills, configurable model/max turns
- **`WakeManager`** (`internal/agent/wake.go`) — background subagent event delivery to parent
- **`SubagentConfig`** (`pkg/agentsdk/subagent.go`) — config with tools whitelist, max turns, depth limiting, skill policies, worktree isolation
- **Per-agent isolation** — each child gets its own `Conversation`, `ContextManager`, tool registry (filtered), skill runtime (snapshotted)
- **`TaskTool`** (`internal/tools/task.go`) — LLM-callable subagent invocation with sync/async modes

**What's missing:**
- No rate limiting across agent instances (parallel subagents = unthrottled API calls)
- No `SpawnParallel` (only sequential `Spawn` or single-goroutine background)
- `SubagentConfig.MaxTokens` controls output tokens but not context window budget
- No `max_subagents` config setting

## Components

### 1. SharedRateLimiter (`internal/agent/ratelimiter.go`)

A token-bucket rate limiter shared across parent and all subagents.

```go
type SharedRateLimiter struct {
    limiter *rate.Limiter  // from golang.org/x/time/rate
}

// NewSharedRateLimiter creates a limiter allowing the given requests per minute.
// If requestsPerMinute <= 0, returns nil (no limiting).
func NewSharedRateLimiter(requestsPerMinute int) *SharedRateLimiter

// Wait blocks until a request is permitted or ctx is cancelled.
// Returns nil immediately if the limiter is nil (no limiting configured).
func (r *SharedRateLimiter) Wait(ctx context.Context) error
```

**Integration into agent loop:** In `runLoop()`, before each `provider.Stream()` call, add:

```go
if a.rateLimiter != nil {
    if err := a.rateLimiter.Wait(ctx); err != nil {
        // Context cancelled — return error
        return
    }
}
```

**Sharing:** The `DefaultSubagentSpawner` passes the same `SharedRateLimiter` instance to child agents via a new `WithRateLimiter` option. Child agents inherit the parent's limiter — all agents share one token bucket.

**Token bucket parameters:**
- Rate: `requestsPerMinute / 60` requests per second
- Burst: `max(requestsPerMinute / 10, 1)` — allows short bursts (e.g., 6 for 60 rpm)

### 2. SpawnParallel (`internal/agent/subagent.go`)

New method on `DefaultSubagentSpawner`:

```go
// SubagentRequest pairs a config with a prompt for parallel spawning.
type SubagentRequest struct {
    Config SubagentConfig
    Prompt string
}

// SpawnParallel launches multiple subagents concurrently, limited by
// maxConcurrent. Returns results in the same order as requests.
// Each goroutine calls the existing Spawn() method.
func (s *DefaultSubagentSpawner) SpawnParallel(
    ctx context.Context,
    requests []SubagentRequest,
    maxConcurrent int,
) ([]SubagentResult, error)
```

**Implementation:** Uses `sourcegraph/conc` pool (already in go.mod) with `pool.WithMaxGoroutines(maxConcurrent)`. Each goroutine calls `s.Spawn(ctx, req.Config, req.Prompt)`. Results collected into a pre-allocated slice (index-based, preserving order). If any spawn fails, the error is recorded in that slot's `SubagentResult.Error` — other subagents continue.

**maxConcurrent** comes from `config.Agent.MaxSubagents` (default 3).

**Interface update:** Add `SpawnParallel` to the `SubagentSpawner` interface in `pkg/agentsdk/subagent.go` so SDK consumers and the spawnerAdapter can access it:

```go
type SubagentSpawner interface {
    Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
    SpawnParallel(ctx context.Context, requests []SubagentRequest, maxConcurrent int) ([]SubagentResult, error)
}
```

**Note:** The existing `TaskTool` is NOT changed. The LLM already triggers parallel subagents by issuing multiple tool calls in one response, which the agent loop executes concurrently via its existing `conc` pool. `SpawnParallel` is for programmatic use by Workflow Skills and future SDK consumers.

### 3. Per-Subagent Context Budget (`internal/agent/subagent.go`)

Add a new `ContextBudget` field to `SubagentConfig` in `pkg/agentsdk/subagent.go`:

```go
ContextBudget int // Context window override (0 = inherit parent). Separate from MaxTokens which controls output tokens.
```

This avoids overloading `MaxTokens` (which means "output token budget") with context window semantics. In `DefaultSubagentSpawner.Spawn()`, when building the child config:

```go
childCfg := *s.Config
if cfg.ContextBudget > 0 {
    childCfg.Agent.ContextBudget = cfg.ContextBudget
}
```

The child's `ContextManager` is already initialized from `childCfg.Agent.ContextBudget` in `New()`.

**Note:** The existing hardcoded `MaxTokens: 4096` in the `CompletionRequest` at agent.go line ~1016 is a pre-existing issue that affects all agents, not just subagents. Fixing it is out of scope for this phase but should be tracked separately.

### 4. Configuration (`internal/config/config.go`)

Add to `AgentConfig`:

```go
MaxSubagents         int `toml:"max_subagents"`
MaxRequestsPerMinute int `toml:"max_requests_per_minute"`
```

Config example:

```toml
[agent]
max_subagents = 3              # max concurrent subagents (default 3)
max_requests_per_minute = 60   # shared rate limit (default 0 = unlimited)
```

### 5. Main.go Wiring (`cmd/rubichan/main.go`)

In both interactive and headless setup:

```go
// Create shared rate limiter
var rateLimiter *agent.SharedRateLimiter
if cfg.Agent.MaxRequestsPerMinute > 0 {
    rateLimiter = agent.NewSharedRateLimiter(cfg.Agent.MaxRequestsPerMinute)
}

// Pass to agent
if rateLimiter != nil {
    opts = append(opts, agent.WithRateLimiter(rateLimiter))
}

// Pass to spawner (after spawner creation)
spawner.RateLimiter = rateLimiter
```

The spawner already has access to its fields set after construction (existing pattern — `spawner.Provider` is set post-construction today).

### 6. Spawner Enhancement (`internal/agent/subagent.go`)

Add `RateLimiter *SharedRateLimiter` field to `DefaultSubagentSpawner`. In `Spawn()`, add `WithRateLimiter(s.RateLimiter)` to child agent options.

## Scope Exclusions

- **No tool state deep-copy** — tools are filtered, not mutated by subagents in practice
- **No skill backend isolation** — shared Starlark VMs acceptable (deterministic)
- **No shared context budget pool** — each agent manages independently (simpler)
- **No TaskTool changes** — parallel execution already handled by agent loop's conc pool
- **No file write locking** — the checkpoint system's advisory locking (from Phase 1) is sufficient

## File Summary

| File | Package | Change |
|------|---------|--------|
| `internal/agent/ratelimiter.go` | `agent` | SharedRateLimiter type |
| `internal/agent/ratelimiter_test.go` | `agent` | Rate limiter tests |
| `pkg/agentsdk/subagent.go` | `agentsdk` | Add ContextBudget field, SubagentRequest type, SpawnParallel to interface |
| `internal/agent/subagent.go` | `agent` | SpawnParallel impl, context budget override, RateLimiter field |
| `internal/agent/subagent_test.go` | `agent` | SpawnParallel tests |
| `internal/agent/agent.go` | `agent` | WithRateLimiter option, rate limit before Stream() |
| `internal/config/config.go` | `config` | MaxSubagents, MaxRequestsPerMinute |
| `internal/config/config_test.go` | `config` | Config test |
| `cmd/rubichan/main.go` | `main` | Create rate limiter, wire into agent and spawner |

## Dependencies

- `golang.org/x/time/rate` — token bucket rate limiter (needs `go get`)
- `sourcegraph/conc` — already in go.mod for parallel execution
- No other new dependencies
