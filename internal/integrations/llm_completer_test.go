package integrations

import (
	"context"
	"testing"

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
// The done channel is closed when the producer goroutine has finished,
// allowing tests to synchronize without time.Sleep.
type slowMockProvider struct {
	done chan struct{}
}

func newSlowMockProvider() *slowMockProvider {
	return &slowMockProvider{done: make(chan struct{})}
}

func (m *slowMockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		defer close(m.done)
		ch <- provider.StreamEvent{Type: "error", Error: assert.AnError}
		// Remaining event after error — should be drained by consumer.
		ch <- provider.StreamEvent{Type: "text_delta", Text: "trailing"}
	}()
	return ch, nil
}

func TestLLMCompleterDrainsChannelOnError(t *testing.T) {
	sp := newSlowMockProvider()
	completer := NewLLMCompleter(sp, "test-model")
	_, err := completer.Complete(context.Background(), "fail")
	require.Error(t, err)

	// Wait for the producer goroutine to finish. If the drain goroutine
	// doesn't consume the trailing event, this will block and the test
	// will be killed by the test timeout.
	<-sp.done
}

// blockingProvider never closes the channel, simulating a hang.
type blockingProvider struct {
	ch chan provider.StreamEvent
}

func (m *blockingProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	return m.ch, nil
}

func TestLLMCompleterRespectsContext(t *testing.T) {
	bp := &blockingProvider{ch: make(chan provider.StreamEvent)}
	completer := NewLLMCompleter(bp, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := completer.Complete(ctx, "should timeout")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	// Close the channel so drain goroutine finishes.
	close(bp.ch)
}
