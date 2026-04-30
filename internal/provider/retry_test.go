package provider

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoWithRetry_RetriesTransientStatus(t *testing.T) {
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay, retryMaxDelay = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() {
		retryBaseDelay, retryMaxDelay = oldBase, oldMax
	})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporary outage"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, bytes.NewReader([]byte("{}")))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := DoWithRetry(context.Background(), &http.Client{}, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 3, calls.Load())
}

func TestDoWithRetry_DoesNotRetryPermanentStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	resp, err := DoWithRetry(context.Background(), &http.Client{}, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.EqualValues(t, 1, calls.Load())
}

func TestDoWithRetry_DoesNotRetryNonTransientClientErrors(t *testing.T) {
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://invalid.local", nil)
	require.NoError(t, err)

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("permanent client error")
	})}

	_, err = DoWithRetry(ctx, client, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permanent client error")
}

func TestDoWithRetry_RetriesTransientNetworkErrors(t *testing.T) {
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay, retryMaxDelay = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() {
		retryBaseDelay, retryMaxDelay = oldBase, oldMax
	})

	var calls atomic.Int32
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) < 3 {
			return nil, &net.DNSError{IsTemporary: true}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	})}

	resp, err := DoWithRetry(ctx, client, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 3, calls.Load())
}

func TestParseRetryAfter(t *testing.T) {
	d, ok := parseRetryAfter("2")
	require.True(t, ok)
	assert.Equal(t, 2*time.Second, d)

	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	d, ok = parseRetryAfter(future)
	require.True(t, ok)
	assert.Greater(t, d, time.Duration(0))

	_, ok = parseRetryAfter("-1")
	assert.False(t, ok)

	_, ok = parseRetryAfter("n/a")
	assert.False(t, ok)
}

func TestDoWithRetry_UsesRetryAfterHeader(t *testing.T) {
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay, retryMaxDelay = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() {
		retryBaseDelay, retryMaxDelay = oldBase, oldMax
	})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, bytes.NewReader([]byte("{}")))
	require.NoError(t, err)

	start := time.Now()
	resp, err := DoWithRetry(context.Background(), &http.Client{}, req)
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 3, calls.Load())
	// Should have waited at least ~1s for the Retry-After header (2 retries * 1s).
	// Use a lower bound to account for timing variance.
	assert.Greater(t, elapsed, 500*time.Millisecond)
}

func TestDoWithRetry_SkipsRetryOnAuthFailed(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	resp, err := DoWithRetry(context.Background(), &http.Client{}, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.EqualValues(t, 1, calls.Load(), "should not retry auth failures")
}

func TestDoWithRetry_SkipsRetryOnContextOverflow(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"error":{"message":"request too large"}}`))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	resp, err := DoWithRetry(context.Background(), &http.Client{}, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	assert.EqualValues(t, 1, calls.Load(), "should not retry context overflow")
}

func TestRetryDelay_UsesJitter(t *testing.T) {
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay, retryMaxDelay = 100*time.Millisecond, 10*time.Second
	t.Cleanup(func() {
		retryBaseDelay, retryMaxDelay = oldBase, oldMax
	})

	var delays []time.Duration
	for i := 0; i < 20; i++ {
		delays = append(delays, retryDelay(1, retryBaseDelay, retryMaxDelay))
	}
	allSame := true
	for i := 1; i < len(delays); i++ {
		if delays[i] != delays[0] {
			allSame = false
			break
		}
	}
	assert.False(t, allSame, "jitter should produce different delays across calls, got %v", delays)

	for _, d := range delays {
		assert.Greater(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, retryBaseDelay+retryBaseDelay/4, "delay should be base + up to 25%% jitter")
	}
}

func TestRetryDelay_CappedAtMaxWithJitter(t *testing.T) {
	oldBase, oldMax := retryBaseDelay, retryMaxDelay
	retryBaseDelay = 100 * time.Millisecond
	retryMaxDelay = 200 * time.Millisecond
	t.Cleanup(func() {
		retryBaseDelay, retryMaxDelay = oldBase, oldMax
	})

	for i := 0; i < 20; i++ {
		d := retryDelay(5, retryBaseDelay, retryMaxDelay)
		assert.GreaterOrEqual(t, d, retryMaxDelay, "delay should be at least maxDelay, got %v", d)
		assert.LessOrEqual(t, d, retryMaxDelay+retryMaxDelay/4, "delay should not exceed maxDelay + 25%% jitter, got %v", d)
	}
}

func TestDoWithRetryConfig_Background_FailFast(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}),
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_, err := DoWithRetryConfig(context.Background(), client, req, RetryConfig{Context: RetryBackground})
	require.Error(t, err)
	assert.EqualValues(t, 1, calls.Load(), "background should not retry")
}

func TestDoWithRetryConfig_Foreground_Retries(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}),
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_, err := DoWithRetryConfig(context.Background(), client, req, RetryConfig{Context: RetryForeground})
	require.Error(t, err)
	assert.EqualValues(t, 3, calls.Load(), "foreground should retry")
}

func TestDoWithRetryConfig_CustomMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}),
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_, err := DoWithRetryConfig(context.Background(), client, req, RetryConfig{MaxAttempts: 5})
	require.Error(t, err)
	assert.EqualValues(t, 5, calls.Load(), "custom max attempts should be respected")
}

func TestDoWithRetryConfig_CustomDelays(t *testing.T) {
	var delays []time.Duration
	var mu sync.Mutex
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}),
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	start := time.Now()
	DoWithRetryConfig(context.Background(), client, req, RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	})
	elapsed := time.Since(start)
	mu.Lock()
	_ = delays
	mu.Unlock()
	assert.Less(t, elapsed, 500*time.Millisecond, "custom delays should be faster than default")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
