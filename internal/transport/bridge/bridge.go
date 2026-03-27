// Package bridge defines the EventBridge interface and utilities for
// forwarding agent events to external systems (NATS, Kafka, etc.).
package bridge

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Envelope mirrors ws.Envelope for bridge-side use without importing the ws package.
type Envelope struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Seq       int64           `json:"seq,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// EventBridge receives agent events and forwards them to an external system.
type EventBridge interface {
	// Publish sends an event to the external system.
	Publish(ctx context.Context, sessionID string, env Envelope) error
	// Subscribe registers a handler for inbound events from the external system.
	Subscribe(ctx context.Context, sessionID string, handler func(Envelope)) error
	// Close shuts down the bridge.
	Close() error
}

// ChannelBridge is an in-memory EventBridge backed by Go channels.
// Useful for testing and in-process event routing.
type ChannelBridge struct {
	events      chan publishedEvent
	subscribers map[string][]func(Envelope)
	mu          sync.RWMutex
	closed      chan struct{}
	once        sync.Once
}

type publishedEvent struct {
	SessionID string
	Envelope  Envelope
}

// NewChannelBridge creates an in-memory bridge with the given buffer size.
func NewChannelBridge(bufferSize int) *ChannelBridge {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	b := &ChannelBridge{
		events:      make(chan publishedEvent, bufferSize),
		subscribers: make(map[string][]func(Envelope)),
		closed:      make(chan struct{}),
	}
	go b.dispatch()
	return b
}

// Publish sends an event to the bridge.
// Blocks until the event is enqueued, the bridge is closed, or the context is cancelled.
func (b *ChannelBridge) Publish(ctx context.Context, sessionID string, env Envelope) error {
	select {
	case <-b.closed:
		return ErrBridgeClosed
	default:
	}
	select {
	case <-b.closed:
		return ErrBridgeClosed
	case b.events <- publishedEvent{SessionID: sessionID, Envelope: env}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Subscribe registers a handler for events on the given session.
// Use "*" to subscribe to all sessions.
func (b *ChannelBridge) Subscribe(_ context.Context, sessionID string, handler func(Envelope)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.closed:
		return ErrBridgeClosed
	default:
	}

	b.subscribers[sessionID] = append(b.subscribers[sessionID], handler)
	return nil
}

// Close shuts down the bridge.
func (b *ChannelBridge) Close() error {
	b.once.Do(func() {
		close(b.closed)
	})
	return nil
}

// dispatch routes published events to subscribers.
func (b *ChannelBridge) dispatch() {
	for {
		select {
		case <-b.closed:
			return
		case evt := <-b.events:
			b.mu.RLock()
			handlers := make([]func(Envelope), 0, len(b.subscribers[evt.SessionID])+len(b.subscribers["*"]))
			handlers = append(handlers, b.subscribers[evt.SessionID]...)
			handlers = append(handlers, b.subscribers["*"]...)
			b.mu.RUnlock()

			for _, handler := range handlers {
				handler(evt.Envelope)
			}
		}
	}
}

// ErrBridgeClosed is returned when operating on a closed bridge.
var ErrBridgeClosed = errBridgeClosed{}

type errBridgeClosed struct{}

func (errBridgeClosed) Error() string { return "bridge: closed" }
