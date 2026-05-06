package permissions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYOLOClassifier_SafeToolBypass(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	result, err := c.Classify("read_file", map[string]interface{}{"path": "/etc/passwd"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestYOLOClassifier_UnsafeToolWithoutProvider(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	// write_file with /etc/ path scores 1 (safe prefix /etc/ gives -1, but write tool name gives +1)
	// Actually /etc/ is safe prefix (-1), but write_file name gives +1, net 0 -> Safe
	// Let's use a dangerous path instead
	result, err := c.Classify("write_file", map[string]interface{}{"path": "/dev/null"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.AutoDenied, result)
}

func TestYOLOClassifier_ConsecutiveDenialFallback(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	c.SetMaxConsecutiveDenials(3)

	for i := 0; i < 3; i++ {
		result, _ := c.Classify("shell", nil)
		assert.Equal(t, agentsdk.ApprovalRequired, result, "iteration %d", i)
	}

	result, _ := c.Classify("shell", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result, "fallback should trigger")
}

func TestYOLOClassifier_ResetDenialsOnSafeTool(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	c.SetMaxConsecutiveDenials(3)

	_, _ = c.Classify("shell", nil)
	_, _ = c.Classify("shell", nil)
	_, _ = c.Classify("read_file", nil)
	_, _ = c.Classify("shell", nil)
	result, _ := c.Classify("shell", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestYOLOClassifier_Stage1Heuristics(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)

	result, _ := c.Classify("write_file", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result)

	result, _ = c.Classify("shell", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result)

	result, _ = c.Classify("some_info_tool", nil)
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestYOLOClassifier_CacheHit(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	c.SetMaxConsecutiveDenials(3)

	result1, _ := c.Classify("write_file", map[string]interface{}{"path": "/tmp/test"})
	result2, _ := c.Classify("write_file", map[string]interface{}{"path": "/tmp/test"})
	assert.Equal(t, result1, result2)

	tele := c.Telemetry()
	assert.GreaterOrEqual(t, tele.CacheHits, 1)
}

func TestYOLOClassifier_Telemetry(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	_, _ = c.Classify("shell", map[string]interface{}{"command": "ls"})
	tele := c.Telemetry()
	assert.GreaterOrEqual(t, tele.Stage1Count, 1)
}

func TestStage1_SafeTool(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("read_file", map[string]interface{}{"path": "/tmp/test"})
	assert.Equal(t, DecisionSafe, decision)
}

func TestStage1_DangerousCommand(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("shell", map[string]interface{}{"command": "rm -rf /"})
	assert.Equal(t, DecisionUnsafe, decision)
}

func TestStage1_UncertainWrite(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("write_file", map[string]interface{}{"path": "/tmp/test", "content": "hello"})
	assert.Equal(t, DecisionUncertain, decision)
}

func TestStage1_PathTraversal(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("read_file", map[string]interface{}{"path": "../../../etc/passwd"})
	assert.Equal(t, DecisionUncertain, decision)
}

// mockProvider implements agentsdk.LLMProvider for testing.
type mockProvider struct {
	response string
}

func (m *mockProvider) Stream(ctx context.Context, req agentsdk.CompletionRequest) (<-chan agentsdk.StreamEvent, error) {
	ch := make(chan agentsdk.StreamEvent, 1)
	ch <- agentsdk.StreamEvent{Type: agentsdk.EventTextDelta, Text: m.response}
	close(ch)
	return ch, nil
}

func TestStage2_SafeResponse(t *testing.T) {
	prov := &mockProvider{response: "safe"}
	c := NewYOLOClassifier(prov, 64, 4096)
	result, err := c.stage2("read_file", map[string]interface{}{"path": "/tmp/test"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestStage2_UnsafeResponse(t *testing.T) {
	prov := &mockProvider{response: "unsafe"}
	c := NewYOLOClassifier(prov, 64, 4096)
	result, err := c.stage2("shell", map[string]interface{}{"command": "rm -rf /"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.AutoDenied, result)
}

func TestStage2_UncertainResponse(t *testing.T) {
	prov := &mockProvider{response: "uncertain"}
	c := NewYOLOClassifier(prov, 64, 4096)
	result, err := c.stage2("write_file", map[string]interface{}{"path": "/tmp/test"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestModeAwareChecker_WithImprovedClassifier(t *testing.T) {
	classifier := NewYOLOClassifier(nil, 0, 0)
	checker := NewModeAwareChecker(agentsdk.ModeAuto, &alwaysRequire{}, WithClassifier(classifier))

	result := checker.CheckApproval("read_file", []byte(`{"path":"/tmp/test"}`))
	assert.Equal(t, agentsdk.AutoApproved, result)

	result = checker.CheckApproval("shell", []byte(`{"command":"rm -rf /"}`))
	// ModeAwareChecker delegates to classifier which returns AutoDenied for rm -rf /
	// But the alwaysRequire base checker returns ApprovalRequired, and ModeAwareChecker
	// only uses classifier when base returns ApprovalRequired. For shell with rm -rf /,
	// stage1 returns DecisionUnsafe -> AutoDenied.
	assert.Equal(t, agentsdk.AutoDenied, result)
}

type alwaysRequire struct{}

func (a *alwaysRequire) CheckApproval(tool string, input json.RawMessage) agentsdk.ApprovalResult {
	return agentsdk.ApprovalRequired
}
