package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApprovalResultString(t *testing.T) {
	tests := []struct {
		result ApprovalResult
		want   string
	}{
		{ApprovalRequired, "ApprovalRequired"},
		{AutoApproved, "AutoApproved"},
		{TrustRuleApproved, "TrustRuleApproved"},
		{AutoDenied, "AutoDenied"},
		{ApprovalResult(99), "Unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.result.String())
	}
}

func TestAlwaysAutoApproveChecker(t *testing.T) {
	var checker AlwaysAutoApprove

	assert.True(t, checker.IsAutoApproved("any_tool"))
	assert.Equal(t, AutoApproved, checker.CheckApproval("any_tool", nil))
}

func TestAllowAllParallel(t *testing.T) {
	var policy AllowAllParallel
	assert.True(t, policy.CanParallelize("shell"))
	assert.True(t, policy.CanParallelize("file"))
}

func TestCompositeApprovalCheckerFirstWins(t *testing.T) {
	// First checker returns AutoApproved for "safe_tool".
	first := &mockApprovalChecker{results: map[string]ApprovalResult{
		"safe_tool": AutoApproved,
	}}
	// Second checker would deny, but first wins.
	second := &mockApprovalChecker{results: map[string]ApprovalResult{
		"safe_tool": AutoDenied,
	}}

	composite := NewCompositeApprovalChecker(first, second)
	assert.Equal(t, AutoApproved, composite.CheckApproval("safe_tool", nil))
}

func TestCompositeApprovalCheckerFallsThrough(t *testing.T) {
	// First checker has no opinion (returns ApprovalRequired).
	first := &mockApprovalChecker{results: map[string]ApprovalResult{}}
	// Second checker approves.
	second := &mockApprovalChecker{results: map[string]ApprovalResult{
		"tool_x": TrustRuleApproved,
	}}

	composite := NewCompositeApprovalChecker(first, second)
	assert.Equal(t, TrustRuleApproved, composite.CheckApproval("tool_x", nil))
}

func TestCompositeApprovalCheckerAllRequireApproval(t *testing.T) {
	first := &mockApprovalChecker{results: map[string]ApprovalResult{}}
	composite := NewCompositeApprovalChecker(first)
	assert.Equal(t, ApprovalRequired, composite.CheckApproval("unknown", nil))
}

type mockApprovalChecker struct {
	results map[string]ApprovalResult
}

func (m *mockApprovalChecker) CheckApproval(tool string, _ json.RawMessage) ApprovalResult {
	if r, ok := m.results[tool]; ok {
		return r
	}
	return ApprovalRequired
}
