package shell

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHintProviderCacheMiss(t *testing.T) {
	t.Parallel()

	llmCalled := make(chan struct{}, 1)
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 3)
		ch <- TurnEvent{Type: "text_delta", Text: "--rm\n--name\n--port"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		llmCalled <- struct{}{}
		return ch, nil
	}

	hp := NewHintProvider(agentTurn)

	// First call — cache miss, returns empty immediately (non-blocking)
	results := hp.Hint("docker run --")
	assert.Empty(t, results, "first call should return empty (cache miss)")

	// Wait for background LLM call to complete
	select {
	case <-llmCalled:
	case <-time.After(time.Second):
		t.Fatal("LLM was not called")
	}

	// Small delay for cache population
	time.Sleep(50 * time.Millisecond)

	// Second call should return cached results
	results = hp.Hint("docker run --")
	assert.NotEmpty(t, results, "second call should return cached results")
}

func TestHintProviderCacheHit(t *testing.T) {
	t.Parallel()

	callCount := 0
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "--verbose\n--quiet"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	hp := NewHintProvider(agentTurn)

	// Pre-populate cache
	hp.mu.Lock()
	hp.cache["docker run --"] = []Completion{
		{Text: "--rm"},
		{Text: "--name"},
	}
	hp.mu.Unlock()

	results := hp.Hint("docker run --")
	assert.Len(t, results, 2)
	assert.Equal(t, 0, callCount, "LLM should not be called on cache hit")
}

func TestHintProviderPromptFormat(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	done := make(chan struct{})
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedPrompt = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "--flag1\n--flag2"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		close(done)
		return ch, nil
	}

	hp := NewHintProvider(agentTurn)
	hp.Hint("go test -run")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("LLM not called")
	}

	assert.Contains(t, capturedPrompt, "go test -run")
	assert.Contains(t, capturedPrompt, "flag")
}

func TestHintProviderConcurrency(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := 0
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "--flag"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	hp := NewHintProvider(agentTurn)

	// Call concurrently with different commands
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			hp.Hint("cmd" + string(rune('0'+n)) + " --")
		}(i)
	}
	wg.Wait()

	// No race condition — if we get here, sync.RWMutex works correctly
	time.Sleep(100 * time.Millisecond)
}

func TestHintProviderDisabled(t *testing.T) {
	t.Parallel()

	hp := NewHintProvider(nil)
	results := hp.Hint("docker run --")
	assert.Empty(t, results, "nil agent should return empty")
}
