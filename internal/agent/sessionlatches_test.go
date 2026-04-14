package agent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionLatches_BoolRatchet_LatchesOnFirstCall(t *testing.T) {
	l := newSessionLatches()

	// First call with false latches false.
	assert.False(t, l.latchToolHint(false))
	// Subsequent calls — even with true — return the latched false.
	assert.False(t, l.latchToolHint(true))
	assert.False(t, l.latchToolHint(true))
}

func TestSessionLatches_BoolRatchet_TrueStaysTrue(t *testing.T) {
	l := newSessionLatches()

	// First call with true latches true.
	assert.True(t, l.latchToolHint(true))
	// Subsequent calls with false return true (ratchet cannot reverse).
	assert.True(t, l.latchToolHint(false))
	assert.True(t, l.latchToolHint(false))
}

func TestSessionLatches_StringRatchet_EmptyDoesNotLatch(t *testing.T) {
	l := newSessionLatches()

	// Empty call returns empty, nothing latched.
	assert.Equal(t, "", l.latchReasoningEffort(""))

	// First non-empty call latches.
	assert.Equal(t, "high", l.latchReasoningEffort("high"))

	// Subsequent calls — any value, empty or not — return the latched value.
	assert.Equal(t, "high", l.latchReasoningEffort("low"))
	assert.Equal(t, "high", l.latchReasoningEffort(""))
	assert.Equal(t, "high", l.latchReasoningEffort("medium"))
}

func TestSessionLatches_ConcurrentAccess(t *testing.T) {
	l := newSessionLatches()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.latchToolHint(n%2 == 0)
			l.latchReasoningEffort("high")
		}(i)
	}
	wg.Wait()
	// After the race, the values are stable (whatever was latched first wins).
	// We can't predict which, but repeated reads must return the same value.
	first := l.latchToolHint(true)
	assert.Equal(t, first, l.latchToolHint(false))
	assert.Equal(t, "high", l.latchReasoningEffort(""))
}
