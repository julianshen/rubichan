package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// ApprovalResult represents the three-tier approval decision for a tool call.
// It distinguishes between known-safe tools, trust-rule-matched operations,
// and operations requiring explicit user approval.
type ApprovalResult int

const (
	// ApprovalRequired means the tool call needs explicit user approval.
	ApprovalRequired ApprovalResult = iota
	// AutoApproved means the tool is unconditionally safe (e.g., read-only tools).
	AutoApproved
	// TrustRuleApproved means the tool input matched a trust rule pattern.
	TrustRuleApproved
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
	default:
		return "Unknown"
	}
}

// ApprovalChecker determines the approval status of a tool call based on
// both the tool name and its input. This replaces the coarse-grained
// AutoApproveChecker with input-sensitive, pattern-based trust decisions.
type ApprovalChecker interface {
	CheckApproval(tool string, input json.RawMessage) ApprovalResult
}

// TrustRule defines a pattern-based approval rule for tool inputs.
// Rules can allow or deny specific operations based on regex matching
// against the serialized tool input.
type TrustRule struct {
	Tool    string `toml:"tool"`    // Tool name to match, or "*" for all tools
	Pattern string `toml:"pattern"` // Regex pattern matched against string values in input
	Action  string `toml:"action"`  // "allow" or "deny"
}

// Matches reports whether this rule applies to the given tool call.
// It checks the tool name (exact match or "*" wildcard) and then
// matches the pattern against string values extracted from the JSON input.
// This allows patterns like "^go test" to match the command value directly,
// rather than requiring users to account for JSON structure.
//
// Note: patterns only match against string values within the JSON input.
// Non-string values (numbers, booleans) are not matched.
func (r TrustRule) Matches(tool string, input json.RawMessage) (bool, error) {
	// Check tool name.
	if r.Tool != "*" && r.Tool != tool {
		return false, nil
	}

	re, err := regexp.Compile(r.Pattern)
	if err != nil {
		return false, err
	}

	// Extract string values from JSON and match against each.
	// This lets patterns like "^go test" match the command field value.
	values := extractStringValues(input)
	for _, v := range values {
		if re.MatchString(v) {
			return true, nil
		}
	}
	return false, nil
}

// ValidateTrustRules checks that all trust rules have valid Action fields
// ("allow" or "deny") and compilable regex patterns. Returns the first
// validation error found.
func ValidateTrustRules(rules []TrustRule) error {
	for i, r := range rules {
		if r.Action != "allow" && r.Action != "deny" {
			return fmt.Errorf("trust rule %d: invalid action %q (must be \"allow\" or \"deny\")", i, r.Action)
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("trust rule %d: invalid pattern %q: %w", i, r.Pattern, err)
		}
	}
	return nil
}

// extractStringValues recursively extracts all string values from a JSON blob.
func extractStringValues(data json.RawMessage) []string {
	// Try as string.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return []string{s}
	}

	// Try as object — extract values.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		var result []string
		for _, v := range obj {
			result = append(result, extractStringValues(v)...)
		}
		return result
	}

	// Try as array.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		var result []string
		for _, v := range arr {
			result = append(result, extractStringValues(v)...)
		}
		return result
	}

	return nil
}

// compiledRule is a trust rule with its regex pre-compiled for efficiency.
type compiledRule struct {
	tool   string
	re     *regexp.Regexp
	action string
}

// TrustRuleChecker evaluates tool calls against a list of trust rules.
// Deny rules take precedence over allow rules. If no rule matches,
// the result is ApprovalRequired. Regexes are pre-compiled at construction
// time; rules with invalid patterns are skipped.
type TrustRuleChecker struct {
	rules []compiledRule
}

// NewTrustRuleChecker creates a checker with the given trust rules.
// Rules with invalid regex patterns are silently skipped.
func NewTrustRuleChecker(rules []TrustRule) *TrustRuleChecker {
	var compiled []compiledRule
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue // skip invalid patterns (validated earlier by ValidateTrustRules)
		}
		compiled = append(compiled, compiledRule{
			tool:   r.Tool,
			re:     re,
			action: r.Action,
		})
	}
	return &TrustRuleChecker{rules: compiled}
}

// CheckApproval evaluates the tool call against all trust rules.
// Deny rules are checked first — if any deny rule matches, the result
// is ApprovalRequired regardless of allow rules. Then allow rules are
// checked — if any matches, the result is TrustRuleApproved.
func (c *TrustRuleChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	values := extractStringValues(input)

	// First pass: check deny rules.
	for _, rule := range c.rules {
		if rule.action != "deny" {
			continue
		}
		if matchesCompiledRule(rule, tool, values) {
			return ApprovalRequired
		}
	}

	// Second pass: check allow rules.
	for _, rule := range c.rules {
		if rule.action != "allow" {
			continue
		}
		if matchesCompiledRule(rule, tool, values) {
			return TrustRuleApproved
		}
	}

	return ApprovalRequired
}

// matchesCompiledRule checks if a compiled rule matches the given tool and values.
func matchesCompiledRule(rule compiledRule, tool string, values []string) bool {
	if rule.tool != "*" && rule.tool != tool {
		return false
	}
	for _, v := range values {
		if rule.re.MatchString(v) {
			return true
		}
	}
	return false
}

// autoApproveAdapter wraps a legacy AutoApproveChecker (tool-name-only)
// into the input-sensitive ApprovalChecker interface.
type autoApproveAdapter struct {
	checker AutoApproveChecker
}

func (a *autoApproveAdapter) CheckApproval(tool string, _ json.RawMessage) ApprovalResult {
	if a.checker.IsAutoApproved(tool) {
		return AutoApproved
	}
	return ApprovalRequired
}

// CompositeApprovalChecker chains multiple ApprovalCheckers. The first
// non-ApprovalRequired result wins. This lets session caches, trust rules,
// and built-in defaults compose together.
//
// Ordering matters: checkers earlier in the list take priority. In the typical
// composition [sessionCache, trustRules], a user's explicit "always approve"
// decision (session cache) intentionally overrides config-based deny rules.
// This is by design — the session cache represents a real-time user decision
// which has higher authority than static configuration.
type CompositeApprovalChecker struct {
	checkers []ApprovalChecker
}

// NewCompositeApprovalChecker creates a checker that evaluates each checker
// in order, returning the first non-ApprovalRequired result.
func NewCompositeApprovalChecker(checkers ...ApprovalChecker) *CompositeApprovalChecker {
	return &CompositeApprovalChecker{checkers: checkers}
}

// CheckApproval evaluates checkers in order. The first checker to return
// AutoApproved or TrustRuleApproved wins. If all return ApprovalRequired,
// the result is ApprovalRequired.
func (c *CompositeApprovalChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	for _, checker := range c.checkers {
		result := checker.CheckApproval(tool, input)
		if result != ApprovalRequired {
			return result
		}
	}
	return ApprovalRequired
}
