package agentsdk

import (
	"context"
	"encoding/json"
)

// ApprovalFunc is called before executing a tool to get user approval.
// Returns true if the tool execution is approved.
type ApprovalFunc func(ctx context.Context, tool string, input json.RawMessage) (bool, error)

// AutoApproveChecker tests if a tool would be auto-approved without blocking.
// When the approval function (or the object that provides it) implements this
// interface, the agent can execute auto-approved tools in parallel.
type AutoApproveChecker interface {
	IsAutoApproved(tool string) bool
}

// AlwaysAutoApprove is an AutoApproveChecker that approves all tools.
// Use for headless mode or --auto-approve where all tools are pre-approved.
type AlwaysAutoApprove struct{}

// IsAutoApproved always returns true.
func (AlwaysAutoApprove) IsAutoApproved(_ string) bool { return true }

// CheckApproval always returns AutoApproved, implementing ApprovalChecker.
func (AlwaysAutoApprove) CheckApproval(_ string, _ json.RawMessage) ApprovalResult {
	return AutoApproved
}

// ToolParallelPolicy determines whether a tool call may be executed in parallel
// with other auto-approved calls. This separates parallelization decisions from
// approval decisions: a tool may be auto-approved yet not safe to parallelize.
type ToolParallelPolicy interface {
	CanParallelize(tool string) bool
}

// AllowAllParallel is a ToolParallelPolicy that permits all tools to run in parallel.
type AllowAllParallel struct{}

// CanParallelize always returns true.
func (AllowAllParallel) CanParallelize(_ string) bool { return true }

// ApprovalResult represents the approval decision for a tool call.
type ApprovalResult int

const (
	// ApprovalRequired means the tool call needs explicit user approval.
	ApprovalRequired ApprovalResult = iota
	// AutoApproved means the tool is unconditionally safe (e.g., read-only tools).
	AutoApproved
	// TrustRuleApproved means the tool input matched a trust rule pattern.
	TrustRuleApproved
	// AutoDenied means the tool was explicitly denied by the user (deny-always).
	AutoDenied
)

// String returns a human-readable name for the approval result.
func (r ApprovalResult) String() string {
	switch r {
	case ApprovalRequired:
		return "ApprovalRequired"
	case AutoApproved:
		return "AutoApproved"
	case TrustRuleApproved:
		return "TrustRuleApproved"
	case AutoDenied:
		return "AutoDenied"
	default:
		return "Unknown"
	}
}

// PermissionMode controls the default approval stance when no explicit
// policy rule matches.
type PermissionMode int

const (
	// ModePlan is the safest default: it auto-approves only read-only tools,
	// forcing explicit approval for anything that could mutate state.
	ModePlan PermissionMode = iota

	// ModeAuto requires upfront trust rule configuration; it avoids prompting
	// for tools the user has already vetted.
	ModeAuto

	// ModeFullAuto assumes all tools are safe unless explicitly denied —
	// use only in trusted environments.
	ModeFullAuto

	// ModeBypass is for emergency recovery when the agent cannot proceed
	// due to over-restrictive policies.
	ModeBypass
)

// String returns a human-readable name for the permission mode.
func (m PermissionMode) String() string {
	switch m {
	case ModePlan:
		return "plan"
	case ModeAuto:
		return "auto"
	case ModeFullAuto:
		return "fullAuto"
	case ModeBypass:
		return "bypass"
	default:
		return "unknown"
	}
}

// ParsePermissionMode parses a mode string. Unknown values default to ModePlan
// to fail safely rather than crash on typos.
func ParsePermissionMode(s string) PermissionMode {
	switch s {
	case "plan":
		return ModePlan
	case "auto":
		return ModeAuto
	case "fullAuto":
		return ModeFullAuto
	case "bypass":
		return ModeBypass
	default:
		return ModePlan
	}
}

// ApprovalChecker determines the approval status of a tool call based on
// both the tool name and its input.
type ApprovalChecker interface {
	CheckApproval(tool string, input json.RawMessage) ApprovalResult
}

// Explainer provides human-readable explanations for approval decisions.
type Explainer interface {
	Explain(tool string, input json.RawMessage) string
}

// CompositeApprovalChecker chains multiple ApprovalCheckers. The first
// non-ApprovalRequired result wins.
type CompositeApprovalChecker struct {
	checkers []ApprovalChecker
}

// NewCompositeApprovalChecker creates a checker that evaluates each checker
// in order, returning the first non-ApprovalRequired result.
func NewCompositeApprovalChecker(checkers ...ApprovalChecker) *CompositeApprovalChecker {
	return &CompositeApprovalChecker{checkers: checkers}
}

// CheckApproval evaluates checkers in order. The first checker to return
// a decisive result (anything other than ApprovalRequired) wins.
func (c *CompositeApprovalChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	for _, checker := range c.checkers {
		result := checker.CheckApproval(tool, input)
		if result != ApprovalRequired {
			return result
		}
	}
	return ApprovalRequired
}
