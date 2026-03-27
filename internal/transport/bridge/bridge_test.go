package bridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelBridge_PublishAndSubscribe(t *testing.T) {
	b := NewChannelBridge(10)
	defer b.Close()

	received := make(chan Envelope, 1)
	require.NoError(t, b.Subscribe(context.Background(), "sess-1", func(env Envelope) {
		received <- env
	}))

	env := Envelope{
		Type:      "turn_event",
		SessionID: "sess-1",
		Seq:       1,
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(`{"type":"text_delta"}`),
	}
	require.NoError(t, b.Publish(context.Background(), "sess-1", env))

	select {
	case got := <-received:
		assert.Equal(t, "turn_event", got.Type)
		assert.Equal(t, int64(1), got.Seq)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestChannelBridge_WildcardSubscriber(t *testing.T) {
	b := NewChannelBridge(10)
	defer b.Close()

	received := make(chan string, 5)
	require.NoError(t, b.Subscribe(context.Background(), "*", func(env Envelope) {
		received <- env.SessionID
	}))

	require.NoError(t, b.Publish(context.Background(), "sess-1", Envelope{Type: "event", SessionID: "sess-1", Timestamp: time.Now().UTC()}))
	require.NoError(t, b.Publish(context.Background(), "sess-2", Envelope{Type: "event", SessionID: "sess-2", Timestamp: time.Now().UTC()}))

	sessions := make(map[string]bool)
	for range 2 {
		select {
		case id := <-received:
			sessions[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	}

	assert.True(t, sessions["sess-1"])
	assert.True(t, sessions["sess-2"])
}

func TestChannelBridge_SessionIsolation(t *testing.T) {
	b := NewChannelBridge(10)
	defer b.Close()

	received := make(chan struct{}, 1)
	require.NoError(t, b.Subscribe(context.Background(), "sess-1", func(_ Envelope) {
		received <- struct{}{}
	}))

	require.NoError(t, b.Publish(context.Background(), "sess-2", Envelope{Type: "event", Timestamp: time.Now().UTC()}))

	// Verify no delivery to sess-1 subscriber.
	select {
	case <-received:
		t.Fatal("sess-1 subscriber should not receive sess-2 event")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message received.
	}
}

func TestChannelBridge_Close(t *testing.T) {
	b := NewChannelBridge(10)
	require.NoError(t, b.Close())

	// Operations after close should return error.
	err := b.Publish(context.Background(), "x", Envelope{Type: "event", Timestamp: time.Now().UTC()})
	assert.ErrorIs(t, err, ErrBridgeClosed)

	err = b.Subscribe(context.Background(), "x", func(_ Envelope) {})
	assert.ErrorIs(t, err, ErrBridgeClosed)

	// Double close should not panic.
	assert.NoError(t, b.Close())
}

func TestChannelBridge_MultipleSubscribers(t *testing.T) {
	b := NewChannelBridge(10)
	defer b.Close()

	count := make(chan int, 2)
	for range 2 {
		require.NoError(t, b.Subscribe(context.Background(), "s", func(_ Envelope) {
			count <- 1
		}))
	}

	require.NoError(t, b.Publish(context.Background(), "s", Envelope{Type: "event", Timestamp: time.Now().UTC()}))

	total := 0
	for range 2 {
		select {
		case <-count:
			total++
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	}
	assert.Equal(t, 2, total)
}
