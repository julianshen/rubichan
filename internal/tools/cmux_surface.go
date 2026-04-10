package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/cmux"
)

// CmuxSplitTool creates a new surface by splitting in the given direction.
type CmuxSplitTool struct {
	client cmux.Caller
}

// NewCmuxSplit creates a CmuxSplitTool backed by client.
func NewCmuxSplit(client cmux.Caller) *CmuxSplitTool {
	return &CmuxSplitTool{client: client}
}

func (t *CmuxSplitTool) Name() string { return "cmux_split" }

func (t *CmuxSplitTool) Description() string {
	return "Create a new surface by splitting the current pane in the given direction."
}

func (t *CmuxSplitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"direction": {
				"type": "string",
				"enum": ["left", "right", "up", "down"],
				"description": "Direction to split"
			}
		},
		"required": ["direction"]
	}`)
}

type splitInput struct {
	Direction string `json:"direction"`
}

func (t *CmuxSplitTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in splitInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.Direction == "" {
		return ToolResult{Content: "direction is required", IsError: true}, nil
	}

	resp, err := t.client.Call("surface.split", map[string]string{"direction": in.Direction})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("surface.split failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("surface.split error: %s", resp.Error), IsError: true}, nil
	}

	var surf cmux.Surface
	if err := json.Unmarshal(resp.Result, &surf); err != nil {
		return ToolResult{Content: fmt.Sprintf("decode surface: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Created %s split — surface_id: %s", in.Direction, surf.ID)}, nil
}

// CmuxSendTool sends text or a key event to a surface.
type CmuxSendTool struct {
	client cmux.Caller
}

// NewCmuxSend creates a CmuxSendTool backed by client.
func NewCmuxSend(client cmux.Caller) *CmuxSendTool {
	return &CmuxSendTool{client: client}
}

func (t *CmuxSendTool) Name() string { return "cmux_send" }

func (t *CmuxSendTool) Description() string {
	return "Send text or a key event to a surface. Provide either 'text' or 'key', not both."
}

func (t *CmuxSendTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {
				"type": "string",
				"description": "Target surface ID"
			},
			"text": {
				"type": "string",
				"description": "Text to send (mutually exclusive with key)"
			},
			"key": {
				"type": "string",
				"description": "Key event to send, e.g. 'Enter' (mutually exclusive with text)"
			}
		},
		"required": ["surface_id"]
	}`)
}

type sendInput struct {
	SurfaceID string `json:"surface_id"`
	Text      string `json:"text"`
	Key       string `json:"key"`
}

func (t *CmuxSendTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in sendInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.SurfaceID == "" {
		return ToolResult{Content: "surface_id is required", IsError: true}, nil
	}
	if in.Text == "" && in.Key == "" {
		return ToolResult{Content: "either text or key must be provided", IsError: true}, nil
	}
	if in.Text != "" && in.Key != "" {
		return ToolResult{Content: "text and key are mutually exclusive", IsError: true}, nil
	}

	if in.Text != "" {
		resp, err := t.client.Call("surface.send_text", map[string]string{
			"surface_id": in.SurfaceID,
			"text":       in.Text,
		})
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("surface.send_text failed: %s", err), IsError: true}, nil
		}
		if !resp.OK {
			return ToolResult{Content: fmt.Sprintf("surface.send_text error: %s", resp.Error), IsError: true}, nil
		}
		return ToolResult{Content: fmt.Sprintf("Sent text to surface %s", in.SurfaceID)}, nil
	}

	resp, err := t.client.Call("surface.send_key", map[string]string{
		"surface_id": in.SurfaceID,
		"key":        in.Key,
	})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("surface.send_key failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("surface.send_key error: %s", resp.Error), IsError: true}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Sent key %q to surface %s", in.Key, in.SurfaceID)}, nil
}
