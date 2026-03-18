package agent

import (
	"context"

	"golang.org/x/time/rate"
)

// SharedRateLimiter throttles LLM API requests across parent and child agents.
// A nil SharedRateLimiter is a no-op (no rate limiting).
type SharedRateLimiter struct {
	limiter *rate.Limiter
}

// NewSharedRateLimiter creates a limiter allowing the given requests per minute.
// Returns nil if requestsPerMinute <= 0 (no limiting).
func NewSharedRateLimiter(requestsPerMinute int) *SharedRateLimiter {
	if requestsPerMinute <= 0 {
		return nil
	}
	r := rate.Limit(float64(requestsPerMinute) / 60.0)
	burst := requestsPerMinute / 10
	if burst < 1 {
		burst = 1
	}
	return &SharedRateLimiter{
		limiter: rate.NewLimiter(r, burst),
	}
}

// Wait blocks until a request is permitted or ctx is cancelled.
// A nil receiver is a no-op.
func (rl *SharedRateLimiter) Wait(ctx context.Context) error {
	if rl == nil {
		return nil
	}
	return rl.limiter.Wait(ctx)
}
