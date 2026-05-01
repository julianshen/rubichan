package permissions

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
)

// staticChecker is a test ApprovalChecker that returns a fixed result.
type staticChecker struct {
	result agentsdk.ApprovalResult
}

func (s *staticChecker) CheckApproval(_ string, _ json.RawMessage) agentsdk.ApprovalResult {
	return s.result
}

func TestModeAwareChecker_ModePlan_ReadOnlyApproved(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("read_file", json.RawMessage(`{"path":"x.go"}`))
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestModeAwareChecker_ModePlan_WriteRequiresApproval(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("write_file", json.RawMessage(`{"path":"x.go"}`))
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestModeAwareChecker_ModeFullAuto_ApprovesAllExceptDenied(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeFullAuto, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("write_file", json.RawMessage(`{"path":"x.go"}`))
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestModeAwareChecker_ModeBypass_ApprovesAll(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeBypass, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("rm_rf", json.RawMessage(`{"path":"/"}`))
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestModeAwareChecker_DenyAlwaysWins(t *testing.T) {
	for _, mode := range []agentsdk.PermissionMode{agentsdk.ModePlan, agentsdk.ModeAuto, agentsdk.ModeFullAuto, agentsdk.ModeBypass} {
		checker := NewModeAwareChecker(mode, &staticChecker{result: agentsdk.AutoDenied})
		result := checker.CheckApproval("read_file", json.RawMessage(`{}`))
		assert.Equal(t, agentsdk.AutoDenied, result, "mode %s should not override deny", mode)
	}
}

func TestModeAwareChecker_PolicyApprovedRespected(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &staticChecker{result: agentsdk.TrustRuleApproved})
	result := checker.CheckApproval("write_file", json.RawMessage(`{}`))
	assert.Equal(t, agentsdk.TrustRuleApproved, result)
}

// explainerChecker is a test ApprovalChecker that also implements Explainer.
type explainerChecker struct {
	result  agentsdk.ApprovalResult
	explain string
}

func (e *explainerChecker) CheckApproval(_ string, _ json.RawMessage) agentsdk.ApprovalResult {
	return e.result
}

func (e *explainerChecker) Explain(_ string, _ json.RawMessage) string {
	return e.explain
}

func TestModeAwareChecker_ExplainDelegates(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &explainerChecker{result: agentsdk.AutoApproved, explain: "because test"})
	result := checker.Explain("tool", json.RawMessage(`{}`))
	assert.Equal(t, "because test", result)
}

func TestModeAwareChecker_ExplainNoExplainer(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &staticChecker{result: agentsdk.AutoApproved})
	result := checker.Explain("tool", json.RawMessage(`{}`))
	assert.Equal(t, "", result)
}

func TestModeAwareChecker_ModeAuto_ReadOnlyApproved(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeAuto, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("read_file", json.RawMessage(`{"path":"x.go"}`))
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestModeAwareChecker_ModeAuto_WriteRequiresApproval(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeAuto, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("write_file", json.RawMessage(`{"path":"x.go"}`))
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestModeAwareChecker_ModeAuto_PolicyApprovedRespected(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeAuto, &staticChecker{result: agentsdk.TrustRuleApproved})
	result := checker.CheckApproval("write_file", json.RawMessage(`{}`))
	assert.Equal(t, agentsdk.TrustRuleApproved, result)
}

func TestModeAwareChecker_EmptyTool(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("", json.RawMessage(`{}`))
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestModeAwareChecker_NilInput(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModePlan, &staticChecker{result: agentsdk.ApprovalRequired})
	result := checker.CheckApproval("read_file", nil)
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestModeAwareChecker_AutoApprovedPreservedInFullAuto(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeFullAuto, &staticChecker{result: agentsdk.AutoApproved})
	result := checker.CheckApproval("write_file", json.RawMessage(`{}`))
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestModeAwareChecker_TrustRuleApprovedPreservedInBypass(t *testing.T) {
	checker := NewModeAwareChecker(agentsdk.ModeBypass, &staticChecker{result: agentsdk.TrustRuleApproved})
	result := checker.CheckApproval("write_file", json.RawMessage(`{}`))
	assert.Equal(t, agentsdk.TrustRuleApproved, result)
}
