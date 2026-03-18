package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSharedRateLimiterNilWhenZero(t *testing.T) {
	rl := NewSharedRateLimiter(0)
	assert.Nil(t, rl)
}

func TestNewSharedRateLimiterNonNil(t *testing.T) {
	rl := NewSharedRateLimiter(60)
	assert.NotNil(t, rl)
}

func TestSharedRateLimiterWaitNil(t *testing.T) {
	var rl *SharedRateLimiter
	err := rl.Wait(context.Background())
	assert.NoError(t, err)
}

func TestSharedRateLimiterWaitPermits(t *testing.T) {
	rl := NewSharedRateLimiter(600) // 10/sec
	err := rl.Wait(context.Background())
	assert.NoError(t, err)
}

func TestSharedRateLimiterWaitCancelledContext(t *testing.T) {
	rl := NewSharedRateLimiter(1) // 1/min — very slow
	_ = rl.Wait(context.Background()) // exhaust burst

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := rl.Wait(ctx)
	assert.Error(t, err)
}
