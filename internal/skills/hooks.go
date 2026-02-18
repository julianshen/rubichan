package skills

import (
	"fmt"
	"sort"
	"sync"
)

// Priority constants for hook registration. Lower number = higher priority.
const (
	// PriorityBuiltin is the priority for built-in skills (highest).
	PriorityBuiltin = 0
	// PriorityUser is the priority for user-level skills.
	PriorityUser = 10
	// PriorityProject is the priority for project-level skills.
	PriorityProject = 20
)

// skillHookEntry pairs a skill name, priority, and handler for dispatch ordering.
type skillHookEntry struct {
	skillName string
	priority  int
	handler   HookHandler
}

// cancellablePhases lists hook phases where Cancel=true aborts further dispatch.
var cancellablePhases = map[HookPhase]bool{
	HookOnBeforeToolCall: true,
}

// modifyingPhases lists hook phases where handler output chains into the next handler's input.
var modifyingPhases = map[HookPhase]bool{
	HookOnAfterToolResult:   true,
	HookOnBeforePromptBuild: true,
	HookOnAfterResponse:     true,
}

// LifecycleManager dispatches hook events to registered skill handlers.
// Handlers are called in priority order (lower number = higher priority).
// All methods are safe for concurrent use.
type LifecycleManager struct {
	mu       sync.RWMutex
	handlers map[HookPhase][]skillHookEntry
}

// NewLifecycleManager creates a new LifecycleManager with an empty handler registry.
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		handlers: make(map[HookPhase][]skillHookEntry),
	}
}

// Register adds a hook handler for the given phase and skill. The priority
// determines dispatch order: lower values run first. Built-in skills should
// use PriorityBuiltin (0), user skills PriorityUser (10), and project skills
// PriorityProject (20).
func (lm *LifecycleManager) Register(phase HookPhase, skillName string, priority int, handler HookHandler) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.handlers[phase] = append(lm.handlers[phase], skillHookEntry{
		skillName: skillName,
		priority:  priority,
		handler:   handler,
	})
}

// Unregister removes all hook handlers for the given skill name across all phases.
func (lm *LifecycleManager) Unregister(skillName string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for phase, entries := range lm.handlers {
		filtered := entries[:0]
		for _, e := range entries {
			if e.skillName != skillName {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(lm.handlers, phase)
		} else {
			lm.handlers[phase] = filtered
		}
	}
}

// Dispatch sends an event to all handlers registered for the event's Phase.
// Handlers run in priority order (lower number first).
//
// Behavior depends on the hook phase:
//   - Cancellable phases (e.g. OnBeforeToolCall): if a handler returns Cancel=true,
//     dispatch stops immediately and the cancellation result is returned.
//   - Modifying phases (e.g. OnAfterToolResult, OnBeforePromptBuild, OnAfterResponse):
//     each handler's Modified data is merged into the event's Data before calling the
//     next handler, and the final merged result is returned.
//   - Informational phases: all handlers run; the last non-empty result is returned.
//
// Returns nil if no handlers are registered for the phase.
func (lm *LifecycleManager) Dispatch(event HookEvent) (*HookResult, error) {
	// Snapshot handlers under read lock, then release before calling them.
	lm.mu.RLock()
	entries, ok := lm.handlers[event.Phase]
	if !ok || len(entries) == 0 {
		lm.mu.RUnlock()
		return nil, nil
	}
	sorted := make([]skillHookEntry, len(entries))
	copy(sorted, entries)
	lm.mu.RUnlock()

	// Sort by priority (stable sort preserves registration order for equal priorities).
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].priority < sorted[j].priority
	})

	isCancellable := cancellablePhases[event.Phase]
	isModifying := modifyingPhases[event.Phase]

	var lastResult *HookResult

	for _, entry := range sorted {
		result, err := entry.handler(event)
		if err != nil {
			return nil, fmt.Errorf("hook handler %q (phase %s) failed: %w",
				entry.skillName, event.Phase, err)
		}

		lastResult = &result

		// For cancellable phases, stop on first cancellation.
		if isCancellable && result.Cancel {
			return lastResult, nil
		}

		// For modifying phases, chain the output into the next handler's input.
		if isModifying && result.Modified != nil {
			if event.Data == nil {
				event.Data = make(map[string]any)
			}
			for k, v := range result.Modified {
				event.Data[k] = v
			}
		}
	}

	return lastResult, nil
}
