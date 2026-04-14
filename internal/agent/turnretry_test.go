package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnRetry_SucceedsFirstAttempt(t *testing.T) {
	called := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		called++
		ch := make(chan provider.StreamEvent, 1)
		ch <- provider.StreamEvent{Type: agentsdk.EventStop}
		close(ch)
		return ch, nil
	}

	ch, err := TurnRetry(context.Background(), TurnRetryConfig{BaseDelay: time.Millisecond}, fn, nil)
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, 1, called)
}

func TestTurnRetry_RetriesOnRetryableError(t *testing.T) {
	attempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		attempts++
		if attempts < 3 {
			return nil, &provider.ProviderError{
				Kind:    provider.ErrRateLimited,
				Message: "slow down",
			}
		}
		ch := make(chan provider.StreamEvent, 1)
		ch <- provider.StreamEvent{Type: agentsdk.EventStop}
		close(ch)
		return ch, nil
	}

	cfg := TurnRetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
	}
	var retryCalls []int
	onRetry := func(attempt int, delay time.Duration, cause error) {
		retryCalls = append(retryCalls, attempt)
	}

	ch, err := TurnRetry(context.Background(), cfg, fn, onRetry)
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, 3, attempts)
	assert.Equal(t, []int{2, 3}, retryCalls)
}

func TestTurnRetry_DoesNotRetryNonRetryable(t *testing.T) {
	attempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		attempts++
		return nil, &provider.ProviderError{
			Kind:    provider.ErrAuthFailed,
			Message: "bad api key",
		}
	}

	_, err := TurnRetry(context.Background(), TurnRetryConfig{BaseDelay: time.Millisecond}, fn, nil)
	require.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestTurnRetry_DoesNotRetryPlainError(t *testing.T) {
	attempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		attempts++
		return nil, errors.New("network refused")
	}

	_, err := TurnRetry(context.Background(), TurnRetryConfig{BaseDelay: time.Millisecond}, fn, nil)
	require.Error(t, err)
	assert.Equal(t, 1, attempts, "plain errors must not be retried")
}

func TestTurnRetry_ExhaustsAttempts(t *testing.T) {
	attempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		attempts++
		return nil, &provider.ProviderError{
			Kind:    provider.ErrServerError,
			Message: "upstream down",
		}
	}

	cfg := TurnRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	_, err := TurnRetry(context.Background(), cfg, fn, nil)
	require.Error(t, err)
	assert.Equal(t, 3, attempts)
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrServerError, pe.Kind)
}

func TestTurnRetry_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		attempts++
		if attempts == 1 {
			cancel() // cancel between first failure and retry delay
		}
		return nil, &provider.ProviderError{Kind: provider.ErrRateLimited, Message: "429"}
	}

	cfg := TurnRetryConfig{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond}
	_, err := TurnRetry(ctx, cfg, fn, nil)
	require.Error(t, err)
	// After cancel during delay, the err should be ctx.Err(), not the retry error.
	assert.ErrorIs(t, err, context.Canceled)
}

// When every streaming attempt fails with a retryable error and the
// caller has supplied a NonStream fallback, TurnRetry must invoke it
// once and replay its events on the returned channel in order. This
// is the "proxy that corrupts SSE" escape hatch documented in PR #236.
func TestTurnRetry_NonStreamFallbackAfterAllRetryableFailures(t *testing.T) {
	streamAttempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		streamAttempts++
		return nil, &provider.ProviderError{Kind: provider.ErrStreamError, Message: "stream stalled"}
	}

	fallbackCalls := 0
	want := []provider.StreamEvent{
		{Type: "message_start", Model: "m"},
		{Type: "text_delta", Text: "hello"},
		{Type: agentsdk.EventStop, StopReason: "end_turn"},
	}
	cfg := TurnRetryConfig{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		NonStreamFallback: func(ctx context.Context) ([]provider.StreamEvent, error) {
			fallbackCalls++
			return want, nil
		},
	}

	ch, err := TurnRetry(context.Background(), cfg, fn, nil)
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, 2, streamAttempts, "all streaming attempts must run before fallback")
	assert.Equal(t, 1, fallbackCalls, "fallback must be called exactly once")

	var got []provider.StreamEvent
	for e := range ch {
		got = append(got, e)
	}
	assert.Equal(t, want, got, "fallback events must be replayed in order, channel closed at end")
}

// Non-retryable errors (auth, 400, etc.) must NOT trigger NonStream —
// the fallback hits the same upstream validation and will fail the same
// way, wasting a round-trip and confusing logs. Short-circuit instead.
func TestTurnRetry_NonStreamFallbackSkippedOnNonRetryable(t *testing.T) {
	streamAttempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		streamAttempts++
		return nil, &provider.ProviderError{Kind: provider.ErrAuthFailed, Message: "bad api key"}
	}

	fallbackCalls := 0
	cfg := TurnRetryConfig{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		NonStreamFallback: func(ctx context.Context) ([]provider.StreamEvent, error) {
			fallbackCalls++
			return nil, nil
		},
	}

	_, err := TurnRetry(context.Background(), cfg, fn, nil)
	require.Error(t, err)
	assert.Equal(t, 1, streamAttempts, "non-retryable errors must short-circuit the retry loop")
	assert.Equal(t, 0, fallbackCalls, "non-retryable errors must NOT trigger NonStream fallback")
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrAuthFailed, pe.Kind)
}

// When streaming exhausts AND the fallback also fails, the caller should
// see the fallback's error — it's the most recent upstream response and
// is what the operator should investigate.
func TestTurnRetry_NonStreamFallbackError(t *testing.T) {
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		return nil, &provider.ProviderError{Kind: provider.ErrStreamError, Message: "sse corrupted"}
	}

	nonStreamErr := &provider.ProviderError{Kind: provider.ErrServerError, Message: "502 from proxy"}
	cfg := TurnRetryConfig{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		NonStreamFallback: func(ctx context.Context) ([]provider.StreamEvent, error) {
			return nil, nonStreamErr
		},
	}

	_, err := TurnRetry(context.Background(), cfg, fn, nil)
	require.Error(t, err)
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrServerError, pe.Kind,
		"returned error must reflect the NonStream failure, not the stream failure")
	assert.Contains(t, pe.Message, "502")
}

// Without a NonStreamFallback, TurnRetry must preserve its original
// behavior: return the last stream error after exhausting attempts.
func TestTurnRetry_NoFallbackConfigured(t *testing.T) {
	attempts := 0
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		attempts++
		return nil, &provider.ProviderError{Kind: provider.ErrServerError, Message: "503"}
	}

	cfg := TurnRetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond}
	_, err := TurnRetry(context.Background(), cfg, fn, nil)
	require.Error(t, err)
	assert.Equal(t, 2, attempts)
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrServerError, pe.Kind)
}

// If the context is cancelled by the time all streaming attempts are
// exhausted, the fallback must not be invoked — its work would just
// reveal the same ctx error. Return ctx.Err() directly.
func TestTurnRetry_NonStreamFallbackSkippedOnCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		// Fail retryably so we get to the fallback path.
		// Cancel between attempts so the ctx check at the top of the
		// fallback branch trips.
		cancel()
		return nil, &provider.ProviderError{Kind: provider.ErrStreamError, Message: "stall"}
	}

	fallbackCalls := 0
	cfg := TurnRetryConfig{
		MaxAttempts: 1, // one attempt → straight to fallback
		BaseDelay:   time.Millisecond,
		NonStreamFallback: func(ctx context.Context) ([]provider.StreamEvent, error) {
			fallbackCalls++
			return nil, nil
		},
	}

	_, err := TurnRetry(ctx, cfg, fn, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, fallbackCalls, "cancelled ctx must short-circuit the fallback")
}
