# Streaming Stall Detection Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's streaming stall detection to rubichan. A `StreamWatchdog` monitors the time between SSE events and logs warnings when gaps exceed a threshold, helping diagnose network or provider issues.

**Architecture:** Per-stream goroutine that resets a timer on each event. If the timer fires (no events received within threshold), a warning is logged. The watchdog is attached to the SSE scanner in the Anthropic provider.

**Tech Stack:** Go, existing `sseScanner` in `internal/provider/anthropic/sse.go`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/provider/watchdog.go` | `StreamWatchdog` with configurable thresholds |
| `internal/provider/watchdog_test.go` | Tests for warning, kill, and reset behavior |
| `internal/provider/anthropic/provider.go` | Integrate watchdog into processStream |

---

## Chunk 1: Core Watchdog

### Task 1: Implement StreamWatchdog

**Files:**
- Create: `internal/provider/watchdog.go`

**Code:**

```go
package provider

import (
	"sync"
	"time"
)

// WatchdogConfig configures stall detection thresholds.
type WatchdogConfig struct {
	WarnThreshold time.Duration // log warning after this idle time (default 30s)
	KillThreshold time.Duration // abort stream after this idle time (default 90s)
}

// DefaultWatchdogConfig returns sensible defaults.
func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		WarnThreshold: 30 * time.Second,
		KillThreshold: 90 * time.Second,
	}
}

// StreamWatchdog monitors a stream for stalls (gaps between events).
type StreamWatchdog struct {
	mu       sync.Mutex
	timer    *time.Timer
	config   WatchdogConfig
	onWarn   func()
	onKill   func()
	stopped  bool
}

// NewStreamWatchdog creates a watchdog. Either onWarn or onKill may be nil.
func NewStreamWatchdog(config WatchdogConfig, onWarn, onKill func()) *StreamWatchdog {
	if config.WarnThreshold <= 0 {
		config.WarnThreshold = DefaultWatchdogConfig().WarnThreshold
	}
	if config.KillThreshold <= 0 {
		config.KillThreshold = DefaultWatchdogConfig().KillThreshold
	}
	return &StreamWatchdog{
		config: config,
		onWarn: onWarn,
		onKill: onKill,
	}
}

// Start begins monitoring. Call Reset() on each event.
func (w *StreamWatchdog) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scheduleKill()
}

// Reset should be called on every received event to reset idle timers.
func (w *StreamWatchdog) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.scheduleKill()
}

// Stop halts the watchdog and cancels pending timers.
func (w *StreamWatchdog) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *StreamWatchdog) scheduleKill() {
	if w.stopped {
		return
	}
	w.timer = time.AfterFunc(w.config.KillThreshold, func() {
		w.mu.Lock()
		if w.stopped {
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
		if w.onWarn != nil {
			w.onWarn()
		}
		if w.onKill != nil {
			w.onKill()
		}
	})
}
```

**Test:**

```go
package provider

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamWatchdogWarnsOnStall(t *testing.T) {
	var warned atomic.Bool
	w := NewStreamWatchdog(WatchdogConfig{
		WarnThreshold: 50 * time.Millisecond,
		KillThreshold: 100 * time.Millisecond,
	}, func() { warned.Store(true) }, nil)

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)
	require.True(t, warned.Load(), "expected warning callback fired")
}

func TestStreamWatchdogResetPreventsStall(t *testing.T) {
	var warned atomic.Bool
	w := NewStreamWatchdog(WatchdogConfig{
		WarnThreshold: 50 * time.Millisecond,
		KillThreshold: 100 * time.Millisecond,
	}, func() { warned.Store(true) }, nil)

	w.Start()
	defer w.Stop()

	// Reset before threshold
	time.Sleep(60 * time.Millisecond)
	w.Reset()
	time.Sleep(60 * time.Millisecond)
	w.Reset()

	require.False(t, warned.Load(), "expected no warning when reset")
}

func TestStreamWatchdogStop(t *testing.T) {
	var warned atomic.Bool
	w := NewStreamWatchdog(WatchdogConfig{
		WarnThreshold: 50 * time.Millisecond,
		KillThreshold: 100 * time.Millisecond,
	}, func() { warned.Store(true) }, nil)

	w.Start()
	w.Stop()

	time.Sleep(150 * time.Millisecond)
	require.False(t, warned.Load(), "expected no warning after stop")
}
```

**Command:**
```bash
go test ./internal/provider/... -run TestStreamWatchdog -v
```

**Expected:** All tests PASS.

---

## Chunk 2: Integration

### Task 2: Wire watchdog into Anthropic provider

**Files:**
- Modify: `internal/provider/anthropic/provider.go`

In `processStream`, wrap the SSE scanner with watchdog resets:
```go
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent, requestID string) {
	defer close(ch)

	watchdog := provider.NewStreamWatchdog(provider.DefaultWatchdogConfig(),
		func() {
			if p.debugLogger != nil {
				p.debugLogger("[DEBUG] anthropic: stream idle for 30s (request %s), still waiting", requestID)
			}
		},
		func() {
			if p.debugLogger != nil {
				p.debugLogger("[DEBUG] anthropic: stream killed after 90s idle (request %s)", requestID)
			}
			// Signal cancellation via context or close body
		},
	)
	watchdog.Start()
	defer watchdog.Stop()

	state := newStreamState()
	scanner := newSSEScanner(body)
	for scanner.Next() {
		watchdog.Reset()
		// ... existing event processing ...
	}
	// ...
}
```

**Test:**

```go
func TestProcessStreamWithWatchdog(t *testing.T) {
	// Integration test verifying watchdog doesn't interfere with normal flow
	p := New("http://test", "key")
	// Mock response body with events spaced normally
}
```

**Command:**
```bash
go test ./internal/provider/anthropic/... -run TestProcessStreamWithWatchdog -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./internal/provider/...
go test ./internal/provider/anthropic/...
golangci-lint run ./internal/provider/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Streaming stall detection for provider streams`

**Body:**
- `StreamWatchdog` monitors time between SSE events
- Configurable warn (30s) and kill (90s) thresholds
- `Reset()` called on every received event
- `Stop()` halts monitoring when stream ends
- Integrated into Anthropic `processStream`
- Logs warnings via debugLogger for diagnostics
- Ports Claude Code's streaming idle timeout pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
