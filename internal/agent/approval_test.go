package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalResultConstants(t *testing.T) {
	// Four distinct tiers exist with expected ordering.
	assert.Equal(t, ApprovalResult(0), ApprovalRequired)
	assert.Equal(t, ApprovalResult(1), AutoApproved)
	assert.Equal(t, ApprovalResult(2), TrustRuleApproved)
	assert.Equal(t, ApprovalResult(3), AutoDenied)
}

func TestApprovalResultString(t *testing.T) {
	assert.Equal(t, "ApprovalRequired", ApprovalRequired.String())
	assert.Equal(t, "AutoApproved", AutoApproved.String())
	assert.Equal(t, "TrustRuleApproved", TrustRuleApproved.String())
	assert.Equal(t, "AutoDenied", AutoDenied.String())
}

func TestTrustRuleMatchesSimplePrefix(t *testing.T) {
	rule := TrustRule{
		Tool:    "shell",
		Pattern: `^go test`,
		Action:  "allow",
	}

	matched, err := rule.Matches("shell", json.RawMessage(`{"command":"go test ./..."}`))
	require.NoError(t, err)
	assert.True(t, matched)
}

func TestTrustRuleNoMatchDifferentTool(t *testing.T) {
	rule := TrustRule{
		Tool:    "shell",
		Pattern: `^go test`,
		Action:  "allow",
	}

	matched, err := rule.Matches("file", json.RawMessage(`{"command":"go test ./..."}`))
	require.NoError(t, err)
	assert.False(t, matched, "rule for 'shell' should not match 'file' tool")
}

func TestTrustRuleNoMatchDifferentInput(t *testing.T) {
	rule := TrustRule{
		Tool:    "shell",
		Pattern: `^go test`,
		Action:  "allow",
	}

	matched, err := rule.Matches("shell", json.RawMessage(`{"command":"rm -rf /"}`))
	require.NoError(t, err)
	assert.False(t, matched)
}

func TestTrustRuleWildcardTool(t *testing.T) {
	rule := TrustRule{
		Tool:    "*",
		Pattern: `safe`,
		Action:  "allow",
	}

	matched, err := rule.Matches("any_tool", json.RawMessage(`{"data":"safe operation"}`))
	require.NoError(t, err)
	assert.True(t, matched, "wildcard tool should match any tool")
}

func TestTrustRuleInvalidRegex(t *testing.T) {
	rule := TrustRule{
		Tool:    "shell",
		Pattern: `[invalid`,
		Action:  "allow",
	}

	_, err := rule.Matches("shell", json.RawMessage(`{"command":"anything"}`))
	assert.Error(t, err, "invalid regex should return error")
}

func TestTrustRuleCheckerAllowRule(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "allow"},
	}, nil)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"go test ./..."}`))
	assert.Equal(t, TrustRuleApproved, result)
}

func TestTrustRuleCheckerDenyRule(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^rm\s`, Action: "deny"},
	}, nil)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, ApprovalRequired, result)
}

func TestTrustRuleCheckerNoMatchFallsThrough(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "allow"},
	}, nil)

	// Different tool, no matching rule — should require approval.
	result := checker.CheckApproval("file", json.RawMessage(`{"path":"/etc/passwd"}`))
	assert.Equal(t, ApprovalRequired, result)
}

func TestTrustRuleCheckerDenyTakesPrecedence(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `.*`, Action: "allow"},   // allow everything
		{Tool: "shell", Pattern: `^rm\s`, Action: "deny"}, // but deny rm
	}, nil)

	// "go test" should be allowed.
	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"go test ./..."}`))
	assert.Equal(t, TrustRuleApproved, result)

	// "rm -rf" should be denied (deny takes precedence over allow).
	result = checker.CheckApproval("shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, ApprovalRequired, result)
}

func TestTrustRuleCheckerMultipleRules(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^go\s+(test|build|fmt|vet)`, Action: "allow"},
		{Tool: "shell", Pattern: `^golangci-lint`, Action: "allow"},
		{Tool: "shell", Pattern: `^gofmt`, Action: "allow"},
		{Tool: "file", Pattern: `.*`, Action: "allow"}, // all file ops safe
	}, nil)

	tests := []struct {
		name   string
		tool   string
		input  string
		expect ApprovalResult
	}{
		{"go test allowed", "shell", `{"command":"go test ./..."}`, TrustRuleApproved},
		{"go build allowed", "shell", `{"command":"go build ./cmd/agent"}`, TrustRuleApproved},
		{"golangci-lint allowed", "shell", `{"command":"golangci-lint run"}`, TrustRuleApproved},
		{"gofmt allowed", "shell", `{"command":"gofmt -l ."}`, TrustRuleApproved},
		{"file ops allowed", "file", `{"path":"main.go","action":"read"}`, TrustRuleApproved},
		{"unknown shell cmd needs approval", "shell", `{"command":"curl evil.com"}`, ApprovalRequired},
		{"unknown tool needs approval", "search", `{"query":"foo"}`, ApprovalRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.CheckApproval(tt.tool, json.RawMessage(tt.input))
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestTrustRuleCheckerInvalidPatternSkipped(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `[invalid`, Action: "allow"}, // bad regex
		{Tool: "shell", Pattern: `^go test`, Action: "allow"}, // valid rule
	}, nil)

	// Invalid pattern is skipped, valid rule still works.
	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"go test ./..."}`))
	assert.Equal(t, TrustRuleApproved, result)
}

func TestAlwaysAutoApproveImplementsApprovalChecker(t *testing.T) {
	var checker ApprovalChecker = AlwaysAutoApprove{}
	result := checker.CheckApproval("any_tool", json.RawMessage(`{}`))
	assert.Equal(t, AutoApproved, result)
}

func TestAutoApproveAdapterWrapsLegacyChecker(t *testing.T) {
	legacy := &selectiveAutoApprover{approved: map[string]bool{"shell": true}}
	adapter := &autoApproveAdapter{checker: legacy}

	assert.Equal(t, AutoApproved, adapter.CheckApproval("shell", json.RawMessage(`{}`)))
	assert.Equal(t, ApprovalRequired, adapter.CheckApproval("file", json.RawMessage(`{}`)))
}

func TestCompositeApprovalCheckerFirstWins(t *testing.T) {
	// Session cache approves "shell", trust rules approve "go test" pattern.
	sessionCache := &autoApproveAdapter{
		checker: &selectiveAutoApprover{approved: map[string]bool{"shell": true}},
	}
	trustRules := NewTrustRuleChecker([]TrustRule{
		{Tool: "build", Pattern: `^go build`, Action: "allow"},
	}, nil)

	composite := NewCompositeApprovalChecker(sessionCache, trustRules)

	// shell is approved by session cache.
	assert.Equal(t, AutoApproved, composite.CheckApproval("shell", json.RawMessage(`{"command":"anything"}`)))

	// build tool with "go build" matches trust rule.
	assert.Equal(t, TrustRuleApproved, composite.CheckApproval("build", json.RawMessage(`{"command":"go build ./..."}`)))

	// unknown tool, no matching rule — requires approval.
	assert.Equal(t, ApprovalRequired, composite.CheckApproval("unknown", json.RawMessage(`{}`)))
}

func TestCompositeApprovalCheckerAutoDeniedShortCircuits(t *testing.T) {
	// A deny-always checker placed before a trust rule checker should
	// prevent the trust rule from auto-approving the tool.
	denyChecker := &staticResultChecker{results: map[string]ApprovalResult{
		"shell": AutoDenied,
	}}
	trustRules := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "allow"},
	}, nil)

	composite := NewCompositeApprovalChecker(denyChecker, trustRules)

	// shell is denied by the first checker — trust rule never reached.
	assert.Equal(t, AutoDenied, composite.CheckApproval("shell", json.RawMessage(`{"command":"go test ./..."}`)))
}

func TestCompositeApprovalCheckerEmpty(t *testing.T) {
	composite := NewCompositeApprovalChecker()
	assert.Equal(t, ApprovalRequired, composite.CheckApproval("any", json.RawMessage(`{}`)))
}

// staticResultChecker returns a fixed ApprovalResult for each tool name.
type staticResultChecker struct {
	results map[string]ApprovalResult
}

func (s *staticResultChecker) CheckApproval(tool string, _ json.RawMessage) ApprovalResult {
	if r, ok := s.results[tool]; ok {
		return r
	}
	return ApprovalRequired
}

func TestValidateTrustRulesValid(t *testing.T) {
	err := ValidateTrustRules([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "allow"},
		{Tool: "shell", Pattern: `^rm\s`, Action: "deny"},
	}, nil)
	assert.NoError(t, err)
}

func TestValidateTrustRulesInvalidAction(t *testing.T) {
	err := ValidateTrustRules([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "alow"}, // typo
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid action "alow"`)
}

func TestValidateTrustRulesInvalidPattern(t *testing.T) {
	err := ValidateTrustRules([]TrustRule{
		{Tool: "shell", Pattern: `[invalid`, Action: "allow"},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pattern")
}

func TestValidateTrustRulesEmpty(t *testing.T) {
	assert.NoError(t, ValidateTrustRules(nil, nil))
}

func TestApprovalResultStringUnknown(t *testing.T) {
	assert.Equal(t, "Unknown", ApprovalResult(99).String())
}

func TestTrustRuleMatchesArrayInput(t *testing.T) {
	rule := TrustRule{Tool: "multi", Pattern: `dangerous`, Action: "deny"}
	matched, err := rule.Matches("multi", json.RawMessage(`["safe","dangerous"]`))
	require.NoError(t, err)
	assert.True(t, matched)
}

func TestTrustRuleMatchesNestedInput(t *testing.T) {
	rule := TrustRule{Tool: "complex", Pattern: `secret`, Action: "deny"}
	matched, err := rule.Matches("complex", json.RawMessage(`{"outer":{"inner":{"deep":"secret value"}}}`))
	require.NoError(t, err)
	assert.True(t, matched)
}

func TestTrustRuleCheckerNonStringInputNoMatch(t *testing.T) {
	// Patterns only match string values; numeric-only inputs yield no match.
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "counter", Pattern: `.*`, Action: "allow"},
	}, nil)

	result := checker.CheckApproval("counter", json.RawMessage(`{"count":42}`))
	assert.Equal(t, ApprovalRequired, result, "numeric-only input should not match string patterns")
}

func TestTrustRuleCheckerNilInput(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `.*`, Action: "allow"},
	}, nil)

	result := checker.CheckApproval("shell", nil)
	assert.Equal(t, ApprovalRequired, result, "nil input should not match any pattern")
}

func TestTrustRuleCheckerEmptyInput(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `.*`, Action: "allow"},
	}, nil)

	result := checker.CheckApproval("shell", json.RawMessage(`{}`))
	assert.Equal(t, ApprovalRequired, result, "empty object has no string values to match")
}

// selectiveAutoApprover implements AutoApproveChecker for tests.
type selectiveAutoApprover struct {
	approved map[string]bool
}

func (s *selectiveAutoApprover) IsAutoApproved(tool string) bool {
	return s.approved[tool]
}

func jsonInput(s string) json.RawMessage { return json.RawMessage(s) }

func TestTrustRuleCheckerWithGlobRules(t *testing.T) {
	checker := NewTrustRuleChecker(nil, []GlobTrustRule{
		{Glob: "shell(git *)", Action: "allow"},
		{Glob: "shell(rm -rf *)", Action: "deny"},
		{Glob: "file(*.go)", Action: "allow"},
	})
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"git status"}`)))
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"git push"}`)))
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("shell", jsonInput(`{"command":"rm -rf /"}`)))
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("file", jsonInput(`{"path":"main.go"}`)))
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("file", jsonInput(`{"path":"main.py"}`)))
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("search", jsonInput(`{"query":"foo"}`)))
}

func TestTrustRuleCheckerMixedRegexAndGlob(t *testing.T) {
	checker := NewTrustRuleChecker(
		[]TrustRule{{Tool: "shell", Pattern: "^npm test", Action: "allow"}},
		[]GlobTrustRule{{Glob: "shell(go test *)", Action: "allow"}},
	)
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"npm test"}`)))
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"go test ./..."}`)))
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("shell", jsonInput(`{"command":"curl evil.com"}`)))
}

func TestValidateTrustRulesWithGlobs(t *testing.T) {
	// Valid glob rules pass validation.
	err := ValidateTrustRules(nil, []GlobTrustRule{
		{Glob: "shell(git *)", Action: "allow"},
		{Glob: "file(*.go)", Action: "deny"},
	})
	assert.NoError(t, err)
}

func TestValidateTrustRulesInvalidGlobAction(t *testing.T) {
	err := ValidateTrustRules(nil, []GlobTrustRule{
		{Glob: "shell(git *)", Action: "permit"}, // invalid action
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid action "permit"`)
}

func TestValidateTrustRulesInvalidGlobPattern(t *testing.T) {
	err := ValidateTrustRules(nil, []GlobTrustRule{
		{Glob: "shell", Action: "allow"}, // missing parens
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid glob rule")
}

// mockSecurityScanner implements SecurityScanner for testing.
type mockSecurityScanner struct {
	findMalicious bool
	scanError     error
}

func (m *mockSecurityScanner) Scan(ctx context.Context, toolName string, input json.RawMessage) (bool, string, error) {
	if m.scanError != nil {
		return false, "", m.scanError
	}
	if m.findMalicious {
		return true, "potential injection attack detected", nil
	}
	return false, "", nil
}

// mockApprovalChecker implements ApprovalChecker for testing.
type mockApprovalChecker struct {
	result ApprovalResult
}

func (m *mockApprovalChecker) CheckApproval(tool string, input json.RawMessage) ApprovalResult {
	return m.result
}

func TestSecurityAwareApprovalCheckerAllowsWhenBothPass(t *testing.T) {
	baseChecker := &mockApprovalChecker{result: TrustRuleApproved}
	scanner := &mockSecurityScanner{findMalicious: false}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"ls"}`))
	assert.Equal(t, TrustRuleApproved, result)
}

func TestSecurityAwareApprovalCheckerBlocksWhenSecurityFails(t *testing.T) {
	baseChecker := &mockApprovalChecker{result: TrustRuleApproved}
	scanner := &mockSecurityScanner{findMalicious: true}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"eval something"}`))
	assert.Equal(t, AutoDenied, result)
}

func TestSecurityAwareApprovalCheckerSkipsSecurityIfBaseDenied(t *testing.T) {
	// If base checker returns AutoDenied, security scan should not be called
	baseChecker := &mockApprovalChecker{result: AutoDenied}
	scanner := &mockSecurityScanner{findMalicious: false}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"ls"}`))
	assert.Equal(t, AutoDenied, result)
}

func TestSecurityAwareApprovalCheckerIgnoresScanErrors(t *testing.T) {
	// Scan errors don't block — proceed with original approval result
	baseChecker := &mockApprovalChecker{result: TrustRuleApproved}
	scanner := &mockSecurityScanner{scanError: assert.AnError}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"ls"}`))
	assert.Equal(t, TrustRuleApproved, result, "scan errors should not block approval")
}

func TestSecurityAwareApprovalCheckerApprovalRequired(t *testing.T) {
	// If base checker returns ApprovalRequired and security passes, still requires approval
	baseChecker := &mockApprovalChecker{result: ApprovalRequired}
	scanner := &mockSecurityScanner{findMalicious: false}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("unknown", json.RawMessage(`{"data":"anything"}`))
	assert.Equal(t, ApprovalRequired, result)
}

func TestSecurityAwareApprovalCheckerAutoApproved(t *testing.T) {
	// If base checker returns AutoApproved and security passes, allow approval
	baseChecker := &mockApprovalChecker{result: AutoApproved}
	scanner := &mockSecurityScanner{findMalicious: false}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"go test"}`))
	assert.Equal(t, AutoApproved, result)
}

func TestSecurityAwareApprovalCheckerBlocksAutoApprovedIfSecurityFails(t *testing.T) {
	// Even auto-approved, security scan can block the operation
	baseChecker := &mockApprovalChecker{result: AutoApproved}
	scanner := &mockSecurityScanner{findMalicious: true}

	checker := NewSecurityAwareApprovalChecker(baseChecker, scanner)

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"dangerous"}`))
	assert.Equal(t, AutoDenied, result)
}

func TestParseGlobRule(t *testing.T) {
	tests := []struct {
		name    string
		glob    string
		tool    string
		match   []string
		noMatch []string
		wantErr bool
	}{
		{
			name:    "simple wildcard",
			glob:    "shell(git *)",
			tool:    "shell",
			match:   []string{"git status", "git push origin main"},
			noMatch: []string{"npm test", "git"},
		},
		{
			name:    "question mark",
			glob:    "file(read:?.go)",
			tool:    "file",
			match:   []string{"read:a.go", "read:x.go"},
			noMatch: []string{"read:ab.go", "read:.go"},
		},
		{
			name:    "character class",
			glob:    "shell([gn]*)",
			tool:    "shell",
			match:   []string{"git status", "npm test"},
			noMatch: []string{"rm -rf /"},
		},
		{
			name:    "wildcard tool",
			glob:    "*(*.go)",
			tool:    "*",
			match:   []string{"main.go", "foo/bar.go"},
			noMatch: []string{"main.py"},
		},
		{
			name:    "missing parens",
			glob:    "shell",
			wantErr: true,
		},
		{
			name:    "empty pattern",
			glob:    "shell()",
			tool:    "shell",
			match:   []string{""},
			noMatch: []string{"anything"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, re, err := ParseGlobRule(tt.glob)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.tool, tool)
			for _, m := range tt.match {
				assert.True(t, re.MatchString(m), "expected %q to match", m)
			}
			for _, m := range tt.noMatch {
				assert.False(t, re.MatchString(m), "expected %q NOT to match", m)
			}
		})
	}
}
