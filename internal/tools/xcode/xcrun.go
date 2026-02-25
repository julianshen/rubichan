package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

// allowedXcrunTools is the set of tools that can be dispatched through xcrun.
// Unknown tools are rejected to prevent arbitrary command execution.
var allowedXcrunTools = map[string]bool{
	"instruments":    true,
	"strings":        true,
	"swift-demangle": true,
	"sourcekit-lsp":  true,
	"simctl":         true,
}

type xcrunInput struct {
	Tool string   `json:"tool"`
	Args []string `json:"args,omitempty"`
}

// XcrunTool is a generic wrapper around xcrun that dispatches to allowlisted tools.
type XcrunTool struct {
	platform PlatformChecker
	runner   CommandRunner
}

// NewXcrunTool creates a new XcrunTool with the given platform checker.
func NewXcrunTool(pc PlatformChecker) *XcrunTool {
	return &XcrunTool{platform: pc, runner: ExecRunner{}}
}

// Name returns the tool name.
func (x *XcrunTool) Name() string {
	return "xcrun"
}

// Description returns a human-readable description of the tool.
func (x *XcrunTool) Description() string {
	return "Run an allowlisted Xcode command-line tool via xcrun."
}

// InputSchema returns the JSON Schema for the tool input.
func (x *XcrunTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"tool": {"type": "string", "description": "The xcrun tool name (e.g. instruments, strings, swift-demangle, sourcekit-lsp, simctl)"},
			"args": {"type": "array", "items": {"type": "string"}, "description": "Arguments to pass to the tool"}
		},
		"required": ["tool"]
	}`)
}

// Execute runs xcrun with the specified tool and arguments.
func (x *XcrunTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	if !x.platform.IsDarwin() {
		return tools.ToolResult{Content: "xcrun requires macOS with Xcode installed", IsError: true}, nil
	}

	var in xcrunInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Tool == "" {
		return tools.ToolResult{Content: "tool is required", IsError: true}, nil
	}

	if !allowedXcrunTools[in.Tool] {
		return tools.ToolResult{Content: fmt.Sprintf("tool not allowed: %s", in.Tool), IsError: true}, nil
	}

	args := append([]string{in.Tool}, in.Args...)
	out, err := x.runner.CombinedOutput(ctx, "", "xcrun", args...)
	output := strings.TrimSpace(string(out))

	if err != nil {
		if output != "" {
			return tools.ToolResult{Content: output, IsError: true}, nil
		}
		return tools.ToolResult{Content: fmt.Sprintf("xcrun %s failed: %s", in.Tool, err), IsError: true}, nil
	}

	if output == "" {
		return tools.ToolResult{Content: fmt.Sprintf("xcrun %s succeeded", in.Tool)}, nil
	}
	return tools.ToolResult{Content: output}, nil
}
