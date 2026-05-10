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
// The Data field is optional and should be treated as read-only by subscribers
// to avoid data races. If a subscriber needs to modify data, it should copy
// the map first.
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
