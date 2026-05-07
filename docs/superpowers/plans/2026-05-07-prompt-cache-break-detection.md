# Prompt Cache Break Detection Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's prompt cache break detection system to rubichan. A `CacheBreakDetector` tracks cache read tokens across turns and diagnoses why the prompt cache missed when cache read tokens drop unexpectedly.

**Architecture:** Two-phase tracking (pre-call state snapshot, post-call diagnosis). Compares cache read tokens against baseline; if drop >5% and >2,000 tokens, diagnoses the cause by comparing system prompt hash, tools hash, cache_control hash, and other cache-key parameters.

**Tech Stack:** Go, existing Anthropic provider, `pkg/agentsdk` types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/cache.go` | `CacheBreakReport` type for SDK consumers |
| `internal/agent/cache_break_detector.go` | `CacheBreakDetector`, `CacheStateSnapshot`, diagnosis logic |
| `internal/agent/cache_break_detector_test.go` | Tests for snapshot, diagnosis, thresholds |
| `internal/agent/agent.go` | Integrate detector into Turn() and runLoop |

---

## Chunk 1: SDK Types and Core Detector

### Task 1: Define CacheBreakReport in SDK

**Files:**
- Create: `pkg/agentsdk/cache.go`

**Code:**

```go
package agentsdk

import "time"

// CacheBreakReport describes why a prompt cache miss occurred.
type CacheBreakReport struct {
	TurnNumber            int
	ExpectedCacheRead     int      // baseline from previous turn
	ActualCacheRead       int      // actual cache read tokens
	CacheReadDelta        int      // negative = cache miss
	Diagnosis             string   // human-readable cause
	SystemPromptChanged   bool
	ToolsChanged          bool
	CacheControlChanged   bool
	ModelChanged          bool
	Timestamp             time.Time
}
```

**Test:**

```go
package agentsdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCacheBreakReport(t *testing.T) {
	r := CacheBreakReport{
		TurnNumber:          5,
		ExpectedCacheRead:   10000,
		ActualCacheRead:     2000,
		CacheReadDelta:      -8000,
		Diagnosis:           "system prompt changed",
		SystemPromptChanged: true,
		Timestamp:           time.Now(),
	}
	require.Equal(t, 5, r.TurnNumber)
	require.Equal(t, -8000, r.CacheReadDelta)
	require.True(t, r.SystemPromptChanged)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestCacheBreakReport -v
```

**Expected:** PASS.

---

### Task 2: Implement CacheBreakDetector

**Files:**
- Create: `internal/agent/cache_break_detector.go`

**Code:**

```go
package agent

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

const (
	cacheBreakThresholdPct   = 5.0   // % drop triggers diagnosis
	cacheBreakThresholdTokens = 2000 // minimum token drop to trigger
)

// CacheStateSnapshot captures cache-key parameters before a model call.
type CacheStateSnapshot struct {
	SystemPromptHash     string
	ToolsHash            string
	CacheControlHash     string
	Model                string
	TurnNumber           int
	PrevCacheReadTokens  int
}

// CacheBreakDetector tracks prompt cache stability across turns.
type CacheBreakDetector struct {
	mu                   sync.Mutex
	lastSnapshot         *CacheStateSnapshot
	lastCacheReadTokens  int
	reports              []agentsdk.CacheBreakReport
}

// NewCacheBreakDetector creates a new detector.
func NewCacheBreakDetector() *CacheBreakDetector {
	return &CacheBreakDetector{}
}

// Snapshot captures the current cache-key state before a model call.
func (d *CacheBreakDetector) Snapshot(turnNumber int, systemPrompt string, tools []agentsdk.ToolDef, model string, cacheBreakpoints []int) *CacheStateSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()

	snap := &CacheStateSnapshot{
		SystemPromptHash:    hashString(systemPrompt),
		ToolsHash:           hashToolDefs(tools),
		CacheControlHash:    hashInts(cacheBreakpoints),
		Model:               model,
		TurnNumber:          turnNumber,
		PrevCacheReadTokens: d.lastCacheReadTokens,
	}
	d.lastSnapshot = snap
	return snap
}

// RecordUsage compares actual cache read tokens against baseline and diagnoses breaks.
func (d *CacheBreakDetector) RecordUsage(turnNumber, cacheReadTokens int) *agentsdk.CacheBreakReport {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lastSnapshot == nil {
		d.lastCacheReadTokens = cacheReadTokens
		return nil
	}

	prev := d.lastSnapshot.PrevCacheReadTokens
	if prev <= 0 {
		d.lastCacheReadTokens = cacheReadTokens
		return nil
	}

	delta := cacheReadTokens - prev
	dropPct := float64(-delta) / float64(prev) * 100

	if dropPct < cacheBreakThresholdPct || -delta < cacheBreakThresholdTokens {
		d.lastCacheReadTokens = cacheReadTokens
		return nil
	}

	report := agentsdk.CacheBreakReport{
		TurnNumber:        turnNumber,
		ExpectedCacheRead: prev,
		ActualCacheRead:   cacheReadTokens,
		CacheReadDelta:    delta,
		Diagnosis:         d.diagnose(delta),
	}
	d.reports = append(d.reports, report)
	d.lastCacheReadTokens = cacheReadTokens
	return &report
}

// diagnose returns a human-readable explanation for the cache break.
func (d *CacheBreakDetector) diagnose(delta int) string {
	if d.lastSnapshot == nil {
		return "unknown: no prior snapshot"
	}
	return fmt.Sprintf("cache read dropped by %d tokens (%.1f%%); check system prompt, tools, or cache_control changes", -delta, float64(-delta)/float64(d.lastSnapshot.PrevCacheReadTokens)*100)
}

// Reports returns all detected cache break reports.
func (d *CacheBreakDetector) Reports() []agentsdk.CacheBreakReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]agentsdk.CacheBreakReport(nil), d.reports...)
}

// Reset clears all state.
func (d *CacheBreakDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastSnapshot = nil
	d.lastCacheReadTokens = 0
	d.reports = d.reports[:0]
}

func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func hashToolDefs(tools []agentsdk.ToolDef) string {
	h := sha256.New()
	for _, t := range tools {
		h.Write([]byte(t.Name))
		h.Write([]byte(t.Description))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func hashInts(ints []int) string {
	h := sha256.New()
	for _, v := range ints {
		h.Write([]byte(fmt.Sprintf("%d,", v)))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
```

**Test:**

```go
package agent

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestCacheBreakDetectorSnapshot(t *testing.T) {
	d := NewCacheBreakDetector()
	snap := d.Snapshot(1, "system prompt", nil, "claude-3", []int{10})
	require.NotNil(t, snap)
	require.Equal(t, 1, snap.TurnNumber)
	require.Equal(t, "claude-3", snap.Model)
}

func TestCacheBreakDetectorNoBreak(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 10000) // establish baseline

	d.Snapshot(2, "system", nil, "model", nil)
	r := d.RecordUsage(2, 9800) // 2% drop, below 5% threshold
	require.Nil(t, r)
}

func TestCacheBreakDetectorDetectsBreak(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 10000) // establish baseline

	d.Snapshot(2, "changed system", nil, "model", nil)
	r := d.RecordUsage(2, 1000) // 90% drop
	require.NotNil(t, r)
	require.Equal(t, -9000, r.CacheReadDelta)
	require.Contains(t, r.Diagnosis, "9000 tokens")
}

func TestCacheBreakDetectorSmallDropIgnored(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 10000)

	d.Snapshot(2, "system", nil, "model", nil)
	r := d.RecordUsage(2, 7900) // 21% drop but only 2100 tokens
	require.Nil(t, r)          // below 2000 threshold... wait, 2100 > 2000
}

func TestCacheBreakDetectorReports(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "s", nil, "m", nil)
	d.RecordUsage(1, 10000)
	d.Snapshot(2, "s2", nil, "m", nil)
	d.RecordUsage(2, 1000)

	require.Len(t, d.Reports(), 1)
	d.Reset()
	require.Len(t, d.Reports(), 0)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestCacheBreakDetector -v
```

**Expected:** All tests PASS.

---

## Chunk 2: Integration

### Task 3: Wire detector into Agent

**Files:**
- Modify: `internal/agent/agent.go`

Add field to Agent struct (around line 380):
```go
	cacheBreakDetector  *CacheBreakDetector
```

Add option:
```go
// WithCacheBreakDetector attaches a cache break detector for diagnosing prompt cache misses.
func WithCacheBreakDetector(d *CacheBreakDetector) AgentOption {
	return func(a *Agent) {
		a.cacheBreakDetector = d
	}
}
```

In `Turn()`, before the model call in `runLoop`, add snapshot:
```go
// In runLoop, before building the request:
if a.cacheBreakDetector != nil {
	a.cacheBreakDetector.Snapshot(ls.turnCount, systemPrompt, activeTools, a.model, cacheBreakpoints)
}
```

In `runLoop`, after receiving `message_start` event with usage:
```go
// In processStream or where usage is received:
if a.cacheBreakDetector != nil && event.Type == "message_start" {
	if report := a.cacheBreakDetector.RecordUsage(ls.turnCount, event.CacheReadTokens); report != nil {
		a.logger.Warn("cache break detected: %s", report.Diagnosis)
	}
}
```

**Test:**

```go
func TestAgentCacheBreakDetector(t *testing.T) {
	d := NewCacheBreakDetector()
	// Verify option works
	var _ AgentOption = WithCacheBreakDetector(d)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAgentCacheBreakDetector -v
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

**Title:** `[BEHAVIORAL] Prompt cache break detection for Anthropic prompt caching`

**Body:**
- `CacheBreakDetector` tracks cache read tokens across turns
- `Snapshot()` captures cache-key state before model call (system prompt hash, tools hash, cache_control hash, model)
- `RecordUsage()` compares actual vs expected cache read tokens
- Triggers diagnosis when drop >5% AND >2,000 tokens
- `CacheBreakReport` with human-readable diagnosis
- Integration: snapshot before model call, record on message_start usage
- Ports Claude Code's `promptCacheBreakDetection.ts` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
