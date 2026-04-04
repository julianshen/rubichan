package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionSignalToolName(t *testing.T) {
	tool := NewCompletionSignalTool()
	assert.Equal(t, "task_complete", tool.Name())
}

func TestCompletionSignalToolDescription(t *testing.T) {
	tool := NewCompletionSignalTool()
	assert.Contains(t, tool.Description(), "task is complete")
}

func TestCompletionSignalToolInputSchema(t *testing.T) {
	tool := NewCompletionSignalTool()
	schema := tool.InputSchema()
	assert.NotNil(t, schema)

	var parsed map[string]interface{}
	err := json.Unmarshal(schema, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "object", parsed["type"])

	props, ok := parsed["properties"].(map[string]interface{})
	require.True(t, ok)
	_, hasSummary := props["summary"]
	assert.True(t, hasSummary)
}

func TestCompletionSignalToolExecute(t *testing.T) {
	tool := NewCompletionSignalTool()
	input, _ := json.Marshal(map[string]string{"summary": "Implemented the feature and ran tests"})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "Implemented the feature and ran tests", result.Content)
}

func TestCompletionSignalToolExecuteInvalidInput(t *testing.T) {
	tool := NewCompletionSignalTool()

	result, err := tool.Execute(context.Background(), json.RawMessage(`not json`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
