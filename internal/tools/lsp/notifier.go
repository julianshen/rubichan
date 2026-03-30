package lsp

import (
	"context"
	"fmt"
	"time"
)

// ManagerNotifier adapts Manager to the tools.LSPNotifier interface.
// It notifies the language server of file changes and collects any
// resulting diagnostics after a brief wait.
type ManagerNotifier struct {
	Manager *Manager
	// Delay is the time to wait for the server to process diagnostics.
	// Defaults to 500ms if zero.
	Delay time.Duration
}

// NotifyAndCollectDiagnostics sends a didOpen/didChange notification for the
// given file and waits briefly for the server to publish diagnostics. It returns
// formatted diagnostic lines (errors only) or nil if there are none.
func (n *ManagerNotifier) NotifyAndCollectDiagnostics(ctx context.Context, filePath string, content []byte) ([]string, error) {
	if err := n.Manager.NotifyFileChanged(ctx, filePath, content); err != nil {
		return nil, err
	}

	// Brief wait for server to process — diagnostics are async.
	delay := n.Delay
	if delay == 0 {
		delay = 500 * time.Millisecond
	}

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	uri := pathToURI(filePath)
	diags := n.Manager.DiagnosticsFor(uri, false) // errors only

	if len(diags) == 0 {
		return nil, nil
	}

	var lines []string
	for _, d := range diags {
		lines = append(lines, fmt.Sprintf("  %s:%d:%d: %s: %s",
			filePath, d.Range.Start.Line+1, d.Range.Start.Character+1,
			d.Severity.String(), d.Message))
	}
	return lines, nil
}
