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
func TestTurnRetry_JitterProducesVariableDelays(t *testing.T) {
	var delays []time.Duration
	cfg := TurnRetryConfig{
		MaxAttempts: 5,
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    500 * time.Millisecond,
	}
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		return nil, &provider.ProviderError{Kind: provider.ErrRateLimited, Message: "429"}
	}
	onRetry := func(attempt int, delay time.Duration, cause error) {
		delays = append(delays, delay)
	}

	_, _ = TurnRetry(context.Background(), cfg, fn, onRetry)
	require.Len(t, delays, 4, "should have 4 retry callbacks for 5 attempts")

	allSame := true
	for i := 1; i < len(delays); i++ {
		if delays[i] != delays[0] {
			allSame = false
			break
		}
	}
	assert.False(t, allSame, "jitter should produce different delays across retries, got %v", delays)

	for _, d := range delays {
		assert.Greater(t, d, time.Duration(0), "delay must be positive")
	}
}

func TestTurnRetry_JitterAppliedToBaseDelay(t *testing.T) {
	base := 100 * time.Millisecond
	var firstDelay time.Duration
	cfg := TurnRetryConfig{
		MaxAttempts: 2,
		BaseDelay:   base,
		MaxDelay:    10 * time.Second,
	}
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		return nil, &provider.ProviderError{Kind: provider.ErrRateLimited, Message: "429"}
	}
	onRetry := func(attempt int, delay time.Duration, cause error) {
		if attempt == 2 {
			firstDelay = delay
		}
	}

	_, _ = TurnRetry(context.Background(), cfg, fn, onRetry)
	assert.Greater(t, firstDelay, time.Duration(0))
	assert.LessOrEqual(t, firstDelay, base+base/4, "first retry delay should be base + up to 25%% jitter, got %v", firstDelay)
}

func TestTurnRetry_JitterStaysWithinBounds(t *testing.T) {
	base := 100 * time.Millisecond
	maxD := 2 * time.Second
	var delays []time.Duration
	cfg := TurnRetryConfig{
		MaxAttempts: 8,
		BaseDelay:   base,
		MaxDelay:    maxD,
	}
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		return nil, &provider.ProviderError{Kind: provider.ErrRateLimited, Message: "429"}
	}
	onRetry := func(attempt int, delay time.Duration, cause error) {
		delays = append(delays, delay)
	}

	_, _ = TurnRetry(context.Background(), cfg, fn, onRetry)
	for _, d := range delays {
		assert.Greater(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, maxD+maxD/4, "delay with jitter should not exceed maxDelay + 25%%")
	}
}

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
