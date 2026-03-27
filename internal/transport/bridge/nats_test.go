package bridge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockNATSConn is a fake NATS connection for testing.
type mockNATSConn struct {
	published   []mockPublished
	subscribers map[string][]func(string, []byte)
	closed      bool
	mu          sync.Mutex
}

type mockPublished struct {
	Subject string
	Data    []byte
}

func newMockNATSConn() *mockNATSConn {
	return &mockNATSConn{
		subscribers: make(map[string][]func(string, []byte)),
	}
}

func (c *mockNATSConn) Publish(subject string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.published = append(c.published, mockPublished{Subject: subject, Data: data})

	// Deliver to matching subscribers (simple prefix match for test).
	for pattern, handlers := range c.subscribers {
		if matchSubject(pattern, subject) {
			for _, h := range handlers {
				h(subject, data)
			}
		}
	}
	return nil
}

func (c *mockNATSConn) Subscribe(subject string, handler func(string, []byte)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers[subject] = append(c.subscribers[subject], handler)
	return nil
}

func (c *mockNATSConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

// matchSubject does a simplified NATS subject match for test purposes.
// Supports "*" (single token wildcard) and ">" (trailing multi-token wildcard).
func matchSubject(pattern, subject string) bool {
	if pattern == subject {
		return true
	}
	pParts := splitDot(pattern)
	sParts := splitDot(subject)

	for i, pp := range pParts {
		if pp == ">" {
			return true // ">" matches the rest
		}
		if i >= len(sParts) {
			return false
		}
		if pp != "*" && pp != sParts[i] {
			return false
		}
	}
	return len(pParts) == len(sParts)
}

func splitDot(s string) []string {
	var parts []string
	start := 0
	for i := range len(s) {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func TestNATSBridge_Publish(t *testing.T) {
	conn := newMockNATSConn()
	b, err := NewNATSBridge(NATSConfig{Conn: conn})
	require.NoError(t, err)
	defer b.Close()

	env := Envelope{
		Type:      "turn_event",
		SessionID: "sess-1",
		Seq:       1,
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(`{"type":"text_delta"}`),
	}
	require.NoError(t, b.Publish(context.Background(), "sess-1", env))

	conn.mu.Lock()
	defer conn.mu.Unlock()

	require.Len(t, conn.published, 1)
	assert.Equal(t, "rubichan.sessions.sess-1.events.turn_event", conn.published[0].Subject)

	var decoded Envelope
	require.NoError(t, json.Unmarshal(conn.published[0].Data, &decoded))
	assert.Equal(t, "turn_event", decoded.Type)
}

func TestNATSBridge_Subscribe_And_Receive(t *testing.T) {
	conn := newMockNATSConn()
	b, err := NewNATSBridge(NATSConfig{Conn: conn})
	require.NoError(t, err)
	defer b.Close()

	received := make(chan Envelope, 1)
	require.NoError(t, b.Subscribe(context.Background(), "sess-1", func(env Envelope) {
		received <- env
	}))

	env := Envelope{
		Type:      "event",
		SessionID: "sess-1",
		Timestamp: time.Now().UTC(),
	}
	require.NoError(t, b.Publish(context.Background(), "sess-1", env))

	select {
	case got := <-received:
		assert.Equal(t, "event", got.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestNATSBridge_CustomSubjectPrefix(t *testing.T) {
	conn := newMockNATSConn()
	b, err := NewNATSBridge(NATSConfig{
		Conn:          conn,
		SubjectPrefix: "myapp.agents",
	})
	require.NoError(t, err)
	defer b.Close()

	env := Envelope{Type: "test", Timestamp: time.Now().UTC()}
	require.NoError(t, b.Publish(context.Background(), "s1", env))

	conn.mu.Lock()
	assert.Equal(t, "myapp.agents.s1.events.test", conn.published[0].Subject)
	conn.mu.Unlock()
}

func TestNATSBridge_Close(t *testing.T) {
	conn := newMockNATSConn()
	b, err := NewNATSBridge(NATSConfig{Conn: conn})
	require.NoError(t, err)

	require.NoError(t, b.Close())

	conn.mu.Lock()
	assert.True(t, conn.closed)
	conn.mu.Unlock()

	err = b.Publish(context.Background(), "x", Envelope{Type: "e", Timestamp: time.Now().UTC()})
	assert.ErrorIs(t, err, ErrBridgeClosed)
}

func TestNATSBridge_NilConn(t *testing.T) {
	_, err := NewNATSBridge(NATSConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NATS connection is required")
}

func TestNATSBridge_WildcardSubscribe(t *testing.T) {
	conn := newMockNATSConn()
	b, err := NewNATSBridge(NATSConfig{Conn: conn})
	require.NoError(t, err)
	defer b.Close()

	received := make(chan Envelope, 5)
	require.NoError(t, b.Subscribe(context.Background(), "*", func(env Envelope) {
		received <- env
	}))

	b.Publish(context.Background(), "sess-1", Envelope{Type: "e1", SessionID: "sess-1", Timestamp: time.Now().UTC()})
	b.Publish(context.Background(), "sess-2", Envelope{Type: "e2", SessionID: "sess-2", Timestamp: time.Now().UTC()})

	types := make(map[string]bool)
	for range 2 {
		select {
		case env := <-received:
			types[env.Type] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	}
	assert.True(t, types["e1"])
	assert.True(t, types["e2"])
}
