# Skill System Improvements — Phase 4: Skill Event Bus

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add pub/sub event bus for skill lifecycle events (activate, deactivate, error, state change).

**Architecture:** 
- `SkillEventBus` with typed events and subscriber callbacks
- Events: `SkillActivated`, `SkillDeactivated`, `SkillError`, `SkillStateChanged`
- Thread-safe with RWMutex
- Integrated into `Runtime` to emit events on lifecycle changes

**Tech Stack:** Go, existing `internal/skills` package.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/skills/events.go` | New: SkillEventBus with pub/sub |
| `internal/skills/events_test.go` | Tests for event bus |
| `internal/skills/runtime.go` | Modify: emit events on lifecycle changes |

---

## Chunk 1: Skill Event Bus

### Task 1: Create SkillEventBus

**Files:**
- Create: `internal/skills/events.go`
- Create: `internal/skills/events_test.go`

**Step 1: Write the failing test**

Create `internal/skills/events_test.go`:

```go
package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillEventBusSubscribeAndPublish(t *testing.T) {
	bus := NewSkillEventBus()

	var received []SkillEvent
	bus.Subscribe(func(evt SkillEvent) {
		received = append(received, evt)
	})

	bus.Publish(SkillEvent{
		Type:    EventSkillActivated,
		SkillName: "test-skill",
		State:   SkillStateActive,
	})

	require.Len(t, received, 1)
	assert.Equal(t, EventSkillActivated, received[0].Type)
	assert.Equal(t, "test-skill", received[0].SkillName)
	assert.Equal(t, SkillStateActive, received[0].State)
}

func TestSkillEventBusMultipleSubscribers(t *testing.T) {
	bus := NewSkillEventBus()

	var count1, count2 int
	bus.Subscribe(func(evt SkillEvent) { count1++ })
	bus.Subscribe(func(evt SkillEvent) { count2++ })

	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s1"})
	bus.Publish(SkillEvent{Type: EventSkillDeactivated, SkillName: "s2"})

	assert.Equal(t, 2, count1)
	assert.Equal(t, 2, count2)
}

func TestSkillEventBusUnsubscribe(t *testing.T) {
	bus := NewSkillEventBus()

	var count int
	handle := bus.Subscribe(func(evt SkillEvent) { count++ })

	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s1"})
	assert.Equal(t, 1, count)

	bus.Unsubscribe(handle)
	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s2"})
	assert.Equal(t, 1, count)
}

func TestSkillEventBusNoSubscribers(t *testing.T) {
	bus := NewSkillEventBus()
	// Should not panic.
	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s1"})
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestSkillEventBus -v
```

Expected: FAIL — types don't exist.

**Step 3: Implement SkillEventBus**

Create `internal/skills/events.go`:

```go
package skills

import "sync"

// EventType represents the type of a skill lifecycle event.
type EventType string

const (
	// EventSkillActivated is emitted when a skill transitions to Active.
	EventSkillActivated EventType = "skill:activated"
	// EventSkillDeactivated is emitted when a skill transitions to Inactive.
	EventSkillDeactivated EventType = "skill:deactivated"
	// EventSkillError is emitted when a skill encounters an error.
	EventSkillError EventType = "skill:error"
	// EventSkillStateChanged is emitted on any state transition.
	EventSkillStateChanged EventType = "skill:state_changed"
)

// SkillEvent represents a skill lifecycle event.
type SkillEvent struct {
	Type      EventType
	SkillName string
	State     SkillState
	Error     error
	Data      map[string]any
}

// EventHandler is a callback function for skill events.
type EventHandler func(SkillEvent)

// EventSubscriptionHandle identifies a subscription for unsubscribe.
type EventSubscriptionHandle int

// SkillEventBus provides pub/sub for skill lifecycle events.
type SkillEventBus struct {
	mu          sync.RWMutex
	subscribers map[EventSubscriptionHandle]EventHandler
	nextHandle  EventSubscriptionHandle
}

// NewSkillEventBus creates a new event bus.
func NewSkillEventBus() *SkillEventBus {
	return &SkillEventBus{
		subscribers: make(map[EventSubscriptionHandle]EventHandler),
	}
}

// Subscribe registers an event handler. Returns a handle for unsubscribe.
func (bus *SkillEventBus) Subscribe(handler EventHandler) EventSubscriptionHandle {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.nextHandle++
	handle := bus.nextHandle
	bus.subscribers[handle] = handler
	return handle
}

// Unsubscribe removes a handler.
func (bus *SkillEventBus) Unsubscribe(handle EventSubscriptionHandle) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	delete(bus.subscribers, handle)
}

// Publish emits an event to all subscribers.
func (bus *SkillEventBus) Publish(event SkillEvent) {
	bus.mu.RLock()
	// Copy subscribers to avoid holding lock during callbacks.
	handlers := make([]EventHandler, 0, len(bus.subscribers))
	for _, h := range bus.subscribers {
		handlers = append(handlers, h)
	}
	bus.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestSkillEventBus -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/skills/events.go internal/skills/events_test.go
git commit -m "[BEHAVIORAL] Add SkillEventBus for skill lifecycle events"
```

---

## Chunk 2: Runtime Integration

### Task 2: Emit events from Runtime lifecycle methods

**Files:**
- Modify: `internal/skills/runtime.go`
- Test: `internal/skills/runtime_test.go`

**Step 1: Add event bus to Runtime**

Add to `Runtime` struct:

```go
	eventBus            *SkillEventBus
```

Initialize in `NewRuntime`:

```go
		eventBus:            NewSkillEventBus(),
```

Add getter:

```go
// EventBus returns the skill event bus for subscribing to lifecycle events.
func (rt *Runtime) EventBus() *SkillEventBus {
	return rt.eventBus
}
```

**Step 2: Emit events in Activate**

After successful activation (`rt.active[name] = sk`), add:

```go
	if rt.eventBus != nil {
		rt.eventBus.Publish(SkillEvent{
			Type:      EventSkillActivated,
			SkillName: name,
			State:     SkillStateActive,
		})
	}
```

**Step 3: Emit events in Deactivate**

After `delete(rt.active, name)`, add:

```go
	if rt.eventBus != nil {
		rt.eventBus.Publish(SkillEvent{
			Type:      EventSkillDeactivated,
			SkillName: name,
			State:     SkillStateInactive,
		})
	}
```

**Step 4: Emit events on error**

In `Activate` error paths (after `TransitionTo(SkillStateError)`), add:

```go
	if rt.eventBus != nil {
		rt.eventBus.Publish(SkillEvent{
			Type:      EventSkillError,
			SkillName: name,
			State:     SkillStateError,
			Error:     err,
		})
	}
```

**Step 5: Add tests**

Add to `runtime_test.go`:

```go
func TestRuntimeEventBusEmitsActivation(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	skillDir := filepath.Join(userDir, "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	writeSkillYAML(t, skillDir, "test-skill", minimalManifestYAML("test-skill"))

	loader := NewLoader(userDir, projectDir)
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	registry := tools.NewRegistry()
	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &stubPermissionChecker{}
	}

	rt := NewRuntime(loader, s, registry, nil, backendFactory, sandboxFactory)
	require.NoError(t, rt.Discover(nil))

	var events []SkillEvent
	rt.EventBus().Subscribe(func(evt SkillEvent) {
		events = append(events, evt)
	})

	require.NoError(t, rt.Activate("test-skill"))
	require.Len(t, events, 1)
	assert.Equal(t, EventSkillActivated, events[0].Type)
	assert.Equal(t, "test-skill", events[0].SkillName)
	assert.Equal(t, SkillStateActive, events[0].State)
}
```

**Step 6: Run tests**

```bash
go test ./internal/skills/... -run TestRuntimeEventBus -v
```

**Step 7: Commit**

```bash
git add internal/skills/runtime.go internal/skills/runtime_test.go
git commit -m "[BEHAVIORAL] Emit skill lifecycle events from Runtime"
```

---

## Validation Commands

```bash
go test ./internal/skills/...
go test -cover ./internal/skills/...
golangci-lint run ./internal/skills/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Skill system Phase 4: Skill event bus`

**Body:**
- Add `SkillEventBus` with typed events: `SkillActivated`, `SkillDeactivated`, `SkillError`, `SkillStateChanged`
- Thread-safe pub/sub with subscribe/unsubscribe handles
- Runtime emits events on skill activation, deactivation, and errors
- Subscribers receive events asynchronously (lock not held during callbacks)

**Commit prefix:** `[BEHAVIORAL]`
