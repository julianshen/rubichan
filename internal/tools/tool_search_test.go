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

func TestToolSearchToolShowsHints(t *testing.T) {
	searcher := &mockSearcher{
		results: []provider.ToolDef{
			{
				Name:        "http_get",
				Description: "Fetch HTTP resources.",
				SearchHint:  "api rest endpoint webhook fetch",
			},
		},
	}
	tool := NewToolSearchTool(searcher)

	input, _ := json.Marshal(map[string]string{"query": "api"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "http_get")
	assert.Contains(t, result.Content, "Hints: api rest endpoint webhook fetch")
}

func TestToolSearchToolOmitsEmptyHints(t *testing.T) {
	searcher := &mockSearcher{
		results: []provider.ToolDef{
			{Name: "plain_tool", Description: "A tool with no hints."},
		},
	}
	tool := NewToolSearchTool(searcher)

	input, _ := json.Marshal(map[string]string{"query": "plain"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "plain_tool")
	assert.NotContains(t, result.Content, "Hints:")
}

func TestToolSearchToolNoResults(t *testing.T) {
	tool := NewToolSearchTool(&mockSearcher{})

	input, _ := json.Marshal(map[string]string{"query": "nonexistent"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No deferred tools")
}
