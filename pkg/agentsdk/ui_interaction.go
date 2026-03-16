package agentsdk

import (
	"context"
	"encoding/json"
	"errors"
)

// UIRequestKind identifies the category of UI interaction a runtime wants to
// present to a client.
type UIRequestKind string

const (
	// UIKindApproval requests an approval/denial decision for a pending action.
	UIKindApproval UIRequestKind = "approval"
	// UIKindConfirm requests a yes/no style confirmation dialog.
	UIKindConfirm UIRequestKind = "confirm"
	// UIKindSelect requests selection from a menu of actions/options.
	UIKindSelect UIRequestKind = "select"
	// UIKindForm requests structured user input using schema-defined fields.
	UIKindForm UIRequestKind = "form"
)

// UIAction defines one selectable action in a UI request.
type UIAction struct {
	ID      string // stable action identifier (e.g. "allow", "deny")
	Label   string // user-visible label
	Style   string // optional presentation hint (e.g. "primary", "danger")
	Default bool   // preferred action for Enter/default submit
}

// UIRequest describes an interaction that should be rendered by an adapter
// (TUI, web, chat, etc.) and answered synchronously by the runtime.
type UIRequest struct {
	ID             string            // unique request ID for correlation
	Kind           UIRequestKind     // approval/confirm/select/form
	Title          string            // concise heading
	Message        string            // primary explanatory text
	Schema         json.RawMessage   // optional JSON Schema for Values
	Actions        []UIAction        // available actions for the user
	TimeoutSeconds int               // optional timeout hint (0 = adapter default)
	Metadata       map[string]string // optional transport-safe key/value hints
}

// UIUpdate carries an incremental update for a previously emitted UI request.
// This supports progress/status updates while a prompt remains open.
type UIUpdate struct {
	RequestID string          // request being updated
	Status    string          // optional machine-readable status
	Message   string          // optional user-facing status text
	Patch     json.RawMessage // optional partial payload patch
}

// UIResponse captures a user's response to a UI request.
type UIResponse struct {
	RequestID string          // ID from UIRequest.ID
	ActionID  string          // chosen action ID
	Values    json.RawMessage // optional structured values (for forms/selectors)
}

// UIRequestHandler resolves runtime UI requests by delegating presentation and
// user interaction to a host-specific adapter.
type UIRequestHandler interface {
	Request(ctx context.Context, req UIRequest) (UIResponse, error)
}

// UIRequestFunc adapts a plain function to UIRequestHandler.
type UIRequestFunc func(ctx context.Context, req UIRequest) (UIResponse, error)

// Request implements UIRequestHandler.
func (f UIRequestFunc) Request(ctx context.Context, req UIRequest) (UIResponse, error) {
	if f == nil {
		return UIResponse{}, errors.New("agentsdk: nil UIRequestFunc")
	}
	return f(ctx, req)
}
