package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

// ProjectInfo holds discovered Apple project information.
type ProjectInfo struct {
	Type       string   `json:"type"`        // "xcodeproj", "xcworkspace", "spm", "none"
	Name       string   `json:"name"`        // project name
	Path       string   `json:"path"`        // path to project file
	SwiftFiles []string `json:"swift_files"` // .swift files found
}

type discoverInput struct {
	Path string `json:"path,omitempty"`
}

// DiscoverTool detects Apple project types in a directory.
type DiscoverTool struct {
	rootDir string
}

// NewDiscoverTool creates a new DiscoverTool rooted at the given directory.
func NewDiscoverTool(rootDir string) *DiscoverTool {
	return &DiscoverTool{rootDir: rootDir}
}

// Name returns the tool name.
func (d *DiscoverTool) Name() string { return "xcode_discover" }

// Description returns a human-readable description of the tool.
func (d *DiscoverTool) Description() string {
	return "Detect Apple project type (.xcodeproj, .xcworkspace, Package.swift) in a directory."
}

// InputSchema returns the JSON Schema for the tool input.
func (d *DiscoverTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Subdirectory to scan (optional, defaults to project root)"
			}
		}
	}`)
}

// Execute runs the discovery scan and returns the result.
func (d *DiscoverTool) Execute(_ context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in discoverInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	scanDir := d.rootDir
	if in.Path != "" {
		validPath, err := validatePath(d.rootDir, in.Path)
		if err != nil {
			return tools.ToolResult{Content: err.Error(), IsError: true}, nil
		}
		scanDir = validPath
	}

	info := DiscoverProject(scanDir)
	out, _ := json.MarshalIndent(info, "", "  ")
	if info.Type == "none" {
		return tools.ToolResult{Content: fmt.Sprintf("no Apple project found in %s\n%s", scanDir, string(out))}, nil
	}
	return tools.ToolResult{Content: string(out)}, nil
}

// DiscoverProject scans a directory for Apple project files. Exported for
// use in auto-activation checks in main.go.
func DiscoverProject(dir string) ProjectInfo {
	info := ProjectInfo{Type: "none"}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return info
	}

	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".xcworkspace") && e.IsDir():
			info.Type = "xcworkspace"
			info.Name = strings.TrimSuffix(name, ".xcworkspace")
			info.Path = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".xcodeproj") && e.IsDir() && info.Type != "xcworkspace":
			info.Type = "xcodeproj"
			info.Name = strings.TrimSuffix(name, ".xcodeproj")
			info.Path = filepath.Join(dir, name)
		case name == "Package.swift" && info.Type == "none":
			info.Type = "spm"
			info.Name = filepath.Base(dir)
			info.Path = filepath.Join(dir, name)
		}
	}

	// Collect swift files (non-recursive, top-level only).
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".swift") {
			info.SwiftFiles = append(info.SwiftFiles, e.Name())
		}
	}

	return info
}
