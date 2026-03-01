package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalResultConstants(t *testing.T) {
	// Three distinct tiers exist with expected ordering.
	assert.Equal(t, ApprovalResult(0), ApprovalRequired)
	assert.Equal(t, ApprovalResult(1), AutoApproved)
	assert.Equal(t, ApprovalResult(2), TrustRuleApproved)
}

func TestApprovalResultString(t *testing.T) {
	assert.Equal(t, "ApprovalRequired", ApprovalRequired.String())
	assert.Equal(t, "AutoApproved", AutoApproved.String())
	assert.Equal(t, "TrustRuleApproved", TrustRuleApproved.String())
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
	})

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"go test ./..."}`))
	assert.Equal(t, TrustRuleApproved, result)
}

func TestTrustRuleCheckerDenyRule(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^rm\s`, Action: "deny"},
	})

	result := checker.CheckApproval("shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, ApprovalRequired, result)
}

func TestTrustRuleCheckerNoMatchFallsThrough(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "allow"},
	})

	// Different tool, no matching rule — should require approval.
	result := checker.CheckApproval("file", json.RawMessage(`{"path":"/etc/passwd"}`))
	assert.Equal(t, ApprovalRequired, result)
}

func TestTrustRuleCheckerDenyTakesPrecedence(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `.*`, Action: "allow"},   // allow everything
		{Tool: "shell", Pattern: `^rm\s`, Action: "deny"}, // but deny rm
	})

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
	})

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
	})

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
	})

	composite := NewCompositeApprovalChecker(sessionCache, trustRules)

	// shell is approved by session cache.
	assert.Equal(t, AutoApproved, composite.CheckApproval("shell", json.RawMessage(`{"command":"anything"}`)))

	// build tool with "go build" matches trust rule.
	assert.Equal(t, TrustRuleApproved, composite.CheckApproval("build", json.RawMessage(`{"command":"go build ./..."}`)))

	// unknown tool, no matching rule — requires approval.
	assert.Equal(t, ApprovalRequired, composite.CheckApproval("unknown", json.RawMessage(`{}`)))
}

func TestCompositeApprovalCheckerEmpty(t *testing.T) {
	composite := NewCompositeApprovalChecker()
	assert.Equal(t, ApprovalRequired, composite.CheckApproval("any", json.RawMessage(`{}`)))
}

func TestValidateTrustRulesValid(t *testing.T) {
	err := ValidateTrustRules([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "allow"},
		{Tool: "shell", Pattern: `^rm\s`, Action: "deny"},
	})
	assert.NoError(t, err)
}

func TestValidateTrustRulesInvalidAction(t *testing.T) {
	err := ValidateTrustRules([]TrustRule{
		{Tool: "shell", Pattern: `^go test`, Action: "alow"}, // typo
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid action "alow"`)
}

func TestValidateTrustRulesInvalidPattern(t *testing.T) {
	err := ValidateTrustRules([]TrustRule{
		{Tool: "shell", Pattern: `[invalid`, Action: "allow"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pattern")
}

func TestValidateTrustRulesEmpty(t *testing.T) {
	assert.NoError(t, ValidateTrustRules(nil))
}

func TestTrustRuleCheckerNonStringInputNoMatch(t *testing.T) {
	// Patterns only match string values; numeric-only inputs yield no match.
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "counter", Pattern: `.*`, Action: "allow"},
	})

	result := checker.CheckApproval("counter", json.RawMessage(`{"count":42}`))
	assert.Equal(t, ApprovalRequired, result, "numeric-only input should not match string patterns")
}

func TestTrustRuleCheckerNilInput(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `.*`, Action: "allow"},
	})

	result := checker.CheckApproval("shell", nil)
	assert.Equal(t, ApprovalRequired, result, "nil input should not match any pattern")
}

func TestTrustRuleCheckerEmptyInput(t *testing.T) {
	checker := NewTrustRuleChecker([]TrustRule{
		{Tool: "shell", Pattern: `.*`, Action: "allow"},
	})

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
