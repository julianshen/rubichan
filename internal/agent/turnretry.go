package agent

import (
	"context"
	"errors"
	"math/rand"
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
	// NonStreamFallback, if set, is called once after every streaming
	// attempt has failed with a retryable error. Its event slice is
	// replayed on a fresh channel that mirrors the streaming output,
	// giving callers a way out of environments where SSE is corrupted
	// end-to-end (proxies that strip or misframe chunks). Leave nil to
	// keep the pure streaming behavior and surface the stream error.
	NonStreamFallback NonStreamFunc
}

// NonStreamFunc builds a full response in a single non-streaming HTTP
// call and returns the equivalent StreamEvent slice. TurnRetry invokes
// this at most once, after all streaming attempts have failed with
// retryable errors.
type NonStreamFunc func(ctx context.Context) ([]provider.StreamEvent, error)

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

// NonStreamer is an optional interface providers may implement to expose
// a non-streaming fallback call. The agent loop detects this via type
// assertion and wires it into TurnRetryConfig.NonStreamFallback — only
// providers that implement it get the fallback benefit, the rest keep
// the pure streaming behavior. Today only *anthropic.Provider satisfies
// this; adding it elsewhere is a matter of defining a NonStream method
// with the same signature.
type NonStreamer interface {
	NonStream(ctx context.Context, req provider.CompletionRequest) ([]provider.StreamEvent, error)
}

// OnRetry is called before each retry attempt (not before the first attempt).
// Use it to emit telemetry events.
type OnRetry func(attempt int, delay time.Duration, cause error)

// TurnRetry calls fn up to cfg.MaxAttempts times. It retries only when fn
// itself returns a *provider.ProviderError whose IsRetryable() returns true.
// Mid-stream errors (errors emitted on the channel after fn returns) are NOT
// retried — by the time they appear, the caller may already have consumed
// partial events, so replay is unsafe.
//
// If every streaming attempt exhausts with retryable errors and cfg has a
// NonStreamFallback set, that function is called one more time; its slice
// is replayed on a fresh channel that mirrors the streaming output. This
// is the final escape hatch for proxies that corrupt SSE mid-stream.
// Non-retryable errors (auth, 400, etc.) short-circuit immediately and do
// NOT trigger the fallback — NonStream wouldn't fix them.
//
// Returns the channel from the successful attempt, or the last error.
func TurnRetry(ctx context.Context, cfg TurnRetryConfig, fn StreamFunc, onRetry OnRetry) (<-chan provider.StreamEvent, error) {
	delay := cfg.baseDelay()
	var lastErr error

	for attempt := 1; attempt <= cfg.maxAttempts(); attempt++ {
		if attempt > 1 {
			jittered := addJitter(delay)
			if onRetry != nil {
				onRetry(attempt, jittered, lastErr)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(jittered):
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

		if !isRetryableProviderError(err) {
			// Non-retryable: bail immediately. NonStream won't rescue
			// auth failures or 400s — they'll hit the same upstream
			// validation path.
			return nil, lastErr
		}
	}

	// All streaming attempts exhausted on retryable errors.
	if cfg.NonStreamFallback != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		events, nsErr := cfg.NonStreamFallback(ctx)
		if nsErr == nil {
			return eventsToChannel(events), nil
		}
		// NonStream also failed — return its error since it reflects
		// the most recent attempt and is what the operator should act on.
		return nil, nsErr
	}
	return nil, lastErr
}

// eventsToChannel replays a slice of StreamEvents on a fresh buffered
// channel so downstream consumers that expect a <-chan interface can be
// fed a non-streaming response without knowing the difference.
func eventsToChannel(events []provider.StreamEvent) <-chan provider.StreamEvent {
	ch := make(chan provider.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func isRetryableProviderError(err error) bool {
	var pe *provider.ProviderError
	if errors.As(err, &pe) {
		return pe.IsRetryable()
	}
	return false
}

func addJitter(d time.Duration) time.Duration {
	jitter := time.Duration(rand.Float64() * 0.25 * float64(d))
	return d + jitter
}
