package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPFetcherSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from server"))
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(15 * time.Second)
	body, err := fetcher.Fetch(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, "hello from server", body)
}

func TestHTTPFetcherResponseSizeLimit(t *testing.T) {
	bigBody := strings.Repeat("x", 1024*1024+100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(bigBody))
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(15 * time.Second)
	body, err := fetcher.Fetch(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Len(t, body, 1<<20)
}

func TestHTTPFetcherConnectionError(t *testing.T) {
	fetcher := NewHTTPFetcher(2 * time.Second)
	_, err := fetcher.Fetch(context.Background(), "http://127.0.0.1:1/unreachable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch")
}

func TestHTTPFetcherHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(15 * time.Second)
	_, err := fetcher.Fetch(context.Background(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
