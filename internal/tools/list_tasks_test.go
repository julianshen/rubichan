package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTaskStatusProvider struct {
	statuses []BackgroundTaskInfo
}

func (f *fakeTaskStatusProvider) BackgroundTaskStatus() []BackgroundTaskInfo {
	return f.statuses
}

func TestListTasksToolName(t *testing.T) {
	tool := NewListTasksTool(nil)
	assert.Equal(t, "list_tasks", tool.Name())
}

func TestListTasksToolDescription(t *testing.T) {
	tool := NewListTasksTool(nil)
	assert.NotEmpty(t, tool.Description())
}

func TestListTasksToolInputSchema(t *testing.T) {
	tool := NewListTasksTool(nil)
	schema := tool.InputSchema()
	var parsed map[string]interface{}
	err := json.Unmarshal(schema, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "object", parsed["type"])
}

func TestListTasksToolEmpty(t *testing.T) {
	provider := &fakeTaskStatusProvider{statuses: nil}
	tool := NewListTasksTool(provider)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "No background tasks running.", result.Content)
}

func TestListTasksToolWithPending(t *testing.T) {
	provider := &fakeTaskStatusProvider{
		statuses: []BackgroundTaskInfo{
			{ID: "abc123", AgentName: "explorer", Status: "running"},
			{ID: "def456", AgentName: "coder", Status: "running"},
		},
	}
	tool := NewListTasksTool(provider)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "2 background task(s):")
	assert.Contains(t, result.Content, "abc123 [explorer] running")
	assert.Contains(t, result.Content, "def456 [coder] running")
}
