package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// ToolSearcher finds deferred tools by query.
type ToolSearcher interface {
	Search(query string) []provider.ToolDef
}

// ToolSearchTool allows the LLM to discover deferred tool descriptions.
type ToolSearchTool struct {
	searcher ToolSearcher
}

// NewToolSearchTool creates a new ToolSearchTool.
func NewToolSearchTool(s ToolSearcher) *ToolSearchTool {
	return &ToolSearchTool{searcher: s}
}

func (t *ToolSearchTool) Name() string { return "tool_search" }

func (t *ToolSearchTool) Description() string {
	return "Search for tools that have been deferred to save context. Returns tool names, descriptions, and schemas."
}

func (t *ToolSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search keyword to match tool names or descriptions"}
		},
		"required": ["query"]
	}`)
}

func (t *ToolSearchTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	results := t.searcher.Search(params.Query)
	if len(results) == 0 {
		return ToolResult{Content: "No deferred tools matching query: " + params.Query}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d deferred tool(s):\n\n", len(results)))
	for _, td := range results {
		sb.WriteString(fmt.Sprintf("**%s**: %s\n", td.Name, td.Description))
		sb.WriteString(fmt.Sprintf("Schema: %s\n\n", string(td.InputSchema)))
	}

	return ToolResult{Content: sb.String()}, nil
}
