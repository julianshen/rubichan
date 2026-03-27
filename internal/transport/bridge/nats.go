package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// NATSConn abstracts the NATS connection for testability.
// In production, *nats.Conn satisfies this interface.
type NATSConn interface {
	Publish(subject string, data []byte) error
	Subscribe(subject string, handler func(subject string, data []byte)) error
	Close()
}

// NATSBridge forwards agent events to a NATS server.
type NATSBridge struct {
	conn          NATSConn
	subjectPrefix string
	closed        chan struct{}
	once          sync.Once
}

// NATSConfig configures the NATS bridge.
type NATSConfig struct {
	// Conn is the NATS connection to use.
	Conn NATSConn
	// SubjectPrefix is the prefix for NATS subjects.
	// Default: "rubichan.sessions"
	SubjectPrefix string
}

// NewNATSBridge creates a NATS bridge.
func NewNATSBridge(cfg NATSConfig) (*NATSBridge, error) {
	if cfg.Conn == nil {
		return nil, fmt.Errorf("bridge: NATS connection is required")
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = "rubichan.sessions"
	}
	return &NATSBridge{
		conn:          cfg.Conn,
		subjectPrefix: cfg.SubjectPrefix,
		closed:        make(chan struct{}),
	}, nil
}

// Publish sends an envelope to NATS.
// Subject format: {prefix}.{session_id}.events.{event_type}
func (b *NATSBridge) Publish(_ context.Context, sessionID string, env Envelope) error {
	select {
	case <-b.closed:
		return ErrBridgeClosed
	default:
	}

	subject := fmt.Sprintf("%s.%s.events.%s", b.subjectPrefix, sessionID, env.Type)
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("bridge: marshal envelope: %w", err)
	}
	return b.conn.Publish(subject, data)
}

// Subscribe registers a handler for events on a NATS subject.
// Subscribes to {prefix}.{session_id}.events.> (wildcard for all event types).
// Use "*" as sessionID to subscribe to all sessions.
func (b *NATSBridge) Subscribe(_ context.Context, sessionID string, handler func(Envelope)) error {
	select {
	case <-b.closed:
		return ErrBridgeClosed
	default:
	}

	subjectPattern := sessionID
	if sessionID == "*" {
		subjectPattern = "*"
	}
	subject := fmt.Sprintf("%s.%s.events.>", b.subjectPrefix, subjectPattern)

	return b.conn.Subscribe(subject, func(_ string, data []byte) {
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return
		}
		handler(env)
	})
}

// Close shuts down the NATS bridge.
func (b *NATSBridge) Close() error {
	b.once.Do(func() {
		close(b.closed)
		b.conn.Close()
	})
	return nil
}
