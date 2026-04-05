package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ApprovalResult, ApprovalChecker, and CompositeApprovalChecker are defined
// in pkg/agentsdk/ and re-exported via sdk_aliases.go.

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

// GlobTrustRule defines a glob-based approval rule using "tool(pattern)" syntax.
// It is a user-friendly alternative to TrustRule's regex patterns.
type GlobTrustRule struct {
	Glob   string `toml:"glob"`   // "tool(pattern)" glob syntax
	Action string `toml:"action"` // "allow" or "deny"
}

// ValidateTrustRules checks that all trust rules (both regex and glob) have
// valid Action fields ("allow" or "deny") and compilable patterns. Returns
// the first validation error found.
func ValidateTrustRules(rules []TrustRule, globs []GlobTrustRule) error {
	for i, r := range rules {
		if r.Action != "allow" && r.Action != "deny" {
			return fmt.Errorf("trust rule %d: invalid action %q (must be \"allow\" or \"deny\")", i, r.Action)
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("trust rule %d: invalid pattern %q: %w", i, r.Pattern, err)
		}
	}
	for i, g := range globs {
		if g.Action != "allow" && g.Action != "deny" {
			return fmt.Errorf("glob trust rule %d: invalid action %q (must be \"allow\" or \"deny\")", i, g.Action)
		}
		if _, _, err := ParseGlobRule(g.Glob); err != nil {
			return fmt.Errorf("glob trust rule %d: %w", i, err)
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

// ParseGlobRule parses a user-friendly glob trust rule in the format
// "ToolName(glob_pattern)" and returns the tool name and a compiled
// regex equivalent of the glob pattern.
//
// Supported glob syntax:
//   - * matches any sequence of characters (including empty)
//   - ? matches exactly one character
//   - [abc] character classes (passed through to regex)
//
// Examples:
//
//	"shell(git *)"  -> tool="shell", regex=^git .*$
//	"file(read:?.go)" -> tool="file", regex=^read:.\.go$
func ParseGlobRule(glob string) (string, *regexp.Regexp, error) {
	idx := strings.Index(glob, "(")
	if idx < 0 || !strings.HasSuffix(glob, ")") {
		return "", nil, fmt.Errorf("invalid glob rule %q: expected format ToolName(pattern)", glob)
	}
	tool := glob[:idx]
	pattern := glob[idx+1 : len(glob)-1]

	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteByte('.')
		case '[':
			j := strings.IndexByte(pattern[i:], ']')
			if j < 0 {
				return "", nil, fmt.Errorf("unclosed character class in glob %q", glob)
			}
			sb.WriteString(pattern[i : i+j+1])
			i += j
		case '.', '+', '^', '$', '|', '\\', '{', '}', '(', ')':
			sb.WriteByte('\\')
			sb.WriteByte(pattern[i])
		default:
			sb.WriteByte(pattern[i])
		}
	}
	sb.WriteString("$")

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return "", nil, fmt.Errorf("invalid glob pattern in %q: %w", glob, err)
	}
	return tool, re, nil
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

// NewTrustRuleChecker creates a checker from both regex and glob trust rules.
// Rules with invalid patterns are silently skipped (they should be caught
// earlier by ValidateTrustRules).
func NewTrustRuleChecker(rules []TrustRule, globs []GlobTrustRule) *TrustRuleChecker {
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
	for _, g := range globs {
		tool, re, err := ParseGlobRule(g.Glob)
		if err != nil {
			continue // skip invalid globs (validated earlier by ValidateTrustRules)
		}
		compiled = append(compiled, compiledRule{
			tool:   tool,
			re:     re,
			action: g.Action,
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

// SecurityScanner performs security checks on tool inputs.
type SecurityScanner interface {
	Scan(ctx context.Context, toolName string, input json.RawMessage) (shouldBlock bool, reason string, err error)
}

// SecurityAwareApprovalChecker wraps an ApprovalChecker with security scanning.
// Note: security scanning is synchronous during the CheckApproval call.
type SecurityAwareApprovalChecker struct {
	base    ApprovalChecker
	scanner SecurityScanner
}

// NewSecurityAwareApprovalChecker creates an approval checker with security scanning.
func NewSecurityAwareApprovalChecker(base ApprovalChecker, scanner SecurityScanner) *SecurityAwareApprovalChecker {
	return &SecurityAwareApprovalChecker{
		base:    base,
		scanner: scanner,
	}
}

// CheckApproval runs both approval and security checks.
// It first runs the base approval check. If AutoDenied, returns immediately.
// Otherwise, runs security scan; if security scan detects an issue, returns AutoDenied.
// Scan errors are silently ignored (scan failures do not block approval).
func (s *SecurityAwareApprovalChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	// Run approval check first
	result := s.base.CheckApproval(tool, input)
	if result == AutoDenied {
		return result
	}

	// Run security scan (use background context since we can't pass context through interface)
	shouldBlock, _, err := s.scanner.Scan(context.Background(), tool, input)
	if err != nil {
		// Scan errors don't block — proceed with original approval result
		return result
	}

	if shouldBlock {
		return AutoDenied
	}

	return result
}
