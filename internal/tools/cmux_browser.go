package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/cmux"
)

// CmuxBrowserNavigateTool navigates a browser surface to a URL.
// If no surface_id is provided it auto-creates a right split first.
type CmuxBrowserNavigateTool struct {
	client cmux.Caller
}

// NewCmuxBrowserNavigate creates a CmuxBrowserNavigateTool backed by client.
func NewCmuxBrowserNavigate(client cmux.Caller) *CmuxBrowserNavigateTool {
	return &CmuxBrowserNavigateTool{client: client}
}

func (t *CmuxBrowserNavigateTool) Name() string { return "cmux_browser_navigate" }

func (t *CmuxBrowserNavigateTool) Description() string {
	return "Navigate a browser surface to a URL. If no surface_id is given, " +
		"auto-creates a right split, then navigates and waits for load."
}

func (t *CmuxBrowserNavigateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "The URL to navigate to"
			},
			"surface_id": {
				"type": "string",
				"description": "Target surface ID. If omitted, a new right split is created."
			}
		},
		"required": ["url"]
	}`)
}

type browserNavigateInput struct {
	URL       string `json:"url"`
	SurfaceID string `json:"surface_id"`
}

func (t *CmuxBrowserNavigateTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in browserNavigateInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.URL == "" {
		return ToolResult{Content: "url is required", IsError: true}, nil
	}

	surfaceID := in.SurfaceID
	if surfaceID == "" {
		resp, err := t.client.Call("surface.split", map[string]string{"direction": "right"})
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
		surfaceID = surf.ID
	}

	resp, err := t.client.Call("browser.navigate", map[string]string{
		"surface_id": surfaceID,
		"url":        in.URL,
	})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("browser.navigate failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("browser.navigate error: %s", resp.Error), IsError: true}, nil
	}

	resp, err = t.client.Call("browser.wait", map[string]string{
		"surface_id": surfaceID,
		"load_state": "load",
	})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("browser.wait failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("browser.wait error: %s", resp.Error), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Navigated to %s in surface %s", in.URL, surfaceID)}, nil
}

// CmuxBrowserSnapshotTool captures the DOM snapshot of a browser surface.
type CmuxBrowserSnapshotTool struct {
	client cmux.Caller
}

// NewCmuxBrowserSnapshot creates a CmuxBrowserSnapshotTool backed by client.
func NewCmuxBrowserSnapshot(client cmux.Caller) *CmuxBrowserSnapshotTool {
	return &CmuxBrowserSnapshotTool{client: client}
}

func (t *CmuxBrowserSnapshotTool) Name() string { return "cmux_browser_snapshot" }

func (t *CmuxBrowserSnapshotTool) Description() string {
	return "Capture the DOM snapshot of a browser surface."
}

func (t *CmuxBrowserSnapshotTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {
				"type": "string",
				"description": "The surface ID to snapshot"
			}
		},
		"required": ["surface_id"]
	}`)
}

type browserSurfaceInput struct {
	SurfaceID string `json:"surface_id"`
}

func (t *CmuxBrowserSnapshotTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in browserSurfaceInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.SurfaceID == "" {
		return ToolResult{Content: "surface_id is required", IsError: true}, nil
	}

	resp, err := t.client.Call("browser.snapshot", map[string]string{"surface_id": in.SurfaceID})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("browser.snapshot failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("browser.snapshot error: %s", resp.Error), IsError: true}, nil
	}

	var result struct {
		DOM string `json:"dom"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return ToolResult{Content: fmt.Sprintf("decode snapshot: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: result.DOM}, nil
}

// CmuxBrowserClickTool clicks an element in a browser surface.
type CmuxBrowserClickTool struct {
	client cmux.Caller
}

// NewCmuxBrowserClick creates a CmuxBrowserClickTool backed by client.
func NewCmuxBrowserClick(client cmux.Caller) *CmuxBrowserClickTool {
	return &CmuxBrowserClickTool{client: client}
}

func (t *CmuxBrowserClickTool) Name() string { return "cmux_browser_click" }

func (t *CmuxBrowserClickTool) Description() string {
	return "Click an element (identified by ref) in a browser surface."
}

func (t *CmuxBrowserClickTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {
				"type": "string",
				"description": "The surface ID"
			},
			"ref": {
				"type": "string",
				"description": "Element reference to click"
			}
		},
		"required": ["surface_id", "ref"]
	}`)
}

type browserClickInput struct {
	SurfaceID string `json:"surface_id"`
	Ref       string `json:"ref"`
}

func (t *CmuxBrowserClickTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in browserClickInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.SurfaceID == "" {
		return ToolResult{Content: "surface_id is required", IsError: true}, nil
	}
	if in.Ref == "" {
		return ToolResult{Content: "ref is required", IsError: true}, nil
	}

	resp, err := t.client.Call("browser.click", map[string]string{
		"surface_id": in.SurfaceID,
		"ref":        in.Ref,
	})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("browser.click failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("browser.click error: %s", resp.Error), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Clicked %s in surface %s", in.Ref, in.SurfaceID)}, nil
}

// CmuxBrowserTypeTool types text into an element in a browser surface.
type CmuxBrowserTypeTool struct {
	client cmux.Caller
}

// NewCmuxBrowserType creates a CmuxBrowserTypeTool backed by client.
func NewCmuxBrowserType(client cmux.Caller) *CmuxBrowserTypeTool {
	return &CmuxBrowserTypeTool{client: client}
}

func (t *CmuxBrowserTypeTool) Name() string { return "cmux_browser_type" }

func (t *CmuxBrowserTypeTool) Description() string {
	return "Type text into an element (identified by ref) in a browser surface."
}

func (t *CmuxBrowserTypeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {
				"type": "string",
				"description": "The surface ID"
			},
			"ref": {
				"type": "string",
				"description": "Element reference to type into"
			},
			"text": {
				"type": "string",
				"description": "Text to type"
			}
		},
		"required": ["surface_id", "ref", "text"]
	}`)
}

type browserTypeInput struct {
	SurfaceID string `json:"surface_id"`
	Ref       string `json:"ref"`
	Text      string `json:"text"`
}

func (t *CmuxBrowserTypeTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in browserTypeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.SurfaceID == "" {
		return ToolResult{Content: "surface_id is required", IsError: true}, nil
	}
	if in.Ref == "" {
		return ToolResult{Content: "ref is required", IsError: true}, nil
	}

	resp, err := t.client.Call("browser.type", map[string]string{
		"surface_id": in.SurfaceID,
		"ref":        in.Ref,
		"text":       in.Text,
	})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("browser.type failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("browser.type error: %s", resp.Error), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Typed %q into %s in surface %s", in.Text, in.Ref, in.SurfaceID)}, nil
}

// CmuxBrowserWaitTool waits for a browser surface to reach a load state.
type CmuxBrowserWaitTool struct {
	client cmux.Caller
}

// NewCmuxBrowserWait creates a CmuxBrowserWaitTool backed by client.
func NewCmuxBrowserWait(client cmux.Caller) *CmuxBrowserWaitTool {
	return &CmuxBrowserWaitTool{client: client}
}

func (t *CmuxBrowserWaitTool) Name() string { return "cmux_browser_wait" }

func (t *CmuxBrowserWaitTool) Description() string {
	return "Wait for a browser surface to reach the given load state."
}

func (t *CmuxBrowserWaitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"surface_id": {
				"type": "string",
				"description": "The surface ID"
			},
			"load_state": {
				"type": "string",
				"description": "Load state to wait for (e.g. load, domcontentloaded, networkidle)"
			}
		},
		"required": ["surface_id", "load_state"]
	}`)
}

type browserWaitInput struct {
	SurfaceID string `json:"surface_id"`
	LoadState string `json:"load_state"`
}

func (t *CmuxBrowserWaitTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in browserWaitInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.SurfaceID == "" {
		return ToolResult{Content: "surface_id is required", IsError: true}, nil
	}
	if in.LoadState == "" {
		return ToolResult{Content: "load_state is required", IsError: true}, nil
	}

	resp, err := t.client.Call("browser.wait", map[string]string{
		"surface_id": in.SurfaceID,
		"load_state": in.LoadState,
	})
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("browser.wait failed: %s", err), IsError: true}, nil
	}
	if !resp.OK {
		return ToolResult{Content: fmt.Sprintf("browser.wait error: %s", resp.Error), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("Surface %s reached load state %q", in.SurfaceID, in.LoadState)}, nil
}
