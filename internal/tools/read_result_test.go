package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRetriever struct {
	data map[string]string
}

func (m *mockRetriever) Retrieve(refID string) (string, error) {
	if v, ok := m.data[refID]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", refID)
}

func TestReadResultToolName(t *testing.T) {
	tool := NewReadResultTool(&mockRetriever{})
	assert.Equal(t, "read_result", tool.Name())
}

func TestReadResultToolDescription(t *testing.T) {
	tool := NewReadResultTool(&mockRetriever{})
	assert.NotEmpty(t, tool.Description())
}

func TestReadResultToolInputSchema(t *testing.T) {
	tool := NewReadResultTool(&mockRetriever{})
	var schema map[string]interface{}
	err := json.Unmarshal(tool.InputSchema(), &schema)
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])
}

func TestReadResultToolExecute(t *testing.T) {
	retriever := &mockRetriever{data: map[string]string{
		"ref-1": "line1\nline2\nline3\nline4\nline5",
	}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1", "offset": 0, "limit": 1024})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "line1")
}

func TestReadResultToolOffsetLimit(t *testing.T) {
	retriever := &mockRetriever{data: map[string]string{
		"ref-1": "abcdefghij",
	}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1", "offset": 3, "limit": 4})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "defg", result.Content)
}

func TestReadResultToolRefIDNotFound(t *testing.T) {
	retriever := &mockRetriever{data: map[string]string{}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "nonexistent"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "retrieval error")
}

func TestReadResultToolOffsetBeyondContent(t *testing.T) {
	retriever := &mockRetriever{data: map[string]string{
		"ref-1": "short",
	}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1", "offset": 100})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "", result.Content)
}

func TestReadResultToolDefaultLimit(t *testing.T) {
	// When limit is 0, the default (4096) should be used.
	retriever := &mockRetriever{data: map[string]string{
		"ref-1": "hello world",
	}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1", "offset": 0, "limit": 0})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "hello world", result.Content)
}

func TestReadResultToolInvalidJSON(t *testing.T) {
	tool := NewReadResultTool(&mockRetriever{})

	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestReadResultToolNilRetriever(t *testing.T) {
	tool := NewReadResultTool(nil)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not initialized")
}
