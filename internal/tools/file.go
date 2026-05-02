package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LSPNotifier provides post-write diagnostic feedback from a language server.
// Implementations should notify the server of file changes and collect any
// resulting diagnostics (errors/warnings) to surface to the LLM.
type LSPNotifier interface {
	NotifyAndCollectDiagnostics(ctx context.Context, filePath string, content []byte) ([]string, error)
}

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
	rootDir     string
	diffTracker *DiffTracker
	lspNotifier LSPNotifier
	readHashes  map[string]uint64 // path -> content hash from last read
	readHashMu  sync.Mutex
	cache       *FileReadCache
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
	return &FileTool{rootDir: resolved, readHashes: make(map[string]uint64)}
}

// SetDiffTracker attaches a DiffTracker to record file changes.
func (f *FileTool) SetDiffTracker(dt *DiffTracker) {
	f.diffTracker = dt
}

// SetLSPNotifier attaches an LSPNotifier that will be called after file writes
// to collect diagnostics from the language server.
func (f *FileTool) SetLSPNotifier(n LSPNotifier) {
	f.lspNotifier = n
}

// SetCache attaches a FileReadCache to avoid redundant I/O.
func (f *FileTool) SetCache(c *FileReadCache) {
	f.cache = c
}

func (f *FileTool) Name() string {
	return "file"
}

func (f *FileTool) SearchHint() string {
	return "create modify content text configuration template"
}

func (f *FileTool) Description() string {
	return "Read, write, or patch files. This is the preferred tool for all file operations — use it instead of shell commands like cat, head, tail, sed, or echo.\n" +
		"Supports operations: read (view file contents), write (create or overwrite a file), patch (apply targeted edits without rewriting the full file).\n" +
		"Always read a file before patching it to understand the existing content.\n" +
		"Examples:\n" +
		"  Read:  {\"operation\": \"read\", \"path\": \"src/main.ts\"}\n" +
		"  Write: {\"operation\": \"write\", \"path\": \"src/App.tsx\", \"content\": \"...\"}\n" +
		"  Patch: {\"operation\": \"patch\", \"path\": \"package.json\", \"old_string\": \"old\", \"new_string\": \"new\"}"
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

// ExecuteStream implements StreamingTool. It emits Begin/End events
// around file operations. For read operations on large files, it
// emits the content as Delta events.
func (f *FileTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit ToolEventEmitter) (ToolResult, error) {
	var in fileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	fullPath, err := f.resolvePath(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	emit(ToolEvent{Stage: EventBegin, Content: fmt.Sprintf("%s %s\n", in.Operation, in.Path)})

	var result ToolResult
	switch in.Operation {
	case "read":
		result, err = f.readFile(fullPath)
		if err == nil && !result.IsError && len(result.Content) > 4096 {
			emit(ToolEvent{Stage: EventDelta, Content: result.Content})
		}
	case "write":
		result, err = f.writeFile(ctx, fullPath, in.Content)
	case "patch":
		result, err = f.patchFile(ctx, fullPath, in.OldString, in.NewString)
	default:
		result = ToolResult{
			Content: fmt.Sprintf("unknown operation: %s", in.Operation),
			IsError: true,
		}
	}

	emit(ToolEvent{Stage: EventEnd, Content: "", IsError: result.IsError})
	return result, err
}

func (f *FileTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
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
		return f.writeFile(ctx, fullPath, in.Content)
	case "patch":
		return f.patchFile(ctx, fullPath, in.OldString, in.NewString)
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
	// Check cache first.
	if f.cache != nil {
		if content, hit := f.cache.Get(path); hit {
			return ToolResult{Content: content}, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	// Update cache with fresh content.
	if f.cache != nil {
		if info, err := os.Stat(path); err == nil {
			f.cache.Put(path, info, string(data))
		}
	}

	hash := hashContent(data)
	f.readHashMu.Lock()
	prev, seen := f.readHashes[path]
	f.readHashes[path] = hash
	// Cap map size to prevent unbounded growth in long sessions.
	if len(f.readHashes) > 500 {
		for k := range f.readHashes {
			if k != path {
				delete(f.readHashes, k)
				break
			}
		}
	}
	f.readHashMu.Unlock()

	if seen && prev == hash {
		relPath, _ := filepath.Rel(f.rootDir, path)
		lineCount := strings.Count(string(data), "\n") + 1
		summary := fmt.Sprintf("File %s unchanged since last read (%d lines). Content already in conversation context.", relPath, lineCount)
		return ToolResult{Content: summary}, nil
	}

	return ToolResult{Content: string(data)}, nil
}

// hashContent returns a FNV-1a hash of the given data.
func hashContent(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

func (f *FileTool) writeFile(ctx context.Context, path, content string) (ToolResult, error) {
	// Invalidate read cache for this path since content is changing.
	f.readHashMu.Lock()
	delete(f.readHashes, path)
	f.readHashMu.Unlock()
	if f.cache != nil {
		f.cache.Invalidate(path)
	}

	// Check if the file already exists to determine create vs modify.
	_, statErr := os.Stat(path)
	existed := statErr == nil

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	relPath, _ := filepath.Rel(f.rootDir, path)

	if f.diffTracker != nil {
		op := OpCreated
		if existed {
			op = OpModified
		}
		f.diffTracker.Record(FileChange{
			Path:      relPath,
			Operation: op,
			Tool:      "file",
		})
	}

	result := ToolResult{Content: fmt.Sprintf("wrote %s", relPath)}

	// Collect LSP diagnostics after write.
	if f.lspNotifier != nil {
		diags, err := f.lspNotifier.NotifyAndCollectDiagnostics(ctx, path, []byte(content))
		if err == nil && len(diags) > 0 {
			result.Content += "\n\n\u26a0\ufe0f LSP diagnostics:\n" + strings.Join(diags, "\n")
		}
	}

	return result, nil
}

func (f *FileTool) patchFile(ctx context.Context, path, oldString, newString string) (ToolResult, error) {
	if oldString == "" {
		return ToolResult{Content: "old_string must not be empty", IsError: true}, nil
	}

	// Invalidate read cache for this path since content is changing.
	f.readHashMu.Lock()
	delete(f.readHashes, path)
	f.readHashMu.Unlock()
	if f.cache != nil {
		f.cache.Invalidate(path)
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

	if f.diffTracker != nil {
		f.diffTracker.Record(FileChange{
			Path:      relPath,
			Operation: OpModified,
			Diff:      fmt.Sprintf("-%s\n+%s", oldString, newString),
			Tool:      "file",
		})
	}

	result := ToolResult{Content: fmt.Sprintf("patched %s", relPath)}

	// Collect LSP diagnostics after patch.
	if f.lspNotifier != nil {
		diags, err := f.lspNotifier.NotifyAndCollectDiagnostics(ctx, path, []byte(patched))
		if err == nil && len(diags) > 0 {
			result.Content += "\n\n\u26a0\ufe0f LSP diagnostics:\n" + strings.Join(diags, "\n")
		}
	}

	return result, nil
}
