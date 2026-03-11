package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestManager creates a Manager with a mock registry and pre-populated mock server.
// Returns the manager and the server side of the mock transport for writing responses.
func newTestManager(t *testing.T) (*Manager, *mockTransport) {
	t.Helper()

	reg := NewRegistry()
	m := NewManager(reg, "/test/workspace")

	mt := newMockTransport()
	client := NewClient(mt.client, func(method string, params json.RawMessage) {
		if method == "textDocument/publishDiagnostics" {
			var p PublishDiagnosticsParams
			if err := json.Unmarshal(params, &p); err == nil {
				m.diagMu.Lock()
				m.diags[p.URI] = p.Diagnostics
				m.diagMu.Unlock()
			}
		}
	})

	m.servers["go"] = &serverHandle{
		client: client,
		capabilities: ServerCapabilities{
			DefinitionProvider: true,
			HoverProvider:      true,
			ReferencesProvider: true,
		},
	}

	t.Cleanup(func() {
		client.Close()
		mt.server.Close()
	})

	return m, mt
}

func TestManagerDiagnosticsForReturnsCopy(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	uri := pathToURI("/test/main.go")
	m.diagMu.Lock()
	m.diags[uri] = []Diagnostic{
		{Severity: SeverityError, Message: "error1"},
		{Severity: SeverityWarning, Message: "warning1"},
	}
	m.diagMu.Unlock()

	// Get a copy and modify it.
	result := m.DiagnosticsFor(uri, true)
	require.Len(t, result, 2)
	result[0].Message = "MODIFIED"

	// Original should be unchanged.
	m.diagMu.RLock()
	assert.Equal(t, "error1", m.diags[uri][0].Message)
	m.diagMu.RUnlock()
}

func TestManagerDiagnosticsForErrorsOnly(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	uri := pathToURI("/test/main.go")
	m.diagMu.Lock()
	m.diags[uri] = []Diagnostic{
		{Severity: SeverityError, Message: "err"},
		{Severity: SeverityWarning, Message: "warn"},
		{Severity: SeverityError, Message: "err2"},
		{Severity: SeverityHint, Message: "hint"},
	}
	m.diagMu.Unlock()

	result := m.DiagnosticsFor(uri, false)
	require.Len(t, result, 2)
	assert.Equal(t, "err", result[0].Message)
	assert.Equal(t, "err2", result[1].Message)
}

func TestManagerDiagnosticsForEmpty(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	result := m.DiagnosticsFor("file:///nonexistent.go", true)
	assert.Empty(t, result)
}

func TestManagerServerForCachesHandle(t *testing.T) {
	m, _ := newTestManager(t)

	ctx := context.Background()
	client1, caps1, err := m.ServerFor(ctx, "go")
	require.NoError(t, err)
	require.NotNil(t, client1)
	require.NotNil(t, caps1)

	// Second call returns the same client.
	client2, _, err := m.ServerFor(ctx, "go")
	require.NoError(t, err)
	assert.Same(t, client1, client2)
}

func TestManagerServerForUnknownLanguage(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	_, _, err := m.ServerFor(context.Background(), "brainfuck")
	assert.ErrorIs(t, err, ErrNoConfig)
}

func TestManagerServerForNotInstalled(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		return "", fmt.Errorf("not found: %s", name)
	}
	m := NewManager(reg, "/test")

	_, _, err := m.ServerFor(context.Background(), "go")
	assert.ErrorIs(t, err, ErrServerNotInstalled)
}

func TestManagerServerForFile(t *testing.T) {
	m, _ := newTestManager(t)

	client, _, err := m.ServerForFile(context.Background(), "/test/main.go")
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestManagerServerForFileUnknownExt(t *testing.T) {
	m, _ := newTestManager(t)

	_, _, err := m.ServerForFile(context.Background(), "/test/Makefile")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no language server configured")
}

func TestManagerEnsureFileOpenIdempotent(t *testing.T) {
	m, mt := newTestManager(t)
	ctx := context.Background()

	// Create a temp file for os.ReadFile to succeed.
	tmpFile := createTempFile(t, "test.go", "package main\n")

	// Server goroutine reads the notification.
	var receivedCount int
	var mu sync.Mutex
	done := make(chan struct{}, 2)

	go func() {
		for {
			_, err := readRequest(mt.server)
			if err != nil {
				return
			}
			mu.Lock()
			receivedCount++
			mu.Unlock()
			done <- struct{}{}
		}
	}()

	client := m.servers["go"].client

	// First call should send didOpen.
	require.NoError(t, m.EnsureFileOpen(ctx, client, tmpFile))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for didOpen")
	}

	// Second call should NOT send another notification.
	require.NoError(t, m.EnsureFileOpen(ctx, client, tmpFile))
	// Give a short window for any unexpected notification.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, receivedCount, "didOpen should only be sent once")
	mu.Unlock()
}

func TestManagerShutdown(t *testing.T) {
	m, mt := newTestManager(t)

	// Server goroutine handles shutdown request and exit notification.
	go func() {
		// Read shutdown request.
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, nil)
		// Read exit notification.
		_, _ = readRequest(mt.server)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := m.Shutdown(ctx)
	assert.NoError(t, err)

	m.mu.Lock()
	assert.Empty(t, m.servers)
	m.mu.Unlock()
}

func TestManagerNotifyFileChanged(t *testing.T) {
	m, mt := newTestManager(t)
	ctx := context.Background()

	// Read the didOpen notification.
	done := make(chan jsonrpcRequest, 2)
	go func() {
		for {
			req, err := readRequest(mt.server)
			if err != nil {
				return
			}
			done <- req
		}
	}()

	err := m.NotifyFileChanged(ctx, "/test/main.go", []byte("package main\n"))
	require.NoError(t, err)

	select {
	case req := <-done:
		assert.Equal(t, "textDocument/didOpen", req.Method)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for didOpen")
	}

	// Second call should send didChange.
	err = m.NotifyFileChanged(ctx, "/test/main.go", []byte("package main\nfunc main() {}\n"))
	require.NoError(t, err)

	select {
	case req := <-done:
		assert.Equal(t, "textDocument/didChange", req.Method)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for didChange")
	}
}

func TestManagerNotifyFileChangedUnknownExt(t *testing.T) {
	m, _ := newTestManager(t)

	// Should silently skip files with unknown extensions.
	err := m.NotifyFileChanged(context.Background(), "/test/Makefile", []byte("all:\n\tgo build\n"))
	assert.NoError(t, err)
}

func TestServerForConcurrentInit(t *testing.T) {
	// Multiple goroutines calling ServerFor for the same language should
	// only spawn one server. The serverInit barrier serializes them.
	reg := NewRegistry()
	reg.lookPath = func(string) (string, error) { return "/usr/bin/gopls", nil }
	m := NewManager(reg, "/test")

	var spawnCount int
	var spawnMu sync.Mutex
	m.spawnServer = func(_ ServerConfig) (io.ReadWriteCloser, error) {
		spawnMu.Lock()
		spawnCount++
		spawnMu.Unlock()
		// Simulate slow startup.
		time.Sleep(50 * time.Millisecond)
		mt := newMockTransport()
		// Respond to initialize and drain the initialized notification.
		go func() {
			raw, _ := readRequest(mt.server)
			var initResult InitializeResult
			initResult.Capabilities.DefinitionProvider = true
			_ = writeResponse(mt.server, raw.ID, initResult)
			// Drain the "initialized" notification that startServer sends.
			_, _ = readRequest(mt.server)
		}()
		return mt.client, nil
	}

	const goroutines = 5
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = m.ServerFor(context.Background(), "go")
		}()
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d", i)
	}

	spawnMu.Lock()
	assert.Equal(t, 1, spawnCount, "server should be spawned exactly once")
	spawnMu.Unlock()
}

func TestServerForConcurrentInitFailure(t *testing.T) {
	// When startServer fails, all waiting goroutines should receive the error.
	reg := NewRegistry()
	reg.lookPath = func(string) (string, error) { return "/usr/bin/gopls", nil }
	m := NewManager(reg, "/test")

	m.spawnServer = func(_ ServerConfig) (io.ReadWriteCloser, error) {
		time.Sleep(50 * time.Millisecond)
		return nil, fmt.Errorf("spawn failed")
	}

	const goroutines = 5
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = m.ServerFor(context.Background(), "go")
		}()
	}
	wg.Wait()

	for i, err := range errs {
		assert.Error(t, err, "goroutine %d should get an error", i)
		assert.Contains(t, err.Error(), "spawn failed", "goroutine %d", i)
	}

	// Server should not be cached after failure.
	m.mu.Lock()
	_, cached := m.servers["go"]
	_, starting := m.starting["go"]
	m.mu.Unlock()
	assert.False(t, cached, "failed server should not be cached")
	assert.False(t, starting, "starting entry should be cleaned up")
}

func TestServerForAfterShutdownReturnsError(t *testing.T) {
	// After Shutdown, ServerFor must return ErrManagerShutdown immediately.
	m, mt := newTestManager(t)

	// Handle the shutdown/exit for the pre-existing "go" server.
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, nil)
		_, _ = readRequest(mt.server) // exit
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, m.Shutdown(ctx))

	// Now ServerFor should be rejected.
	_, _, err := m.ServerFor(ctx, "go")
	assert.ErrorIs(t, err, ErrManagerShutdown)
}

func TestServerForMidFlightShutdownClosesOrphan(t *testing.T) {
	// If Shutdown runs while ServerFor is inside startServer (mu released),
	// the newly-created server should be closed rather than stored in m.servers.
	reg := NewRegistry()
	reg.lookPath = func(string) (string, error) { return "/usr/bin/gopls", nil }
	m := NewManager(reg, "/test")

	spawnReady := make(chan struct{}) // signals spawn has started
	spawnGo := make(chan struct{})    // lets spawn proceed to completion

	m.spawnServer = func(_ ServerConfig) (io.ReadWriteCloser, error) {
		close(spawnReady) // signal that we're inside startServer
		<-spawnGo         // wait for test to run Shutdown
		mt := newMockTransport()
		go func() {
			raw, _ := readRequest(mt.server)
			_ = writeResponse(mt.server, raw.ID, InitializeResult{
				Capabilities: ServerCapabilities{DefinitionProvider: true},
			})
			_, _ = readRequest(mt.server) // initialized
		}()
		return mt.client, nil
	}

	// Launch ServerFor in background.
	type result struct {
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		_, _, err := m.ServerFor(context.Background(), "go")
		resultCh <- result{err: err}
	}()

	// Wait for ServerFor to enter startServer (mu is released).
	<-spawnReady

	// Run Shutdown while ServerFor is blocked. Use a background ctx so
	// Shutdown waits for the init barrier if needed.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- m.Shutdown(shutdownCtx)
	}()

	// Let the spawn proceed. ServerFor will finish startServer, but by now
	// m.closed is true — it should close the new client and return an error.
	close(spawnGo)

	// Wait for both ServerFor and Shutdown to complete.
	res := <-resultCh
	assert.ErrorIs(t, res.err, ErrManagerShutdown, "ServerFor should return ErrManagerShutdown")

	err := <-shutdownDone
	assert.NoError(t, err)

	// The server map should be empty — the orphaned handle was NOT stored.
	m.mu.Lock()
	assert.Empty(t, m.servers, "no servers should remain after mid-flight shutdown")
	m.mu.Unlock()
}

func TestManagerSetSummarizer(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	s := &Summarizer{MaxReferences: 10}
	m.SetSummarizer(s)
	assert.Equal(t, 10, m.summarizer.MaxReferences)
}

func TestManagerDiagnosticsUpdatedViaNotification(t *testing.T) {
	m, mt := newTestManager(t)

	// Send publishDiagnostics notification from server.
	go func() {
		_ = writeNotification(mt.server, "textDocument/publishDiagnostics", PublishDiagnosticsParams{
			URI: "file:///test/main.go",
			Diagnostics: []Diagnostic{
				{Severity: SeverityError, Message: "syntax error"},
			},
		})
	}()

	// Wait for the notification to be processed.
	require.Eventually(t, func() bool {
		diags := m.DiagnosticsFor("file:///test/main.go", true)
		return len(diags) == 1
	}, 2*time.Second, 10*time.Millisecond)

	diags := m.DiagnosticsFor("file:///test/main.go", true)
	assert.Equal(t, "syntax error", diags[0].Message)
}

// createTempFile creates a temp file in t.TempDir() with the given content.
func createTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/" + name
	require.NoError(t, writeTestFile(path, content))
	return path
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestManagerStartServerViaSpawnFunc(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}

	m := NewManager(reg, "/test/workspace")

	// Inject a mock spawnServer that returns a pipe transport.
	mt := newMockTransport()
	m.spawnServer = func(cfg ServerConfig) (io.ReadWriteCloser, error) {
		return mt.client, nil
	}

	// Server goroutine: handle initialize, initialized, then shutdown/exit.
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		initResult := InitializeResult{
			Capabilities: ServerCapabilities{
				DefinitionProvider: true,
				HoverProvider:      true,
			},
		}
		_ = writeResponse(mt.server, req.ID, initResult)
		// Read initialized notification.
		_, _ = readRequest(mt.server)
		// Handle shutdown request.
		req, err = readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, nil)
		// Read exit notification.
		_, _ = readRequest(mt.server)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, caps, err := m.ServerFor(ctx, "go")
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.True(t, caps.DefinitionProvider)
	assert.True(t, caps.HoverProvider)

	// Cleanup.
	require.NoError(t, m.Shutdown(ctx))
	mt.server.Close()
}

func TestManagerStartServerSpawnError(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}

	m := NewManager(reg, "/test/workspace")
	m.spawnServer = func(cfg ServerConfig) (io.ReadWriteCloser, error) {
		return nil, fmt.Errorf("binary crashed")
	}

	_, _, err := m.ServerFor(context.Background(), "go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary crashed")
}

func TestManagerEnsureFileOpenReturnsError(t *testing.T) {
	m, _ := newTestManager(t)
	client := m.servers["go"].client

	// Try to open a file that doesn't exist.
	err := m.EnsureFileOpen(context.Background(), client, "/nonexistent/file.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read file for didOpen")

	// Verify the file was NOT marked as opened.
	uri := pathToURI("/nonexistent/file.go")
	m.docsMu.Lock()
	_, opened := m.docs[uri]
	m.docsMu.Unlock()
	assert.False(t, opened)
}

func TestManagerShutdownJoinsErrors(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	// Create two mock servers that will both fail on Close.
	mt1 := newMockTransport()
	mt2 := newMockTransport()

	// Close the client sides first to make the real Close fail.
	mt1.client.Close()
	mt2.client.Close()

	client1 := NewClient(mt1.client, nil)
	client2 := NewClient(mt2.client, nil)

	m.servers["lang1"] = &serverHandle{client: client1}
	m.servers["lang2"] = &serverHandle{client: client2}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Shutdown should not panic and should complete.
	_ = m.Shutdown(ctx)

	m.mu.Lock()
	assert.Empty(t, m.servers)
	m.mu.Unlock()

	mt1.server.Close()
	mt2.server.Close()
}

func TestClientErrorHandler(t *testing.T) {
	mt := newMockTransport()

	var capturedErr error
	var mu sync.Mutex
	errReceived := make(chan struct{})

	client := newClient(mt.client, nil, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		capturedErr = err
		close(errReceived)
	})
	defer client.Close()

	// Send malformed JSON.
	go func() {
		msg := "Content-Length: 5\r\n\r\n{bad}"
		_, _ = mt.server.Write([]byte(msg))
	}()

	select {
	case <-errReceived:
		mu.Lock()
		assert.Contains(t, capturedErr.Error(), "malformed message")
		mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error handler")
	}

	mt.server.Close()
}

func TestClientReadErrStoredInDrainPending(t *testing.T) {
	mt := newMockTransport()

	client := NewClient(mt.client, nil)

	// Start a call that will block.
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Call(context.Background(), "test/blocked", nil)
		errCh <- err
	}()

	// Wait for the request to be sent, then close server.
	_, _ = readRequest(mt.server)
	mt.server.Close()

	select {
	case err := <-errCh:
		require.Error(t, err)
		// May unblock via drainPending ("transport closed: ...") or via done channel ("client closed").
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "transport closed") || strings.Contains(errMsg, "client closed"),
			"unexpected error: %s", errMsg)
	case <-time.After(2 * time.Second):
		t.Fatal("Call was not unblocked")
	}

	// Verify the readErr was stored.
	stored := client.readErr.Load()
	assert.NotNil(t, stored, "readErr should be stored from transport failure")

	client.Close()
}

func TestManagerStartServerInitializeError(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}
	m := NewManager(reg, "/test")

	mt := newMockTransport()
	m.spawnServer = func(cfg ServerConfig) (io.ReadWriteCloser, error) {
		return mt.client, nil
	}

	// Server returns an error for initialize.
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		resp := struct {
			JSONRPC string       `json:"jsonrpc"`
			ID      int64        `json:"id"`
			Error   jsonrpcError `json:"error"`
		}{JSONRPC: "2.0", ID: req.ID, Error: jsonrpcError{Code: -32600, Message: "initialize failed"}}
		body, _ := json.Marshal(resp)
		fmt.Fprintf(mt.server, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := m.ServerFor(ctx, "go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialize")

	mt.server.Close()
}

func TestManagerStartServerBadInitResult(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}
	m := NewManager(reg, "/test")

	mt := newMockTransport()
	m.spawnServer = func(cfg ServerConfig) (io.ReadWriteCloser, error) {
		return mt.client, nil
	}

	// Server returns non-JSON for initialize result.
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, "not-a-valid-init-result")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := m.ServerFor(ctx, "go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse initialize result")

	mt.server.Close()
}

func TestNewClientNilRwcPanics(t *testing.T) {
	assert.PanicsWithValue(t, "lsp.NewClient: rwc must not be nil", func() {
		NewClient(nil, nil)
	})
}

func TestClientDispatchServerRequest(t *testing.T) {
	mt := newMockTransport()

	var capturedErr error
	var mu sync.Mutex
	errReceived := make(chan struct{})

	client := newClient(mt.client, nil, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		capturedErr = err
		select {
		case <-errReceived:
		default:
			close(errReceived)
		}
	})
	defer client.Close()

	// Send a server-to-client request (has both ID and Method).
	go func() {
		msg := struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int64  `json:"id"`
			Method  string `json:"method"`
		}{JSONRPC: "2.0", ID: 42, Method: "window/showMessageRequest"}
		body, _ := json.Marshal(msg)
		fmt.Fprintf(mt.server, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}()

	select {
	case <-errReceived:
		mu.Lock()
		assert.Contains(t, capturedErr.Error(), "unsupported server request")
		assert.Contains(t, capturedErr.Error(), "window/showMessageRequest")
		mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error handler")
	}

	mt.server.Close()
}

func TestManagerPublishDiagnosticsUnmarshalError(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}
	m := NewManager(reg, "/test")

	var capturedErr error
	var mu sync.Mutex
	errReceived := make(chan struct{})
	m.onError = func(err error) {
		mu.Lock()
		defer mu.Unlock()
		capturedErr = err
		select {
		case <-errReceived:
		default:
			close(errReceived)
		}
	}

	mt := newMockTransport()
	m.spawnServer = func(cfg ServerConfig) (io.ReadWriteCloser, error) {
		return mt.client, nil
	}

	// Server goroutine: handle initialize, initialized, then send malformed diagnostics.
	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, InitializeResult{
			Capabilities: ServerCapabilities{DefinitionProvider: true},
		})
		_, _ = readRequest(mt.server) // initialized

		// Send malformed publishDiagnostics notification.
		_ = writeNotification(mt.server, "textDocument/publishDiagnostics", "not-valid-json-params")

		// Handle shutdown/exit.
		req, err = readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, nil)
		_, _ = readRequest(mt.server)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := m.ServerFor(ctx, "go")
	require.NoError(t, err)

	select {
	case <-errReceived:
		mu.Lock()
		assert.Contains(t, capturedErr.Error(), "publishDiagnostics unmarshal")
		mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for publishDiagnostics error")
	}

	require.NoError(t, m.Shutdown(ctx))
	mt.server.Close()
}

func TestManagerNotifyFileChangedPropagatesError(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		return "", fmt.Errorf("not installed")
	}
	m := NewManager(reg, "/test")

	// NotifyFileChanged for a Go file should now return the server error.
	err := m.NotifyFileChanged(context.Background(), "/test/main.go", []byte("package main"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server for go")
}
