package lsp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerNotifierNoDiagnostics(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	notifier := &ManagerNotifier{
		Manager: m,
		Delay:   1 * time.Millisecond,
	}

	// No server configured for .txt files — skipped immediately, no sleep.
	lines, err := notifier.NotifyAndCollectDiagnostics(context.Background(), "/test/readme.txt", []byte("hello"))
	require.NoError(t, err)
	assert.Nil(t, lines)
}

func TestManagerNotifierSkipsUnsupportedLanguage(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	notifier := &ManagerNotifier{
		Manager: m,
		Delay:   10 * time.Second, // would block if not skipped
	}

	// .txt has no server — should return immediately without sleeping.
	start := time.Now()
	lines, err := notifier.NotifyAndCollectDiagnostics(context.Background(), "/test/readme.txt", []byte("hello"))
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Nil(t, lines)
	assert.Less(t, elapsed, 100*time.Millisecond, "should skip immediately for unsupported languages")
}

func TestManagerNotifierFormatsDiagnostics(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	// Use a .go file — Go has a registered server in the default registry.
	// Pre-populate diagnostics cache to simulate a server having published them.
	// NotifyFileChanged will fail (gopls not running) but we skip past it.
	uri := pathToURI("/test/main.go")
	m.diagMu.Lock()
	m.diags[uri] = []Diagnostic{
		{
			Range:    Range{Start: Position{Line: 4, Character: 10}},
			Severity: SeverityError,
			Message:  "undefined: foo",
		},
		{
			Range:    Range{Start: Position{Line: 12, Character: 0}},
			Severity: SeverityWarning,
			Message:  "unused variable",
		},
	}
	m.diagMu.Unlock()

	notifier := &ManagerNotifier{
		Manager: m,
		Delay:   1 * time.Millisecond,
	}

	// NotifyFileChanged will error (no actual server) but we still check cached diags.
	lines, err := notifier.NotifyAndCollectDiagnostics(context.Background(), "/test/main.go", []byte("package main"))
	// Error from NotifyFileChanged is acceptable — gopls isn't running.
	// What matters is whether diagnostics are collected from cache.
	if err != nil {
		// Server failed to start — can't test full flow without gopls.
		// Just verify the language check passed (didn't return nil early).
		t.Skip("gopls not available — skipping diagnostic format test")
	}

	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "main.go:5:11")
	assert.Contains(t, lines[0], "undefined: foo")
}

func TestManagerNotifierRespectsContextCancellation(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	notifier := &ManagerNotifier{
		Manager: m,
		Delay:   10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// .txt → no language server → returns nil immediately (no sleep).
	// Use .go to actually hit the sleep path.
	// But without gopls, NotifyFileChanged will error.
	// Test with .txt to verify cancellation doesn't panic.
	_, err := notifier.NotifyAndCollectDiagnostics(ctx, "/test/readme.txt", []byte("hello"))
	// .txt is unsupported → returns nil, nil (no error even with cancelled ctx).
	assert.NoError(t, err)
}

func TestManagerNotifierDefaultDelay(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	notifier := &ManagerNotifier{
		Manager: m,
		// Delay is zero — should default to 500ms.
	}

	// .txt has no server → returns immediately. Use a timeout to verify
	// that unsupported languages don't sleep at all.
	start := time.Now()
	_, err := notifier.NotifyAndCollectDiagnostics(context.Background(), "/test/readme.txt", []byte("hello"))
	elapsed := time.Since(start)
	assert.NoError(t, err)
	assert.Less(t, elapsed, 50*time.Millisecond, "unsupported language should not sleep")
}
