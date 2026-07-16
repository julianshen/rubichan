package agentsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type checkerFunc func(tool string, input json.RawMessage) ApprovalResult

func (f checkerFunc) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	return f(tool, input)
}

func approvalTC() ToolUseBlock {
	return ToolUseBlock{ID: "tu_1", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)}
}

func TestApprovalFlowAutoApproved(t *testing.T) {
	for _, result := range []ApprovalResult{AutoApproved, TrustRuleApproved} {
		flow := &ApprovalFlow{
			Checker: checkerFunc(func(string, json.RawMessage) ApprovalResult { return result }),
			Approve: func(context.Context, string, json.RawMessage) (bool, error) {
				t.Fatal("approver must not be consulted for auto-approved tools")
				return false, nil
			},
		}
		out := flow.Decide(context.Background(), approvalTC())
		assert.True(t, out.Approved)
		assert.Empty(t, out.Message)
		assert.NoError(t, out.Err)
	}
}

func TestApprovalFlowAutoDenied(t *testing.T) {
	flow := &ApprovalFlow{
		Checker: checkerFunc(func(string, json.RawMessage) ApprovalResult { return AutoDenied }),
		Approve: func(context.Context, string, json.RawMessage) (bool, error) {
			t.Fatal("approver must not be consulted for auto-denied tools")
			return false, nil
		},
	}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
	assert.True(t, out.DenyAlways)
	assert.Equal(t, "Tool call denied by user (deny-always).", out.Message)
	assert.NoError(t, out.Err)
}

func TestApprovalFlowRequiredNoApproverConfigured(t *testing.T) {
	flow := &ApprovalFlow{
		Checker: checkerFunc(func(string, json.RawMessage) ApprovalResult { return ApprovalRequired }),
	}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
	assert.Equal(t, "approval function not configured", out.Message)
	assert.NoError(t, out.Err)
}

func TestApprovalFlowNilCheckerAsksApprover(t *testing.T) {
	asked := false
	flow := &ApprovalFlow{
		Approve: func(_ context.Context, tool string, _ json.RawMessage) (bool, error) {
			asked = true
			assert.Equal(t, "shell", tool)
			return true, nil
		},
	}
	out := flow.Decide(context.Background(), approvalTC())
	assert.True(t, asked, "nil checker must be treated as approval-required")
	assert.True(t, out.Approved)
}

func TestApprovalFlowApproveFuncDenies(t *testing.T) {
	flow := &ApprovalFlow{
		Approve: func(context.Context, string, json.RawMessage) (bool, error) { return false, nil },
	}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
	assert.False(t, out.DenyAlways)
	assert.Equal(t, "tool call denied by user", out.Message)
	assert.NoError(t, out.Err)
}

func TestApprovalFlowApproveFuncError(t *testing.T) {
	boom := errors.New("boom")
	flow := &ApprovalFlow{
		Approve: func(context.Context, string, json.RawMessage) (bool, error) { return false, boom },
	}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
	assert.Equal(t, "approval error", out.Message)
	assert.ErrorIs(t, out.Err, boom)
}

func uiHandlerReturning(action string) UIRequestHandler {
	return UIRequestFunc(func(_ context.Context, req UIRequest) (UIResponse, error) {
		return UIResponse{RequestID: req.ID, ActionID: action}, nil
	})
}

func TestApprovalFlowUIActions(t *testing.T) {
	cases := []struct {
		action     string
		approved   bool
		denyAlways bool
		message    string
		wantErr    bool
	}{
		{"allow", true, false, "", false},
		{"allow_always", true, false, "", false},
		{"yes", true, false, "", false},
		{"ALLOW", true, false, "", false}, // case-insensitive
		{"deny", false, false, "tool call denied by user", false},
		{"no", false, false, "tool call denied by user", false},
		{"deny_always", false, true, "Tool call denied by user (deny-always).", false},
		{"bogus", false, false, "approval error", true},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			flow := &ApprovalFlow{UIHandler: uiHandlerReturning(c.action)}
			out := flow.Decide(context.Background(), approvalTC())
			assert.Equal(t, c.approved, out.Approved)
			assert.Equal(t, c.denyAlways, out.DenyAlways)
			assert.Equal(t, c.message, out.Message)
			if c.wantErr {
				assert.Error(t, out.Err)
			} else {
				assert.NoError(t, out.Err)
			}
		})
	}
}

func TestApprovalFlowUIRequestShapeAndEvents(t *testing.T) {
	var captured UIRequest
	handler := UIRequestFunc(func(_ context.Context, req UIRequest) (UIResponse, error) {
		captured = req
		return UIResponse{RequestID: req.ID, ActionID: "allow"}, nil
	})
	var events []TurnEvent
	flow := &ApprovalFlow{
		UIHandler: handler,
		Emit:      func(ev TurnEvent) { events = append(events, ev) },
	}
	out := flow.Decide(context.Background(), approvalTC())
	require.True(t, out.Approved)

	// Canonical request shape shared by both loops.
	assert.Equal(t, "tu_1", captured.ID)
	assert.Equal(t, UIKindApproval, captured.Kind)
	assert.Equal(t, "Approve shell tool call", captured.Title)
	require.Len(t, captured.Actions, 4)
	assert.Equal(t, "allow", captured.Actions[0].ID)
	assert.True(t, captured.Actions[0].Default)
	assert.Equal(t, "shell", captured.Metadata["tool"])
	assert.Equal(t, `{"command":"ls"}`, captured.Metadata["input"])

	// ui_request then ui_response events.
	require.Len(t, events, 2)
	assert.Equal(t, "ui_request", events[0].Type)
	assert.Equal(t, "ui_response", events[1].Type)
}

func TestApprovalFlowUIResponseIDMismatch(t *testing.T) {
	handler := UIRequestFunc(func(_ context.Context, _ UIRequest) (UIResponse, error) {
		return UIResponse{RequestID: "wrong", ActionID: "allow"}, nil
	})
	flow := &ApprovalFlow{UIHandler: handler}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
	assert.Equal(t, "approval error", out.Message)
	assert.Error(t, out.Err)
}

func TestApprovalFlowUIHandlerError(t *testing.T) {
	boom := errors.New("ui down")
	handler := UIRequestFunc(func(_ context.Context, _ UIRequest) (UIResponse, error) {
		return UIResponse{}, boom
	})
	flow := &ApprovalFlow{UIHandler: handler}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
	assert.Equal(t, "approval error", out.Message)
	assert.ErrorIs(t, out.Err, boom)
}

func TestApprovalFlowUIHandlerPrecedesApproveFunc(t *testing.T) {
	flow := &ApprovalFlow{
		UIHandler: uiHandlerReturning("deny"),
		Approve: func(context.Context, string, json.RawMessage) (bool, error) {
			t.Fatal("approve func must not be called when a UI handler is set")
			return true, nil
		},
	}
	out := flow.Decide(context.Background(), approvalTC())
	assert.False(t, out.Approved)
}

func TestApprovalFlowNilEmitSafe(t *testing.T) {
	flow := &ApprovalFlow{UIHandler: uiHandlerReturning("allow")}
	out := flow.Decide(context.Background(), approvalTC()) // Emit is nil
	assert.True(t, out.Approved)
}

func TestApprovalFlowTruncatesLargeUIInput(t *testing.T) {
	big := fmt.Sprintf(`{"data":%q}`, strings.Repeat("x", 5000))
	tc := ToolUseBlock{ID: "tu_big", Name: "shell", Input: json.RawMessage(big)}
	var captured UIRequest
	handler := UIRequestFunc(func(_ context.Context, req UIRequest) (UIResponse, error) {
		captured = req
		return UIResponse{RequestID: req.ID, ActionID: "allow"}, nil
	})
	flow := &ApprovalFlow{UIHandler: handler}
	flow.Decide(context.Background(), tc)

	input := captured.Metadata["input"]
	assert.True(t, strings.HasSuffix(input, "...(truncated)"))
	assert.Len(t, input, maxUIRequestInputBytes+len("...(truncated)"))
}

func TestTruncateUIInputNeverSplitsRunes(t *testing.T) {
	// Place a 3-byte rune ("€") straddling the truncation boundary: naive
	// byte slicing at maxUIRequestInputBytes would cut it mid-sequence and
	// produce invalid UTF-8 in the preview.
	prefix := strings.Repeat("a", maxUIRequestInputBytes-1)
	input := json.RawMessage(prefix + "€xxxxx")

	result := truncateUIInput(input)
	assert.True(t, utf8.ValidString(result), "truncated preview must be valid UTF-8")
	assert.True(t, strings.HasSuffix(result, "...(truncated)"))
	assert.Equal(t, prefix+"...(truncated)", result, "partial rune must be dropped entirely")
}
