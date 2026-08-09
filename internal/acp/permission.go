package acp

import (
	"context"
	"encoding/json"
	"fmt"
)

// PermissionOptionKind is what an option does if the user picks it. ACP defines
// exactly these four; a client renders the option by its Name and reports back
// the OptionID, so the kind is the agent's own record of what it promised.
type PermissionOptionKind string

const (
	PermissionAllowOnce    PermissionOptionKind = "allow_once"
	PermissionAllowAlways  PermissionOptionKind = "allow_always"
	PermissionRejectOnce   PermissionOptionKind = "reject_once"
	PermissionRejectAlways PermissionOptionKind = "reject_always"
)

// PermissionOption is one choice offered to the user.
type PermissionOption struct {
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

// PermissionToolCall describes the tool call awaiting a decision. It is ACP's
// ToolCallUpdate shape, and every field but the id is optional — but a decision
// made without seeing what the tool would do is not consent, so callers are
// expected to fill Title and Content.
type PermissionToolCall struct {
	ToolCallID string            `json:"toolCallId"`
	Title      string            `json:"title,omitempty"`
	Status     ToolCallStatus    `json:"status,omitempty"`
	Content    []ToolCallContent `json:"content,omitempty"`
}

// ToolCallContent carries displayable detail about a tool call.
type ToolCallContent struct {
	Type    string       `json:"type"`
	Content ContentBlock `json:"content"`
}

// TextToolCallContent shows the user what the tool was asked to do.
func TextToolCallContent(text string) ToolCallContent {
	return ToolCallContent{
		Type:    "content",
		Content: ContentBlock{Type: ContentText, Text: text},
	}
}

// RequestPermissionParams is the agent asking the client to get a decision.
type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  PermissionToolCall `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// Outcome values a client may report.
const (
	// OutcomeSelected means the user picked one of the offered options.
	OutcomeSelected = "selected"
	// OutcomeCancelled means the turn ended before a decision was made.
	OutcomeCancelled = "cancelled"
)

// PermissionOutcome is the client's answer: a selection, or nothing.
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// RequestPermissionResult wraps the outcome, which is nested one level deep on
// the wire: {"outcome":{"outcome":"selected","optionId":"..."}}.
type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// Requester is the half of a connection that originates calls to the peer and
// waits for an answer. Narrowed to one method so a caller asking for permission
// cannot also stream, and so tests need not stand up a connection.
type Requester interface {
	Request(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// The connection is the only production Requester. Asserting it here keeps the
// two from drifting apart silently: a signature change on Conn.Request would
// otherwise surface as a wiring failure at the composition root rather than
// here, where the seam is defined.
var _ Requester = (*Conn)(nil)

// RequestToolPermission asks the client whether a tool may run and reports the
// user's decision.
//
// Every failure path denies. A permission request that cannot be asked, cannot
// be understood, or names an option that was never offered has produced no
// consent, and running the tool anyway would be the agent deciding on the
// user's behalf while appearing to have asked.
func RequestToolPermission(ctx context.Context, r Requester, params RequestPermissionParams) (bool, error) {
	raw, err := r.Request(ctx, MethodSessionRequestPermission, params)
	if err != nil {
		return false, fmt.Errorf("request tool permission: %w", err)
	}

	var result RequestPermissionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, fmt.Errorf("decode permission outcome: %w", err)
	}

	switch result.Outcome.Outcome {
	case OutcomeCancelled:
		return false, nil
	case OutcomeSelected:
		return allows(params.Options, result.Outcome.OptionID)
	default:
		return false, fmt.Errorf("unknown permission outcome %q", result.Outcome.Outcome)
	}
}

// allows resolves the chosen optionId against the options that were offered.
//
// The lookup is deliberate: the client also echoes nothing else the agent could
// use, and honouring an id the agent never offered would let a malformed or
// hostile answer approve a tool by naming an option that does not exist.
func allows(offered []PermissionOption, optionID string) (bool, error) {
	for _, opt := range offered {
		if opt.OptionID != optionID {
			continue
		}
		switch opt.Kind {
		case PermissionAllowOnce, PermissionAllowAlways:
			return true, nil
		case PermissionRejectOnce, PermissionRejectAlways:
			return false, nil
		default:
			return false, fmt.Errorf("option %q has unknown kind %q", optionID, opt.Kind)
		}
	}
	return false, fmt.Errorf("client selected option %q, which was never offered", optionID)
}
