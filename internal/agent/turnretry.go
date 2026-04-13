package agent

import (
	"context"
	"errors"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
)

// TurnRetryConfig configures turn-level retry behavior.
type TurnRetryConfig struct {
	// MaxAttempts is the maximum number of total attempts (including the first).
	// Defaults to 3 if zero.
	MaxAttempts int
	// BaseDelay is the initial backoff delay before the second attempt.
	// Defaults to 2 seconds if zero.
	BaseDelay time.Duration
	// MaxDelay caps the exponential backoff. Defaults to 30 seconds if zero.
	MaxDelay time.Duration
}

func (c TurnRetryConfig) maxAttempts() int {
	if c.MaxAttempts <= 0 {
		return 3
	}
	return c.MaxAttempts
}

func (c TurnRetryConfig) baseDelay() time.Duration {
	if c.BaseDelay <= 0 {
		return 2 * time.Second
	}
	return c.BaseDelay
}

func (c TurnRetryConfig) maxDelay() time.Duration {
	if c.MaxDelay <= 0 {
		return 30 * time.Second
	}
	return c.MaxDelay
}

// StreamFunc opens a new stream and returns its event channel.
// TurnRetry calls this once per attempt.
type StreamFunc func(ctx context.Context) (<-chan provider.StreamEvent, error)

// OnRetry is called before each retry attempt (not before the first attempt).
// Use it to emit telemetry events.
type OnRetry func(attempt int, delay time.Duration, cause error)

// TurnRetry calls fn up to cfg.MaxAttempts times. It retries only when fn
// itself returns a *provider.ProviderError whose IsRetryable() returns true.
// Mid-stream errors (errors emitted on the channel after fn returns) are NOT
// retried — by the time they appear, the caller may already have consumed
// partial events, so replay is unsafe.
//
// Returns the channel from the successful attempt, or the last error.
func TurnRetry(ctx context.Context, cfg TurnRetryConfig, fn StreamFunc, onRetry OnRetry) (<-chan provider.StreamEvent, error) {
	delay := cfg.baseDelay()
	var lastErr error

	for attempt := 1; attempt <= cfg.maxAttempts(); attempt++ {
		if attempt > 1 {
			if onRetry != nil {
				onRetry(attempt, delay, lastErr)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
			if delay > cfg.maxDelay() {
				delay = cfg.maxDelay()
			}
		}

		ch, err := fn(ctx)
		if err == nil {
			return ch, nil
		}
		lastErr = err

		if !isRetryableProviderError(err) || attempt == cfg.maxAttempts() {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func isRetryableProviderError(err error) bool {
	var pe *provider.ProviderError
	if errors.As(err, &pe) {
		return pe.IsRetryable()
	}
	return false
}
