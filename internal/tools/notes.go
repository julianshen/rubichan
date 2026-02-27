package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ScratchpadAccess is the interface for accessing the agent's scratchpad.
// Defined here to break the import cycle between tools/ and agent/.
type ScratchpadAccess interface {
	Set(tag, content string)
	Get(tag string) string
	Delete(tag string)
	All() map[string]string
}

// NotesTool allows the agent to take structured notes that survive compaction.
type NotesTool struct {
	scratchpad ScratchpadAccess
}

// NewNotesTool creates a NotesTool backed by the given scratchpad.
func NewNotesTool(sp ScratchpadAccess) *NotesTool {
	return &NotesTool{scratchpad: sp}
}

func (n *NotesTool) Name() string { return "notes" }

func (n *NotesTool) Description() string {
	return "Take structured notes that persist across context compaction. " +
		"Actions: set (tag + content), get (tag), delete (tag), list."
}

func (n *NotesTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["set", "get", "delete", "list"],
				"description": "The action to perform"
			},
			"tag": {
				"type": "string",
				"description": "The note tag/key (required for set, get, delete)"
			},
			"content": {
				"type": "string",
				"description": "The note content (required for set)"
			}
		},
		"required": ["action"]
	}`)
}

type notesInput struct {
	Action  string `json:"action"`
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

func (n *NotesTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in notesInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	switch in.Action {
	case "set":
		if in.Tag == "" {
			return ToolResult{Content: "tag is required for set action", IsError: true}, nil
		}
		n.scratchpad.Set(in.Tag, in.Content)
		return ToolResult{Content: fmt.Sprintf("Note '%s' saved.", in.Tag)}, nil

	case "get":
		if in.Tag == "" {
			return ToolResult{Content: "tag is required for get action", IsError: true}, nil
		}
		content := n.scratchpad.Get(in.Tag)
		if content == "" {
			return ToolResult{Content: fmt.Sprintf("No note found with tag '%s'.", in.Tag)}, nil
		}
		return ToolResult{Content: content}, nil

	case "delete":
		if in.Tag == "" {
			return ToolResult{Content: "tag is required for delete action", IsError: true}, nil
		}
		n.scratchpad.Delete(in.Tag)
		return ToolResult{Content: fmt.Sprintf("Note '%s' deleted.", in.Tag)}, nil

	case "list":
		all := n.scratchpad.All()
		if len(all) == 0 {
			return ToolResult{Content: "No notes stored."}, nil
		}
		var sb strings.Builder
		for tag, content := range all {
			preview := content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf("- %s: %s\n", tag, preview))
		}
		return ToolResult{Content: sb.String()}, nil

	default:
		return ToolResult{
			Content: fmt.Sprintf("unknown action: %s (use set, get, delete, or list)", in.Action),
			IsError: true,
		}, nil
	}
}
