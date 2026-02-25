package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

type codesignMode string

const (
	codesignInfo   codesignMode = "info"
	codesignVerify codesignMode = "verify"
)

type codesignInput struct {
	Path string `json:"path,omitempty"` // required for verify
}

// CodesignTool wraps macOS code signing utilities for introspection.
type CodesignTool struct {
	rootDir  string
	platform PlatformChecker
	mode     codesignMode
}

// NewCodesignInfoTool creates a tool that lists signing identities.
func NewCodesignInfoTool(pc PlatformChecker) *CodesignTool {
	return &CodesignTool{platform: pc, mode: codesignInfo}
}

// NewCodesignVerifyTool creates a tool that verifies code signatures.
func NewCodesignVerifyTool(rootDir string, pc PlatformChecker) *CodesignTool {
	return &CodesignTool{rootDir: rootDir, platform: pc, mode: codesignVerify}
}

// Name returns the tool name based on the mode.
func (c *CodesignTool) Name() string {
	return "codesign_" + string(c.mode)
}

// Description returns a human-readable description of the tool.
func (c *CodesignTool) Description() string {
	switch c.mode {
	case codesignInfo:
		return "List available code signing identities and provisioning profiles on macOS."
	case codesignVerify:
		return "Verify the code signature of an app bundle or binary."
	default:
		return "Code signing operation."
	}
}

// InputSchema returns the JSON Schema for the tool input.
func (c *CodesignTool) InputSchema() json.RawMessage {
	switch c.mode {
	case codesignInfo:
		return json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`)
	case codesignVerify:
		return json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to the .app bundle or binary to verify"}
			},
			"required": ["path"]
		}`)
	default:
		return json.RawMessage(`{"type": "object", "properties": {}}`)
	}
}

// Execute runs the code signing command based on the configured mode.
func (c *CodesignTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	if !c.platform.IsDarwin() {
		return tools.ToolResult{Content: "codesign requires macOS", IsError: true}, nil
	}

	var in codesignInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	switch c.mode {
	case codesignInfo:
		return c.executeInfo(ctx)
	case codesignVerify:
		if in.Path == "" {
			return tools.ToolResult{Content: "path is required", IsError: true}, nil
		}
		if c.rootDir != "" {
			if _, err := validatePath(c.rootDir, in.Path); err != nil {
				return tools.ToolResult{Content: err.Error(), IsError: true}, nil
			}
		}
		return c.executeVerify(ctx, in.Path)
	default:
		return tools.ToolResult{Content: "unknown codesign mode", IsError: true}, nil
	}
}

func (c *CodesignTool) executeInfo(ctx context.Context) (tools.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "security", "find-identity", "-v", "-p", "codesigning")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		if output != "" {
			return tools.ToolResult{Content: output, IsError: true}, nil
		}
		return tools.ToolResult{Content: fmt.Sprintf("security find-identity failed: %s", err), IsError: true}, nil
	}

	if output == "" {
		return tools.ToolResult{Content: "No signing identities found."}, nil
	}
	return tools.ToolResult{Content: output}, nil
}

func (c *CodesignTool) executeVerify(ctx context.Context, path string) (tools.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", "-v", path)
	// codesign --verify writes to stderr, so we use CombinedOutput.
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		if output != "" {
			return tools.ToolResult{Content: output, IsError: true}, nil
		}
		return tools.ToolResult{Content: fmt.Sprintf("codesign verify failed: %s", err), IsError: true}, nil
	}

	if output == "" {
		return tools.ToolResult{Content: "Signature verified successfully."}, nil
	}
	return tools.ToolResult{Content: output}, nil
}
