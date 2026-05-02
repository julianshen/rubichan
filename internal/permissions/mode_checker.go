package permissions

import (
	"encoding/json"
	"log"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// safeToolAllowlist contains tools that are unconditionally safe and bypass
// the LLM classifier. These tools are read-only or information-query tools.
// Kept unexported to prevent external mutation.
var safeToolAllowlist = map[string]struct{}{
	"read_file": {}, "grep": {}, "glob": {}, "list_dir": {},
	"code_search": {}, "view": {}, "ls": {}, "cat": {},
	"find": {}, "search": {},
	"git_status": {}, "git_diff": {}, "git_log": {}, "git_show": {},
	"browser_snapshot": {}, "http_get": {}, "web_fetch": {},
	"lsp_diagnostics": {}, "lsp_hover": {}, "lsp_definition": {},
	"lsp_references": {},
}

// isReadOnlyTool reports whether a tool is safe to auto-approve when no
// explicit policy matches. These tools are read-only by convention; any
// addition here must be vetted for side effects.
func isReadOnlyTool(tool string) bool {
	_, ok := safeToolAllowlist[tool]
	return ok
}

// ModeAwareChecker is a decorator: it only changes behavior when the wrapped
// checker returns ApprovalRequired. Deny and explicit-approval results pass
// through unchanged.
type ModeAwareChecker struct {
	mode       agentsdk.PermissionMode
	checker    agentsdk.ApprovalChecker
	explainer  agentsdk.Explainer
	classifier *YOLOClassifier // nil when not in auto mode
}

// ModeAwareOption configures a ModeAwareChecker.
type ModeAwareOption func(*ModeAwareChecker)

// WithClassifier attaches a YOLOClassifier for ModeAuto decision support.
func WithClassifier(classifier *YOLOClassifier) ModeAwareOption {
	return func(c *ModeAwareChecker) {
		c.classifier = classifier
	}
}

// NewModeAwareChecker creates a ModeAwareChecker with the given mode and
// underlying checker. Bypass mode disables all safety checks; the warning
// gives operators an audit trail but callers must check the mode field if
// they need to know bypass is active.
func NewModeAwareChecker(mode agentsdk.PermissionMode, checker agentsdk.ApprovalChecker, opts ...ModeAwareOption) *ModeAwareChecker {
	if mode == agentsdk.ModeBypass {
		log.Println("WARNING: permission mode is 'bypass' — all tools will be auto-approved. Use with caution.")
	}
	var explainer agentsdk.Explainer
	if e, ok := checker.(agentsdk.Explainer); ok {
		explainer = e
	}
	c := &ModeAwareChecker{mode: mode, checker: checker, explainer: explainer}
	for _, opt := range opts {
		opt(c)
	}
	return c
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
		// In ModeAuto, use the LLM classifier for additional safety.
		if c.mode == agentsdk.ModeAuto && c.classifier != nil {
			var parsedInput map[string]interface{}
			if len(input) > 0 {
				if err := json.Unmarshal(input, &parsedInput); err != nil {
					// Malformed input: classifier can't evaluate, fall through to
					// manual approval rather than making a blind decision.
					parsedInput = nil
				}
			}
			decision, err := c.classifier.Classify(tool, parsedInput)
			if err == nil && decision == agentsdk.AutoApproved {
				return agentsdk.AutoApproved
			}
		}
		return agentsdk.ApprovalRequired
	default:
		return agentsdk.ApprovalRequired
	}
}

// Explain forwards to the wrapped checker's Explainer so UI messages
// show the original policy reason even when mode logic overrides the result.
// The explainer is captured at construction (via type assertion) to avoid
// repeated interface checks on every Explain call — this is a hot path
// when rendering approval prompts in the TUI.
func (c *ModeAwareChecker) Explain(tool string, input json.RawMessage) string {
	if c.explainer != nil {
		return c.explainer.Explain(tool, input)
	}
	return ""
}
