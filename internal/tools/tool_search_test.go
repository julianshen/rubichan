package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSearcher struct {
	results []provider.ToolDef
}

func (m *mockSearcher) Search(query string) []provider.ToolDef {
	return m.results
}

func TestToolSearchToolName(t *testing.T) {
	tool := NewToolSearchTool(&mockSearcher{})
	assert.Equal(t, "tool_search", tool.Name())
}

func TestToolSearchToolExecute(t *testing.T) {
	searcher := &mockSearcher{
		results: []provider.ToolDef{
			{Name: "mcp-xcode-build", Description: "Build Xcode projects"},
		},
	}
	tool := NewToolSearchTool(searcher)

	input, _ := json.Marshal(map[string]string{"query": "xcode"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "mcp-xcode-build")
}

func TestToolSearchToolNoResults(t *testing.T) {
	tool := NewToolSearchTool(&mockSearcher{})

	input, _ := json.Marshal(map[string]string{"query": "nonexistent"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No deferred tools")
}
