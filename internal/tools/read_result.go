package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ResultRetriever retrieves stored tool results by reference ID.
// Defined here to break the import cycle between tools/ and agent/.
type ResultRetriever interface {
	Retrieve(refID string) (string, error)
}

// ReadResultTool allows the LLM to retrieve offloaded tool results.
type ReadResultTool struct {
	retriever ResultRetriever
}

// NewReadResultTool creates a new ReadResultTool with the given retriever.
func NewReadResultTool(r ResultRetriever) *ReadResultTool {
	return &ReadResultTool{retriever: r}
}

func (t *ReadResultTool) Name() string { return "read_result" }

func (t *ReadResultTool) Description() string {
	return "Read a previously stored tool result by reference ID. Supports offset and limit for pagination."
}

func (t *ReadResultTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ref_id": {"type": "string", "description": "Reference ID of the stored result"},
			"offset": {"type": "integer", "description": "Byte offset to start reading from (default 0)"},
			"limit":  {"type": "integer", "description": "Maximum bytes to return (default 4096)"}
		},
		"required": ["ref_id"]
	}`)
}

type readResultInput struct {
	RefID  string `json:"ref_id"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (t *ReadResultTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	if t.retriever == nil {
		return ToolResult{Content: "read_result tool not initialized", IsError: true}, nil
	}

	var params readResultInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if params.Limit <= 0 {
		params.Limit = 4096
	}

	content, err := t.retriever.Retrieve(params.RefID)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("retrieval error: %s", err), IsError: true}, nil
	}

	// Apply offset and limit.
	if params.Offset > len(content) {
		return ToolResult{Content: ""}, nil
	}
	content = content[params.Offset:]
	if len(content) > params.Limit {
		content = content[:params.Limit]
	}

	return ToolResult{Content: content}, nil
}
