package cmux

// Ensure Client satisfies Caller at compile time.
var _ Caller = (*Client)(nil)

// Caller is the interface for making cmux JSON-RPC calls.
// Both Client and cmuxtest.MockClient implement this.
type Caller interface {
	Call(method string, params any) (*Response, error)
	Identity() *Identity
}

// CallerNotify sends a notification via any Caller. Errors are intentionally
// suppressed — notifications are best-effort.
func CallerNotify(c Caller, title, subtitle, body string) {
	c.Call("notification.create", map[string]string{ //nolint:errcheck
		"title": title, "subtitle": subtitle, "body": body,
	})
}

// CallerSetProgress sets the sidebar progress via any Caller. Best-effort.
func CallerSetProgress(c Caller, fraction float64, label string) {
	c.Call("set-progress", map[string]any{"value": fraction, "label": label}) //nolint:errcheck
}

// CallerClearProgress clears the sidebar progress via any Caller. Best-effort.
func CallerClearProgress(c Caller) {
	c.Call("clear-progress", map[string]any{}) //nolint:errcheck
}
