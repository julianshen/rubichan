package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Shared user-facing messages for approval outcomes. Both agent loops embed
// these verbatim in tool results, so they live in one place.
const (
	approvalMsgDenyAlways   = "Tool call denied by user (deny-always)."
	approvalMsgDenied       = "tool call denied by user"
	approvalMsgError        = "approval error"
	approvalMsgUnconfigured = "approval function not configured"
)

// ApprovalOutcome is the result of running the approval flow for one tool
// call. When Approved is false, Message carries the user-facing tool-result
// text and Err (if non-nil) carries the underlying failure for logging.
type ApprovalOutcome struct {
	Approved   bool
	DenyAlways bool
	Message    string
	Err        error
}

// ApprovalFlow is the tool-approval decision engine shared by the SDK and
// internal agent loops. It consults the Checker first (nil is treated as
// approval-required), then interacts with the user through UIHandler or
// Approve — UIHandler takes precedence when both are set.
//
// Emit, when non-nil, receives the ui_request / ui_response TurnEvents
// produced during a UI interaction; each loop wires its own emission.
type ApprovalFlow struct {
	Checker   ApprovalChecker
	Approve   ApprovalFunc
	UIHandler UIRequestHandler
	Emit      func(TurnEvent)
}

// Decide runs the approval flow for the given tool call.
func (f *ApprovalFlow) Decide(ctx context.Context, tc ToolUseBlock) ApprovalOutcome {
	result := ApprovalRequired
	if f.Checker != nil {
		result = f.Checker.CheckApproval(tc.Name, tc.Input)
	}

	switch result {
	case AutoApproved, TrustRuleApproved:
		return ApprovalOutcome{Approved: true}
	case AutoDenied:
		return ApprovalOutcome{DenyAlways: true, Message: approvalMsgDenyAlways}
	}

	if f.UIHandler == nil && f.Approve == nil {
		return ApprovalOutcome{Message: approvalMsgUnconfigured}
	}

	approved, denyAlways, err := f.requestApproval(ctx, tc)
	switch {
	case err != nil:
		return ApprovalOutcome{Message: approvalMsgError, Err: err}
	case approved:
		return ApprovalOutcome{Approved: true}
	case denyAlways:
		return ApprovalOutcome{DenyAlways: true, Message: approvalMsgDenyAlways}
	default:
		return ApprovalOutcome{Message: approvalMsgDenied}
	}
}

// requestApproval asks the user through the UI handler when available,
// otherwise through the plain approval function.
func (f *ApprovalFlow) requestApproval(ctx context.Context, tc ToolUseBlock) (approved, denyAlways bool, err error) {
	if f.UIHandler == nil {
		approved, err = f.Approve(ctx, tc.Name, tc.Input)
		return approved, false, err
	}

	req := UIRequest{
		ID:      tc.ID,
		Kind:    UIKindApproval,
		Title:   fmt.Sprintf("Approve %s tool call", tc.Name),
		Message: "Review and choose how to proceed.",
		Actions: []UIAction{
			{ID: "allow", Label: "Allow", Default: true},
			{ID: "deny", Label: "Deny", Style: "danger"},
			{ID: "allow_always", Label: "Always Allow"},
			{ID: "deny_always", Label: "Always Deny", Style: "danger"},
		},
		Metadata: map[string]string{
			"tool":  tc.Name,
			"input": truncateUIInput(tc.Input),
		},
	}
	f.emit(TurnEvent{Type: "ui_request", UIRequest: &req})
	resp, err := f.UIHandler.Request(ctx, req)
	if err != nil {
		return false, false, err
	}
	if resp.RequestID != req.ID {
		return false, false, fmt.Errorf("unexpected UI response id %q for request %q", resp.RequestID, req.ID)
	}
	f.emit(TurnEvent{Type: "ui_response", UIResponse: &resp})

	switch strings.ToLower(resp.ActionID) {
	case "allow", "allow_always", "yes":
		// "allow_always" cache persistence is handled by the UI adapter.
		return true, false, nil
	case "deny_always":
		return false, true, nil
	case "deny", "no":
		return false, false, nil
	default:
		return false, false, fmt.Errorf("unsupported UI approval action %q", resp.ActionID)
	}
}

func (f *ApprovalFlow) emit(ev TurnEvent) {
	if f.Emit != nil {
		f.Emit(ev)
	}
}

const maxUIRequestInputBytes = 2048

// truncateUIInput bounds tool input shown in approval UI metadata. The
// length check happens on the raw bytes so oversize inputs are sliced
// before the string conversion, and the cut backs up to a rune boundary
// so truncation never produces invalid UTF-8 in the preview.
func truncateUIInput(input json.RawMessage) string {
	if len(input) <= maxUIRequestInputBytes {
		return string(input)
	}
	cut := maxUIRequestInputBytes
	for cut > 0 && !utf8.RuneStart(input[cut]) {
		cut--
	}
	return string(input[:cut]) + "...(truncated)"
}
