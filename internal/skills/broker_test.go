package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- CapabilityBroker tests ---

func TestCapabilityBrokerAllowsPermittedExecution(t *testing.T) {
	checker := &stubPermissionChecker{}
	broker := NewCapabilityBroker("my-skill", checker, []Permission{PermFileRead})

	err := broker.CheckExecution(context.Background(), "read_file", json.RawMessage(`{}`))
	assert.NoError(t, err)
}

func TestCapabilityBrokerDeniesUnpermittedExecution(t *testing.T) {
	checker := &stubPermissionChecker{
		denied: map[Permission]bool{PermShellExec: true},
	}
	broker := NewCapabilityBroker("my-skill", checker, []Permission{PermFileRead, PermShellExec})

	err := broker.CheckExecution(context.Background(), "run_shell", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my-skill")
	assert.Contains(t, err.Error(), "run_shell")
	assert.Contains(t, err.Error(), "shell:exec")
}

func TestCapabilityBrokerChecksAllDeclaredPermissions(t *testing.T) {
	checker := &countingChecker{}
	perms := []Permission{PermFileRead, PermShellExec, PermNetFetch}
	broker := NewCapabilityBroker("s", checker, perms)

	_ = broker.CheckExecution(context.Background(), "t", nil)
	assert.Equal(t, 3, checker.count, "should check all declared permissions")
}

func TestCapabilityBrokerCopiesPermissions(t *testing.T) {
	perms := []Permission{PermFileRead}
	broker := NewCapabilityBroker("s", &stubPermissionChecker{}, perms)

	// Mutate the original slice — should not affect the broker.
	perms[0] = PermShellExec

	err := broker.CheckExecution(context.Background(), "t", nil)
	assert.NoError(t, err, "broker should use its own copy of permissions")
}

// --- BrokeredTool tests ---

func TestBrokeredToolDelegatesToInner(t *testing.T) {
	inner := &stubTool{name: "read_file", result: tools.ToolResult{Content: "ok"}}
	broker := &stubBroker{allow: true}
	bt := NewBrokeredTool(inner, broker)

	result, err := bt.Execute(context.Background(), json.RawMessage(`{}`))
	assert.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
	assert.True(t, inner.called, "inner tool should be called")
}

func TestBrokeredToolBlocksOnBrokerDenial(t *testing.T) {
	inner := &stubTool{name: "shell", result: tools.ToolResult{Content: "ok"}}
	broker := &stubBroker{allow: false, err: fmt.Errorf("denied")}
	bt := NewBrokeredTool(inner, broker)

	result, err := bt.Execute(context.Background(), json.RawMessage(`{}`))
	assert.NoError(t, err, "should return tool error, not Go error")
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "denied")
	assert.False(t, inner.called, "inner tool should NOT be called when denied")
}

func TestBrokeredToolPreservesMetadata(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	inner := &stubTool{name: "my_tool", desc: "does stuff", schema: schema}
	bt := NewBrokeredTool(inner, &stubBroker{allow: true})

	assert.Equal(t, "my_tool", bt.Name())
	assert.Equal(t, "does stuff", bt.Description())
	assert.Equal(t, schema, bt.InputSchema())
}

func TestBrokeredToolInnerAccessor(t *testing.T) {
	inner := &stubTool{name: "x"}
	bt := NewBrokeredTool(inner, &stubBroker{allow: true})
	assert.Equal(t, inner, bt.Inner())
}

// --- test helpers ---

type stubPermissionChecker struct {
	denied map[Permission]bool
}

func (s *stubPermissionChecker) CheckPermission(perm Permission) error {
	if s.denied[perm] {
		return fmt.Errorf("permission %s not granted", perm)
	}
	return nil
}

func (s *stubPermissionChecker) CheckRateLimit(_ string) error { return nil }
func (s *stubPermissionChecker) ResetTurnLimits()              {}

type countingChecker struct {
	count int
}

func (c *countingChecker) CheckPermission(_ Permission) error {
	c.count++
	return nil
}
func (c *countingChecker) CheckRateLimit(_ string) error { return nil }
func (c *countingChecker) ResetTurnLimits()              {}

type stubBroker struct {
	allow bool
	err   error
}

func (s *stubBroker) CheckExecution(_ context.Context, _ string, _ json.RawMessage) error {
	if !s.allow {
		return s.err
	}
	return nil
}

type stubTool struct {
	name   string
	desc   string
	schema json.RawMessage
	result tools.ToolResult
	called bool
}

func (s *stubTool) Name() string                 { return s.name }
func (s *stubTool) Description() string          { return s.desc }
func (s *stubTool) InputSchema() json.RawMessage { return s.schema }
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	s.called = true
	return s.result, nil
}
