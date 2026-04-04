package provider

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
