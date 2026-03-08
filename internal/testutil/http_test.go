package testutil

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	resp, err := http.DefaultClient.Get(server.URL + "/ping")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(server.URL, "mem://"), "URL should use mem:// scheme")
	assert.Equal(t, "ok", string(body))
}

func TestNewServer_URLScheme(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	require.True(t, strings.HasPrefix(server.URL, "mem://"))
}

func TestNewServer_Client(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Client() returns http.DefaultClient because mem:// is on DefaultTransport.
	assert.Equal(t, http.DefaultClient, server.Client())
}

func TestRoundTrip_ServerNotFound(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	server.Close() // remove handler

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	_, err = http.DefaultClient.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRoundTrip_ContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		// Block until context is cancelled.
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		_, err := http.DefaultClient.Do(req) //nolint:bodyclose
		errCh <- err
	}()

	<-started // wait for handler to start
	cancel()  // cancel the context

	err = <-errCh
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled"))
}

func TestRoundTrip_HandlerPanic(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write header first so the response is sent before panic.
		w.WriteHeader(http.StatusOK)
		// Flush ensures the response writer has sent headers.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		panic("boom")
	}))

	resp, err := http.DefaultClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Body read should return an error from the panic recovery.
	_, err = io.ReadAll(resp.Body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler panic")
}

func TestRoundTrip_NonOKStatus(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not here")
	}))

	resp, err := http.DefaultClient.Get(server.URL + "/missing")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "not here", string(body))
}

func TestRoundTrip_WriteHeaderIdempotent(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError) // second call ignored
		_, _ = io.WriteString(w, "created")
	}))

	resp, err := http.DefaultClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// First WriteHeader wins.
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestRoundTrip_ImplicitOKStatus(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write body without calling WriteHeader — should implicitly set 200.
		_, _ = io.WriteString(w, "implicit ok")
	}))

	resp, err := http.DefaultClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "implicit ok", string(body))
}

func TestRoundTrip_ResponseHeaders(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "test-value")
		w.WriteHeader(http.StatusOK)
	}))

	resp, err := http.DefaultClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "test-value", resp.Header.Get("X-Custom"))
}

func TestRoundTrip_ConcurrentRequests(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	}))

	const n = 10
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := http.DefaultClient.Get(server.URL + "/" + strings.Repeat("x", idx+1))
			if err != nil {
				errs[idx] = err
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errs[idx] = err
				return
			}
			results[idx] = string(body)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "request %d failed", i)
		expected := "/" + strings.Repeat("x", i+1)
		assert.Equal(t, expected, results[i], "request %d body mismatch", i)
	}
}

func TestRoundTrip_Flusher(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		require.True(t, ok, "ResponseWriter should implement http.Flusher")
		_, _ = io.WriteString(w, "part1")
		f.Flush() // no-op but should not panic
		_, _ = io.WriteString(w, "part2")
	}))

	resp, err := http.DefaultClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "part1part2", string(body))
}

func TestMemoryBody_WriteAfterClose(t *testing.T) {
	body := newMemoryBody()
	require.NoError(t, body.Close())

	_, err := body.Write([]byte("data"))
	assert.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestMemoryBody_CloseWithError(t *testing.T) {
	body := newMemoryBody()
	testErr := errors.New("test error")
	require.NoError(t, body.CloseWithError(testErr))

	_, err := body.Read(make([]byte, 10))
	assert.Equal(t, testErr, err)
}

func TestMemoryBody_WriteAfterCloseWithError(t *testing.T) {
	body := newMemoryBody()
	testErr := errors.New("custom error")
	require.NoError(t, body.CloseWithError(testErr))

	// Write after CloseWithError should return the custom error.
	_, err := body.Write([]byte("data"))
	assert.Equal(t, testErr, err)
}

func TestMemoryBody_DoubleClose(t *testing.T) {
	body := newMemoryBody()
	require.NoError(t, body.Close())
	// Second close is a no-op.
	require.NoError(t, body.CloseWithError(errors.New("ignored")))
}

func TestMemoryBody_ReadBlocksUntilWrite(t *testing.T) {
	body := newMemoryBody()

	readDone := make(chan struct{})
	var readData []byte
	var readErr error

	go func() {
		buf := make([]byte, 10)
		n, err := body.Read(buf)
		readData = buf[:n]
		readErr = err
		close(readDone)
	}()

	// Ensure reader is blocked.
	select {
	case <-readDone:
		t.Fatal("Read should block until data is available")
	case <-time.After(50 * time.Millisecond):
		// Expected: Read is still waiting.
	}

	_, err := body.Write([]byte("hello"))
	require.NoError(t, err)

	<-readDone
	require.NoError(t, readErr)
	assert.Equal(t, "hello", string(readData))
}

func TestMemoryBody_ReadReturnsEOFOnClose(t *testing.T) {
	body := newMemoryBody()
	require.NoError(t, body.Close())

	_, err := body.Read(make([]byte, 10))
	assert.ErrorIs(t, err, io.EOF)
}

func TestNewServer_CleanupRegistered(t *testing.T) {
	// Verify that t.Cleanup is registered by observing that Close is idempotent.
	// After the test, the handler should be removed automatically.
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Manual close should succeed and be idempotent with cleanup.
	server.Close()
	server.Close() // no panic on double close
}
