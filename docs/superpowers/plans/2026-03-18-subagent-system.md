# Subagent System Enhancements Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shared rate limiter, SpawnParallel, per-subagent context budget, and max concurrency config to the existing subagent system.

**Architecture:** SharedRateLimiter wraps `golang.org/x/time/rate` and is shared via AgentOption + spawner field. SpawnParallel uses `sourcegraph/conc` pool calling existing Spawn(). ContextBudget field on SubagentConfig overrides child's context window. Config adds max_subagents and max_requests_per_minute.

**Tech Stack:** `golang.org/x/time/rate` (new dep), `sourcegraph/conc` (existing), existing `DefaultSubagentSpawner`.

**Spec:** `docs/superpowers/specs/2026-03-18-subagent-system-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/agent/ratelimiter.go` | `agent` | SharedRateLimiter type, NewSharedRateLimiter, Wait |
| `internal/agent/ratelimiter_test.go` | `agent` | Rate limiter tests |
| `pkg/agentsdk/subagent.go` | `agentsdk` | Add ContextBudget field, SubagentRequest type, SpawnParallel to interface |
| `internal/agent/subagent.go` | `agent` | SpawnParallel impl, context budget override, RateLimiter field |
| `internal/agent/subagent_test.go` | `agent` | SpawnParallel tests |
| `internal/agent/agent.go` | `agent` | WithRateLimiter option, rate limit before Stream() |
| `internal/config/config.go` | `config` | MaxSubagents, MaxRequestsPerMinute fields |
| `cmd/rubichan/main.go` | `main` | Create rate limiter, wire into agent and spawner |

---

## Chunk 1: Rate Limiter + Config

### Task 1: SharedRateLimiter

**Files:**
- Create: `internal/agent/ratelimiter.go`
- Test: `internal/agent/ratelimiter_test.go`

- [ ] **Step 1: Add golang.org/x/time dependency**

Run: `go get golang.org/x/time`

- [ ] **Step 2: Write failing tests**

Add to a new `internal/agent/ratelimiter_test.go`:

```go
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSharedRateLimiterNilWhenZero(t *testing.T) {
	rl := NewSharedRateLimiter(0)
	assert.Nil(t, rl)
}

func TestNewSharedRateLimiterNonNil(t *testing.T) {
	rl := NewSharedRateLimiter(60)
	assert.NotNil(t, rl)
}

func TestSharedRateLimiterWaitNil(t *testing.T) {
	// nil limiter should be a no-op
	var rl *SharedRateLimiter
	err := rl.Wait(context.Background())
	assert.NoError(t, err)
}

func TestSharedRateLimiterWaitPermits(t *testing.T) {
	rl := NewSharedRateLimiter(600) // 10/sec — fast enough for tests
	err := rl.Wait(context.Background())
	assert.NoError(t, err)
}

func TestSharedRateLimiterWaitCancelledContext(t *testing.T) {
	rl := NewSharedRateLimiter(1) // 1/min — very slow
	// Exhaust the burst
	_ = rl.Wait(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := rl.Wait(ctx)
	assert.Error(t, err, "should fail when context cancelled before rate allows")
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestSharedRateLimiter -v`
Expected: FAIL — SharedRateLimiter not defined

- [ ] **Step 4: Write implementation**

Create `internal/agent/ratelimiter.go`:

```go
package agent

import (
	"context"

	"golang.org/x/time/rate"
)

// SharedRateLimiter throttles LLM API requests across parent and child agents.
// A nil SharedRateLimiter is a no-op (no rate limiting).
type SharedRateLimiter struct {
	limiter *rate.Limiter
}

// NewSharedRateLimiter creates a limiter allowing the given requests per minute.
// Returns nil if requestsPerMinute <= 0 (no limiting).
func NewSharedRateLimiter(requestsPerMinute int) *SharedRateLimiter {
	if requestsPerMinute <= 0 {
		return nil
	}
	r := rate.Limit(float64(requestsPerMinute) / 60.0)
	burst := requestsPerMinute / 10
	if burst < 1 {
		burst = 1
	}
	return &SharedRateLimiter{
		limiter: rate.NewLimiter(r, burst),
	}
}

// Wait blocks until a request is permitted or ctx is cancelled.
// A nil receiver is a no-op.
func (rl *SharedRateLimiter) Wait(ctx context.Context) error {
	if rl == nil {
		return nil
	}
	return rl.limiter.Wait(ctx)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestSharedRateLimiter -v`
Expected: PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add SharedRateLimiter with token bucket rate limiting
```

---

### Task 2: Config fields + WithRateLimiter option

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/agent/agent.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing config test**

```go
func TestConfigSubagentSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[agent]
max_subagents = 5
max_requests_per_minute = 120
`), 0644)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Agent.MaxSubagents)
	assert.Equal(t, 120, cfg.Agent.MaxRequestsPerMinute)
}
```

- [ ] **Step 2: Add config fields**

In `internal/config/config.go`, add to `AgentConfig`:

```go
MaxSubagents         int `toml:"max_subagents"`
MaxRequestsPerMinute int `toml:"max_requests_per_minute"`
```

- [ ] **Step 3: Add WithRateLimiter option to agent**

In `internal/agent/agent.go`:

```go
// Add to Agent struct:
rateLimiter *SharedRateLimiter

// New option:
func WithRateLimiter(rl *SharedRateLimiter) AgentOption {
	return func(a *Agent) {
		a.rateLimiter = rl
	}
}
```

- [ ] **Step 4: Add rate limit check before provider.Stream()**

In `runLoop()`, just before the `a.provider.Stream(ctx, req)` call (line ~1020):

```go
if a.rateLimiter != nil {
	if err := a.rateLimiter.Wait(ctx); err != nil {
		ch <- TurnEvent{Type: "error", Error: fmt.Errorf("rate limiter: %w", err)}
		ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
		return
	}
}
```

- [ ] **Step 5: Verify build and tests**

Run: `go build ./... && go test ./internal/config/ -run TestConfigSubagentSettings -v`
Expected: BUILD OK, PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add MaxSubagents/MaxRequestsPerMinute config and WithRateLimiter
```

---

## Chunk 2: SDK Types + SpawnParallel + Context Budget

### Task 3: SDK type changes (ContextBudget field, SubagentRequest, interface)

**Files:**
- Modify: `pkg/agentsdk/subagent.go`

- [ ] **Step 1: Add ContextBudget field to SubagentConfig**

```go
type SubagentConfig struct {
	// ... existing fields ...
	ContextBudget int // Context window override (0 = inherit parent)
}
```

Add after the `MaxTokens` field.

- [ ] **Step 2: Add SubagentRequest type**

```go
// SubagentRequest pairs a config with a prompt for parallel spawning.
type SubagentRequest struct {
	Config SubagentConfig
	Prompt string
}
```

- [ ] **Step 3: Add SpawnParallel to interface**

```go
type SubagentSpawner interface {
	Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
	SpawnParallel(ctx context.Context, requests []SubagentRequest, maxConcurrent int) ([]SubagentResult, error)
}
```

- [ ] **Step 4: Verify build**

Run: `go build ./pkg/agentsdk/`
Expected: FAIL — DefaultSubagentSpawner doesn't implement SpawnParallel yet. That's expected.

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add ContextBudget, SubagentRequest, SpawnParallel to SDK types
```

---

### Task 4: SpawnParallel implementation + context budget override

**Files:**
- Modify: `internal/agent/subagent.go`
- Test: `internal/agent/subagent_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestSpawnParallelBasic(t *testing.T) {
	// Create a mock spawner that records calls
	spawner := &DefaultSubagentSpawner{
		Config: &config.Config{
			Provider: config.ProviderConfig{Model: "test"},
			Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
		},
	}

	// SpawnParallel requires a working provider — test the structure
	// and error handling, not full agent execution
	requests := []SubagentRequest{
		{Config: SubagentConfig{Name: "a"}, Prompt: "task a"},
		{Config: SubagentConfig{Name: "b"}, Prompt: "task b"},
	}

	// Without a provider, Spawn will fail — but SpawnParallel should
	// return results with errors, not panic
	results, err := spawner.SpawnParallel(context.Background(), requests, 2)
	assert.NoError(t, err) // top-level error is nil
	assert.Len(t, results, 2)
	// Each result should have an error (no provider)
	assert.Error(t, results[0].Error)
	assert.Error(t, results[1].Error)
}

func TestSpawnParallelEmptyRequests(t *testing.T) {
	spawner := &DefaultSubagentSpawner{
		Config: &config.Config{},
	}
	results, err := spawner.SpawnParallel(context.Background(), nil, 3)
	assert.NoError(t, err)
	assert.Empty(t, results)
}
```

Note: Full integration tests with a real provider are complex. These test the structural behavior (result ordering, error propagation, empty input).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestSpawnParallel -v`
Expected: FAIL — SpawnParallel not defined

- [ ] **Step 3: Write implementation**

Add to `internal/agent/subagent.go`:

```go
// Add to DefaultSubagentSpawner struct:
RateLimiter *SharedRateLimiter

// SpawnParallel launches multiple subagents concurrently.
func (s *DefaultSubagentSpawner) SpawnParallel(
	ctx context.Context,
	requests []SubagentRequest,
	maxConcurrent int,
) ([]SubagentResult, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}

	results := make([]SubagentResult, len(requests))
	p := pool.New().WithMaxGoroutines(maxConcurrent)

	for i, req := range requests {
		i, req := i, req // capture
		p.Go(func() {
			result, err := s.Spawn(ctx, req.Config, req.Prompt)
			if err != nil {
				results[i] = SubagentResult{
					Name:  req.Config.Name,
					Error: err,
				}
				return
			}
			results[i] = *result
		})
	}

	p.Wait()
	return results, nil
}
```

Add the `sourcegraph/conc/pool` import.

Also, in `Spawn()`, add context budget override after building `childCfg`:

```go
// After: childCfg := *s.Config (or wherever childCfg is built)
if cfg.ContextBudget > 0 {
	childCfg.Agent.ContextBudget = cfg.ContextBudget
}
```

And pass rate limiter to child agent:

```go
// In the opts building section of Spawn():
if s.RateLimiter != nil {
	opts = append(opts, WithRateLimiter(s.RateLimiter))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestSpawnParallel -v`
Expected: PASS

- [ ] **Step 5: Verify full build**

Run: `go build ./...`
Expected: BUILD OK (DefaultSubagentSpawner now satisfies SubagentSpawner interface)

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add SpawnParallel, context budget override, and rate limiter propagation
```

---

## Chunk 3: Main.go Wiring + Final

### Task 5: Wire rate limiter into main.go

**Files:**
- Modify: `cmd/rubichan/main.go`

- [ ] **Step 1: Add rate limiter creation in interactive setup**

Find where the spawner is created (search for `DefaultSubagentSpawner`). After spawner creation, add:

```go
// Create shared rate limiter
var rateLimiter *agent.SharedRateLimiter
if cfg.Agent.MaxRequestsPerMinute > 0 {
	rateLimiter = agent.NewSharedRateLimiter(cfg.Agent.MaxRequestsPerMinute)
}
if rateLimiter != nil {
	opts = append(opts, agent.WithRateLimiter(rateLimiter))
}

// After spawner fields are set:
spawner.RateLimiter = rateLimiter
```

- [ ] **Step 2: Add same wiring in headless setup**

Same pattern in `runHeadless()`.

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: BUILD OK

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] wire shared rate limiter into interactive and headless modes
```

---

### Task 6: Final integration — tests + lint + coverage

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Check formatting**

Run: `gofmt -l .`
Expected: No files

- [ ] **Step 3: Verify interface satisfaction**

Run: `go build ./...`
Expected: BUILD OK (no interface mismatch errors)

- [ ] **Step 4: Commit any fixes**

```
[STRUCTURAL] fix lint and formatting
```
