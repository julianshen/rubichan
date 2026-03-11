package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	m.EnsureFileOpen(ctx, client, tmpFile)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for didOpen")
	}

	// Second call should NOT send another notification.
	m.EnsureFileOpen(ctx, client, tmpFile)
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
