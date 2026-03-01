package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCompactor struct {
	called bool
}

func (m *mockCompactor) ForceCompact(_ context.Context) CompactResult {
	m.called = true
	return CompactResult{
		BeforeTokens:   9500,
		AfterTokens:    4200,
		BeforeMsgCount: 40,
		AfterMsgCount:  22,
		StrategiesRun:  []string{"tool_result_clearing", "summarization"},
	}
}

func TestCompactContextToolName(t *testing.T) {
	tool := NewCompactContextTool(&mockCompactor{})
	assert.Equal(t, "compact_context", tool.Name())
}

func TestCompactContextToolDescription(t *testing.T) {
	tool := NewCompactContextTool(&mockCompactor{})
	assert.NotEmpty(t, tool.Description())
}

func TestCompactContextToolInputSchema(t *testing.T) {
	tool := NewCompactContextTool(&mockCompactor{})
	schema := tool.InputSchema()
	assert.True(t, json.Valid(schema))
}

func TestCompactContextToolExecute(t *testing.T) {
	compactor := &mockCompactor{}
	tool := NewCompactContextTool(compactor)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, compactor.called)
	assert.Contains(t, result.Content, "9500")
	assert.Contains(t, result.Content, "4200")
}

func TestCompactContextToolImplementsToolInterface(t *testing.T) {
	compactor := &mockCompactor{}
	tool := NewCompactContextTool(compactor)

	// Verify it satisfies the Tool interface.
	var _ Tool = tool
}
