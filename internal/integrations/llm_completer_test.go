package integrations

import (
	"context"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	events []provider.StreamEvent
}

func (m *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, len(m.events))
	for _, evt := range m.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func TestLLMCompleterComplete(t *testing.T) {
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hello"},
			{Type: "text_delta", Text: " world"},
			{Type: "stop"},
		},
	}

	completer := NewLLMCompleter(mp, "test-model")
	result, err := completer.Complete(context.Background(), "Say hi")
	require.NoError(t, err)
	assert.Equal(t, "Hello world", result)
}

func TestLLMCompleterStreamError(t *testing.T) {
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "partial"},
			{Type: "error", Error: assert.AnError},
		},
	}

	completer := NewLLMCompleter(mp, "test-model")
	_, err := completer.Complete(context.Background(), "fail")
	require.Error(t, err)
}

// slowMockProvider sends events with a delay after the error, simulating a
// provider goroutine that still has events to deliver after an error.
type slowMockProvider struct{}

func (m *slowMockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		ch <- provider.StreamEvent{Type: "error", Error: assert.AnError}
		// Remaining event after error — should be drained by consumer.
		ch <- provider.StreamEvent{Type: "text_delta", Text: "trailing"}
	}()
	return ch, nil
}

func TestLLMCompleterDrainsChannelOnError(t *testing.T) {
	completer := NewLLMCompleter(&slowMockProvider{}, "test-model")
	_, err := completer.Complete(context.Background(), "fail")
	require.Error(t, err)

	// The goroutine sending trailing events should not be blocked.
	// Give it a moment to finish.
	time.Sleep(50 * time.Millisecond)
}
