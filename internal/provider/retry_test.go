package provider

import (
	"bytes"
	"context"
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
