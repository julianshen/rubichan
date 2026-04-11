package cmux

// Ensure Client satisfies Caller at compile time.
var _ Caller = (*Client)(nil)

// Caller is the interface for making cmux JSON-RPC calls.
// Both Client and cmuxtest.MockClient implement this.
type Caller interface {
	Call(method string, params any) (*Response, error)
	Identity() *Identity
}

// CallerNotify sends a notification via any Caller.
// Returns true if the notification was accepted (resp.OK), false on any failure.
// Callers can use the return value to fall back to terminal notifications.
func CallerNotify(c Caller, title, subtitle, body string) bool {
	resp, err := c.Call("notification.create", map[string]string{
		"title": title, "subtitle": subtitle, "body": body,
	})
	return err == nil && resp.OK
}

// CallerSetProgress sets the sidebar progress via any Caller. Best-effort.
func CallerSetProgress(c Caller, fraction float64, label string) {
	c.Call("set-progress", map[string]any{"value": fraction, "label": label}) //nolint:errcheck
}

// CallerClearProgress clears the sidebar progress via any Caller. Best-effort.
func CallerClearProgress(c Caller) {
	c.Call("clear-progress", map[string]any{}) //nolint:errcheck
}
