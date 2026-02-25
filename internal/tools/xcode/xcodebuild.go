package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

type xcodebuildInput struct {
	Project       string `json:"project,omitempty"`
	Workspace     string `json:"workspace,omitempty"`
	Scheme        string `json:"scheme"`
	Destination   string `json:"destination,omitempty"`
	Configuration string `json:"configuration,omitempty"`
	ArchivePath   string `json:"archive_path,omitempty"`
}

type xcodebuildMode string

const (
	modeBuild   xcodebuildMode = "build"
	modeTest    xcodebuildMode = "test"
	modeArchive xcodebuildMode = "archive"
	modeClean   xcodebuildMode = "clean"
)

// XcodeBuildTool wraps xcodebuild for a specific operation mode.
type XcodeBuildTool struct {
	rootDir  string
	platform PlatformChecker
	mode     xcodebuildMode
}

// NewXcodeBuildTool creates a tool that runs xcodebuild build.
func NewXcodeBuildTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeBuild}
}

// NewXcodeTestTool creates a tool that runs xcodebuild test.
func NewXcodeTestTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeTest}
}

// NewXcodeArchiveTool creates a tool that runs xcodebuild archive.
func NewXcodeArchiveTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeArchive}
}

// NewXcodeCleanTool creates a tool that runs xcodebuild clean.
func NewXcodeCleanTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeClean}
}

// Name returns the tool name based on the mode.
func (x *XcodeBuildTool) Name() string {
	return "xcode_" + string(x.mode)
}

// Description returns a human-readable description of the tool.
func (x *XcodeBuildTool) Description() string {
	switch x.mode {
	case modeBuild:
		return "Build an Xcode project or workspace. Parses output for errors and warnings."
	case modeTest:
		return "Run tests for an Xcode project. Returns structured test results."
	case modeArchive:
		return "Create an archive for distribution. Requires code signing."
	case modeClean:
		return "Clean build artifacts for a scheme."
	default:
		return "Xcode build operation."
	}
}

// InputSchema returns the JSON Schema for the tool input.
func (x *XcodeBuildTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"project":       {"type": "string", "description": ".xcodeproj path (optional if workspace set)"},
			"workspace":     {"type": "string", "description": ".xcworkspace path (optional if project set)"},
			"scheme":        {"type": "string", "description": "Build scheme name"},
			"destination":   {"type": "string", "description": "Build destination (e.g. platform=iOS Simulator,name=iPhone 15)"},
			"configuration": {"type": "string", "description": "Build configuration (Debug/Release)"},
			"archive_path":  {"type": "string", "description": "Archive output path (archive mode only)"}
		},
		"required": ["scheme"]
	}`)
}

// Execute runs xcodebuild with the configured mode and input parameters.
func (x *XcodeBuildTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	if !x.platform.IsDarwin() {
		return tools.ToolResult{Content: "xcodebuild requires macOS with Xcode installed", IsError: true}, nil
	}

	var in xcodebuildInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Scheme == "" {
		return tools.ToolResult{Content: "scheme is required", IsError: true}, nil
	}

	// Validate path inputs stay within project directory.
	for _, p := range []string{in.Workspace, in.Project, in.ArchivePath} {
		if p != "" {
			if _, err := validatePath(x.rootDir, p); err != nil {
				return tools.ToolResult{Content: err.Error(), IsError: true}, nil
			}
		}
	}

	args := x.buildArgs(in)
	cmd := exec.CommandContext(ctx, "xcodebuild", args...)
	cmd.Dir = x.rootDir

	out, err := cmd.CombinedOutput()
	output := string(out)

	if x.mode == modeTest {
		testResult := ParseTestLog(output)
		buildResult := ParseBuildLog(output)
		var b strings.Builder
		b.WriteString(FormatTestResult(testResult))
		if len(buildResult.Errors) > 0 {
			b.WriteString("\n")
			b.WriteString(FormatBuildResult(buildResult))
		}
		isErr := !testResult.AllPassed() || (err != nil && !buildResult.Success)
		return tools.ToolResult{Content: b.String(), IsError: isErr}, nil
	}

	buildResult := ParseBuildLog(output)
	if err != nil && !buildResult.Success {
		return tools.ToolResult{Content: FormatBuildResult(buildResult), IsError: true}, nil
	}
	return tools.ToolResult{Content: FormatBuildResult(buildResult)}, nil
}

func (x *XcodeBuildTool) buildArgs(in xcodebuildInput) []string {
	var args []string

	if in.Workspace != "" {
		args = append(args, "-workspace", in.Workspace)
	} else if in.Project != "" {
		args = append(args, "-project", in.Project)
	}

	args = append(args, "-scheme", in.Scheme)

	if in.Destination != "" {
		args = append(args, "-destination", in.Destination)
	}
	if in.Configuration != "" {
		args = append(args, "-configuration", in.Configuration)
	}

	switch x.mode {
	case modeBuild:
		args = append(args, "build")
	case modeTest:
		args = append(args, "test")
	case modeArchive:
		args = append(args, "archive")
		if in.ArchivePath != "" {
			args = append(args, "-archivePath", in.ArchivePath)
		}
	case modeClean:
		args = append(args, "clean")
	}

	args = append(args, "-quiet")
	return args
}
