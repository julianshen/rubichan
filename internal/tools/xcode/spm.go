package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

type spmMode string

const (
	spmBuild   spmMode = "build"
	spmTest    spmMode = "test"
	spmResolve spmMode = "resolve"
	spmAddDep  spmMode = "add_dep"
)

type spmInput struct {
	PackagePath   string `json:"package_path,omitempty"`
	Configuration string `json:"configuration,omitempty"`
	URL           string `json:"url,omitempty"`
	FromVersion   string `json:"from_version,omitempty"`
}

// SPMTool wraps the swift CLI for Swift Package Manager operations.
// Unlike xcodebuild/simctl tools, SPM tools are cross-platform and do not
// require macOS — they work wherever swift is installed (macOS and Linux).
type SPMTool struct {
	rootDir string
	mode    spmMode
}

// NewSwiftBuildTool creates a tool that runs swift build.
func NewSwiftBuildTool(rootDir string) *SPMTool {
	return &SPMTool{rootDir: rootDir, mode: spmBuild}
}

// NewSwiftTestTool creates a tool that runs swift test.
func NewSwiftTestTool(rootDir string) *SPMTool {
	return &SPMTool{rootDir: rootDir, mode: spmTest}
}

// NewSwiftResolveTool creates a tool that runs swift package resolve.
func NewSwiftResolveTool(rootDir string) *SPMTool {
	return &SPMTool{rootDir: rootDir, mode: spmResolve}
}

// NewSwiftAddDepTool creates a tool that runs swift package add-dependency.
func NewSwiftAddDepTool(rootDir string) *SPMTool {
	return &SPMTool{rootDir: rootDir, mode: spmAddDep}
}

// Name returns the tool name based on the mode.
func (s *SPMTool) Name() string {
	switch s.mode {
	case spmBuild:
		return "swift_build"
	case spmTest:
		return "swift_test"
	case spmResolve:
		return "swift_resolve"
	case spmAddDep:
		return "swift_add_dep"
	default:
		return "swift_" + string(s.mode)
	}
}

// Description returns a human-readable description of the tool.
func (s *SPMTool) Description() string {
	switch s.mode {
	case spmBuild:
		return "Build a Swift package using Swift Package Manager."
	case spmTest:
		return "Run tests for a Swift package using Swift Package Manager."
	case spmResolve:
		return "Resolve Swift package dependencies."
	case spmAddDep:
		return "Add a dependency to a Swift package."
	default:
		return "Swift Package Manager operation."
	}
}

// InputSchema returns the JSON Schema for the tool input.
func (s *SPMTool) InputSchema() json.RawMessage {
	switch s.mode {
	case spmBuild, spmTest:
		return json.RawMessage(`{
		"type": "object",
		"properties": {
			"package_path":  {"type": "string", "description": "Path to the directory containing Package.swift"},
			"configuration": {"type": "string", "description": "Build configuration (debug/release)"}
		}
	}`)
	case spmResolve:
		return json.RawMessage(`{
		"type": "object",
		"properties": {
			"package_path": {"type": "string", "description": "Path to the directory containing Package.swift"}
		}
	}`)
	case spmAddDep:
		return json.RawMessage(`{
		"type": "object",
		"properties": {
			"package_path":  {"type": "string", "description": "Path to the directory containing Package.swift"},
			"url":           {"type": "string", "description": "URL of the dependency repository"},
			"from_version":  {"type": "string", "description": "Minimum version requirement (e.g. 1.0.0)"}
		},
		"required": ["url", "from_version"]
	}`)
	default:
		return json.RawMessage(`{"type": "object", "properties": {}}`)
	}
}

// Execute runs the swift CLI with the configured mode and input parameters.
func (s *SPMTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in spmInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if err := s.validate(in); err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	if in.PackagePath != "" {
		if _, err := validatePath(s.rootDir, in.PackagePath); err != nil {
			return tools.ToolResult{Content: err.Error(), IsError: true}, nil
		}
	}

	args := s.buildArgs(in)
	cmd := exec.CommandContext(ctx, "swift", args...)
	cmd.Dir = s.rootDir

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		if output != "" {
			return tools.ToolResult{Content: output, IsError: true}, nil
		}
		return tools.ToolResult{Content: fmt.Sprintf("swift %s failed: %s", s.mode, err), IsError: true}, nil
	}

	if output == "" {
		return tools.ToolResult{Content: fmt.Sprintf("swift %s succeeded", s.mode)}, nil
	}
	return tools.ToolResult{Content: output}, nil
}

func (s *SPMTool) validate(in spmInput) error {
	if s.mode == spmAddDep {
		if in.URL == "" {
			return fmt.Errorf("url is required")
		}
		if in.FromVersion == "" {
			return fmt.Errorf("from_version is required")
		}
	}
	return nil
}

func (s *SPMTool) buildArgs(in spmInput) []string {
	var args []string

	switch s.mode {
	case spmBuild:
		args = append(args, "build")
	case spmTest:
		args = append(args, "test")
	case spmResolve:
		args = append(args, "package", "resolve")
	case spmAddDep:
		args = append(args, "package", "add-dependency", in.URL, "--from", in.FromVersion)
	}

	if in.PackagePath != "" {
		args = append(args, "--package-path", in.PackagePath)
	}

	if (s.mode == spmBuild || s.mode == spmTest) && in.Configuration != "" {
		args = append(args, "-c", in.Configuration)
	}

	return args
}
