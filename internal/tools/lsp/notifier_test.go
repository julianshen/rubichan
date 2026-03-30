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
		Delay:   1 * time.Millisecond, // fast for tests
	}

	// No server configured for .txt files — NotifyFileChanged returns nil.
	lines, err := notifier.NotifyAndCollectDiagnostics(context.Background(), "/test/readme.txt", []byte("hello"))
	require.NoError(t, err)
	assert.Nil(t, lines)
}

func TestManagerNotifierFormatsDiagnostics(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	// Use a .txt file — no language server is registered for it, so
	// NotifyFileChanged returns nil without attempting to start a server.
	// Pre-populate diagnostics cache to simulate a server having published them.
	uri := pathToURI("/test/notes.txt")
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

	lines, err := notifier.NotifyAndCollectDiagnostics(context.Background(), "/test/notes.txt", []byte("some content"))
	require.NoError(t, err)

	// Only errors (SeverityError) should be returned.
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "/test/notes.txt:5:11")
	assert.Contains(t, lines[0], "error")
	assert.Contains(t, lines[0], "undefined: foo")
}

func TestManagerNotifierRespectsContextCancellation(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	notifier := &ManagerNotifier{
		Manager: m,
		Delay:   10 * time.Second, // would block forever without cancellation
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := notifier.NotifyAndCollectDiagnostics(ctx, "/test/readme.txt", []byte("hello"))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestManagerNotifierDefaultDelay(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test", false)

	notifier := &ManagerNotifier{
		Manager: m,
		// Delay is zero — should default to 500ms.
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := notifier.NotifyAndCollectDiagnostics(ctx, "/test/readme.txt", []byte("hello"))
	elapsed := time.Since(start)

	// Context should cancel before the 500ms default delay completes.
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 200*time.Millisecond)
}
