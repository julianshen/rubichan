package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fileInput represents the input for the file tool.
type fileInput struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	OldString string `json:"old_string,omitempty"`
	NewString string `json:"new_string,omitempty"`
}

// FileTool provides file read, write, and patch operations.
type FileTool struct {
	rootDir string
}

// NewFileTool creates a new FileTool that operates within the given root directory.
// The root is resolved through EvalSymlinks so that symlink checks inside
// resolvePath compare against the canonical path.
func NewFileTool(rootDir string) *FileTool {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		abs = rootDir
	}
	// Resolve symlinks in the root itself (e.g. /var → /private/var on macOS)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}
	return &FileTool{rootDir: resolved}
}

func (f *FileTool) Name() string {
	return "file"
}

func (f *FileTool) Description() string {
	return "Read, write, or patch files. Supports operations: read, write, patch."
}

func (f *FileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["read", "write", "patch"],
				"description": "The file operation to perform"
			},
			"path": {
				"type": "string",
				"description": "Relative path to the file"
			},
			"content": {
				"type": "string",
				"description": "Content to write (for write operation)"
			},
			"old_string": {
				"type": "string",
				"description": "String to find (for patch operation)"
			},
			"new_string": {
				"type": "string",
				"description": "Replacement string (for patch operation)"
			}
		},
		"required": ["operation", "path"]
	}`)
}

func (f *FileTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in fileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	fullPath, err := f.resolvePath(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	switch in.Operation {
	case "read":
		return f.readFile(fullPath)
	case "write":
		return f.writeFile(fullPath, in.Content)
	case "patch":
		return f.patchFile(fullPath, in.OldString, in.NewString)
	default:
		return ToolResult{
			Content: fmt.Sprintf("unknown operation: %s", in.Operation),
			IsError: true,
		}, nil
	}
}

// resolvePath resolves and validates the path to prevent both path traversal
// and symlink traversal attacks.
func (f *FileTool) resolvePath(relPath string) (string, error) {
	// Reject absolute paths outright
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("path traversal denied: %s escapes root directory", relPath)
	}

	// Join with root and get absolute path
	joined := filepath.Join(f.rootDir, relPath)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("path traversal denied: %s", err)
	}

	// Lexical check first (catches ../.. before the file exists)
	if !strings.HasPrefix(abs, f.rootDir+string(filepath.Separator)) && abs != f.rootDir {
		return "", fmt.Errorf("path traversal denied: %s escapes root directory", relPath)
	}

	// Resolve symlinks to get the real path on disk. If the file doesn't
	// exist yet (write/create), walk up to the nearest existing ancestor.
	evalPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			ancestorEval, ancestorErr := resolveNearestAncestor(abs)
			if ancestorErr != nil {
				return "", fmt.Errorf("path traversal denied: %s", ancestorErr)
			}
			if !strings.HasPrefix(ancestorEval, f.rootDir+string(filepath.Separator)) && ancestorEval != f.rootDir {
				return "", fmt.Errorf("path traversal denied: %s escapes root directory via symlink", relPath)
			}
			return abs, nil
		}
		return "", fmt.Errorf("path traversal denied: %s", err)
	}

	// Verify the real (symlink-resolved) path is still within rootDir
	if !strings.HasPrefix(evalPath, f.rootDir+string(filepath.Separator)) && evalPath != f.rootDir {
		return "", fmt.Errorf("path traversal denied: %s escapes root directory via symlink", relPath)
	}

	return evalPath, nil
}

// resolveNearestAncestor walks up from path until it finds an existing
// directory, then resolves symlinks on that ancestor.
func resolveNearestAncestor(path string) (string, error) {
	dir := filepath.Dir(path)
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no existing ancestor found for %s", path)
		}
		dir = parent
	}
}

func (f *FileTool) readFile(path string) (ToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return ToolResult{Content: string(data)}, nil
}

func (f *FileTool) writeFile(path, content string) (ToolResult, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	// Return the relative path for a cleaner message
	relPath, _ := filepath.Rel(f.rootDir, path)
	return ToolResult{Content: fmt.Sprintf("wrote %s", relPath)}, nil
}

func (f *FileTool) patchFile(path, oldString, newString string) (ToolResult, error) {
	if oldString == "" {
		return ToolResult{Content: "old_string must not be empty", IsError: true}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	content := string(data)
	if !strings.Contains(content, oldString) {
		return ToolResult{
			Content: "old_string not found in file",
			IsError: true,
		}, nil
	}

	// Replace only the first occurrence
	patched := strings.Replace(content, oldString, newString, 1)
	if err := os.WriteFile(path, []byte(patched), 0644); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	relPath, _ := filepath.Rel(f.rootDir, path)
	return ToolResult{Content: fmt.Sprintf("patched %s", relPath)}, nil
}
