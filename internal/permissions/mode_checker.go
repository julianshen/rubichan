package permissions

import (
	"encoding/json"
	"log"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// isReadOnlyTool reports whether a tool is safe to auto-approve when no
// explicit policy matches. These tools are read-only by convention; any
// addition here must be vetted for side effects.
func isReadOnlyTool(tool string) bool {
	switch tool {
	case "read_file", "grep", "glob", "list_dir", "code_search", "view",
		"ls", "cat", "find", "search",
		"git_status", "git_diff", "git_log", "git_show",
		"browser_snapshot", "http_get", "web_fetch",
		"lsp_diagnostics", "lsp_hover", "lsp_definition", "lsp_references":
		return true
	}
	return false
}

// ModeAwareChecker is a decorator: it only changes behavior when the wrapped
// checker returns ApprovalRequired. Deny and explicit-approval results pass
// through unchanged.
type ModeAwareChecker struct {
	mode      agentsdk.PermissionMode
	checker   agentsdk.ApprovalChecker
	explainer agentsdk.Explainer
}

// NewModeAwareChecker creates a ModeAwareChecker with the given mode and
// underlying checker. Bypass mode disables all safety checks; the warning
// gives operators an audit trail but callers must check the mode field if
// they need to know bypass is active.
func NewModeAwareChecker(mode agentsdk.PermissionMode, checker agentsdk.ApprovalChecker) *ModeAwareChecker {
	if mode == agentsdk.ModeBypass {
		log.Println("WARNING: permission mode is 'bypass' — all tools will be auto-approved. Use with caution.")
	}
	var explainer agentsdk.Explainer
	if e, ok := checker.(agentsdk.Explainer); ok {
		explainer = e
	}
	return &ModeAwareChecker{mode: mode, checker: checker, explainer: explainer}
}

// CheckApproval evaluates the underlying checker, then applies mode logic
// when the policy returns ApprovalRequired.
func (c *ModeAwareChecker) CheckApproval(tool string, input json.RawMessage) agentsdk.ApprovalResult {
	result := c.checker.CheckApproval(tool, input)

	// Deny rules are absolute — mode must not override explicit denials.
	if result == agentsdk.AutoDenied {
		return agentsdk.AutoDenied
	}

	// If the policy already approved, respect it.
	if result == agentsdk.AutoApproved || result == agentsdk.TrustRuleApproved {
		return result
	}

	// Policy returned ApprovalRequired. Apply mode logic.
	switch c.mode {
	case agentsdk.ModeBypass:
		return agentsdk.AutoApproved
	case agentsdk.ModeFullAuto:
		return agentsdk.AutoApproved
	case agentsdk.ModeAuto, agentsdk.ModePlan:
		// ModeAuto and ModePlan share the same fallback because trust-rule
		// auto-approval was already evaluated above. The distinction is in
		// UX messaging, not policy logic.
		if isReadOnlyTool(tool) {
			return agentsdk.AutoApproved
		}
		return agentsdk.ApprovalRequired
	default:
		return agentsdk.ApprovalRequired
	}
}

// Explain delegates to the underlying checker if it implements Explainer.
func (c *ModeAwareChecker) Explain(tool string, input json.RawMessage) string {
	if c.explainer != nil {
		return c.explainer.Explain(tool, input)
	}
	return ""
}
