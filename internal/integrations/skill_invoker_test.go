package integrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockWorkflowInvoker struct {
	name   string
	input  map[string]any
	result map[string]any
	err    error
}

func (m *mockWorkflowInvoker) InvokeWorkflow(_ context.Context, name string, args map[string]any) (map[string]any, error) {
	m.name = name
	m.input = args
	return m.result, m.err
}

func TestSkillInvokerSuccess(t *testing.T) {
	mock := &mockWorkflowInvoker{
		result: map[string]any{"output": "done"},
	}

	invoker := NewSkillInvoker(mock)
	result, err := invoker.Invoke(context.Background(), "other-skill", map[string]any{"key": "value"})
	require.NoError(t, err)
	assert.Equal(t, "done", result["output"])
	assert.Equal(t, "other-skill", mock.name)
	assert.Equal(t, "value", mock.input["key"])
}

func TestSkillInvokerNilInvoker(t *testing.T) {
	invoker := NewSkillInvoker(nil)
	_, err := invoker.Invoke(context.Background(), "test", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestSetInvokerReplaces(t *testing.T) {
	// Start with nil invoker — should fail.
	invoker := NewSkillInvoker(nil)
	_, err := invoker.Invoke(context.Background(), "test", nil)
	require.Error(t, err)

	// Wire a real invoker — should succeed.
	mock := &mockWorkflowInvoker{
		result: map[string]any{"status": "ok"},
	}
	invoker.SetInvoker(mock)
	result, err := invoker.Invoke(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
}

func TestSetInvokerConcurrent(t *testing.T) {
	invoker := NewSkillInvoker(nil)
	mock := &mockWorkflowInvoker{
		result: map[string]any{"v": "1"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		invoker.SetInvoker(mock)
	}()
	<-done

	result, err := invoker.Invoke(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Equal(t, "1", result["v"])
}
