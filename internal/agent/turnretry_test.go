package agent

import (
	"context"
	"errors"
	"sync/atomic"
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
	var attempts int32
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
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
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
	assert.Equal(t, []int{2, 3}, retryCalls)
}

func TestTurnRetry_DoesNotRetryNonRetryable(t *testing.T) {
	var attempts int32
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, &provider.ProviderError{
			Kind:    provider.ErrAuthFailed,
			Message: "bad api key",
		}
	}

	_, err := TurnRetry(context.Background(), TurnRetryConfig{BaseDelay: time.Millisecond}, fn, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
}

func TestTurnRetry_DoesNotRetryPlainError(t *testing.T) {
	var attempts int32
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, errors.New("network refused")
	}

	_, err := TurnRetry(context.Background(), TurnRetryConfig{BaseDelay: time.Millisecond}, fn, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "plain errors must not be retried")
}

func TestTurnRetry_ExhaustsAttempts(t *testing.T) {
	var attempts int32
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, &provider.ProviderError{
			Kind:    provider.ErrServerError,
			Message: "upstream down",
		}
	}

	cfg := TurnRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	_, err := TurnRetry(context.Background(), cfg, fn, nil)
	require.Error(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrServerError, pe.Kind)
}

func TestTurnRetry_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts int32
	fn := func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
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
