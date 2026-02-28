package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// searchInput represents the input for the search tool.
type searchInput struct {
	Pattern      string `json:"pattern"`
	Path         string `json:"path,omitempty"`
	FilePattern  string `json:"file_pattern,omitempty"`
	MaxResults   int    `json:"max_results,omitempty"`
	ContextLines int    `json:"context_lines,omitempty"`
}

// SearchTool provides regex-based code search within a root directory.
// It tries ripgrep (rg) for speed, falling back to Go-native search.
type SearchTool struct {
	rootDir string
}

// NewSearchTool creates a new SearchTool that searches within the given root directory.
func NewSearchTool(rootDir string) *SearchTool {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		abs = rootDir
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}
	return &SearchTool{rootDir: resolved}
}

func (s *SearchTool) Name() string {
	return "search"
}

func (s *SearchTool) Description() string {
	return "Search for patterns in code files using regex. Supports file filtering, context lines, and max results."
}

func (s *SearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Regex pattern to search for"
			},
			"path": {
				"type": "string",
				"description": "Relative subdirectory to search in (optional)"
			},
			"file_pattern": {
				"type": "string",
				"description": "Glob pattern to filter files (e.g. *.go)"
			},
			"max_results": {
				"type": "integer",
				"description": "Maximum number of matching lines to return"
			},
			"context_lines": {
				"type": "integer",
				"description": "Number of context lines around each match"
			}
		},
		"required": ["pattern"]
	}`)
}

func (s *SearchTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var in searchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Pattern == "" {
		return ToolResult{Content: "pattern is required", IsError: true}, nil
	}

	// Validate the regex before attempting search.
	if _, err := regexp.Compile(in.Pattern); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid regex pattern: %s", err), IsError: true}, nil
	}

	// Resolve search directory.
	searchDir := s.rootDir
	if in.Path != "" {
		if filepath.IsAbs(in.Path) {
			return ToolResult{Content: "path traversal denied: absolute paths not allowed", IsError: true}, nil
		}
		candidate := filepath.Join(s.rootDir, in.Path)
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("path traversal denied: %s", err), IsError: true}, nil
		}
		if !strings.HasPrefix(abs, s.rootDir+string(filepath.Separator)) && abs != s.rootDir {
			return ToolResult{Content: "path traversal denied: path escapes root directory", IsError: true}, nil
		}
		// Resolve symlinks to prevent symlink traversal attacks.
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return ToolResult{Content: fmt.Sprintf("path not found: %s", in.Path), IsError: true}, nil
			}
			return ToolResult{Content: fmt.Sprintf("path traversal denied: %s", err), IsError: true}, nil
		}
		if !strings.HasPrefix(resolved, s.rootDir+string(filepath.Separator)) && resolved != s.rootDir {
			return ToolResult{Content: "path traversal denied: path escapes root directory via symlink", IsError: true}, nil
		}
		searchDir = resolved
	}

	if in.MaxResults <= 0 {
		in.MaxResults = 200
	}
	if in.ContextLines < 0 {
		in.ContextLines = 0
	}

	// Try ripgrep first, fall back to Go-native search.
	var result string
	var err error
	if rgPath, lookErr := exec.LookPath("rg"); lookErr == nil {
		result, err = s.searchWithRipgrep(ctx, rgPath, searchDir, in)
	} else {
		result, err = s.searchGoNative(ctx, searchDir, in)
	}

	if err != nil {
		return ToolResult{Content: fmt.Sprintf("search error: %s", err), IsError: true}, nil
	}

	if result == "" {
		return ToolResult{Content: "no matches found"}, nil
	}

	// Truncate for LLM; optionally set richer DisplayContent for user.
	var displayContent string
	if len(result) > maxOutputBytes {
		display := result
		if len(display) > maxDisplayBytes {
			display = display[:maxDisplayBytes] + "\n... output truncated"
		}
		displayContent = display
		result = result[:maxOutputBytes] + "\n... output truncated"
	}

	return ToolResult{Content: result, DisplayContent: displayContent}, nil
}

func (s *SearchTool) searchWithRipgrep(ctx context.Context, rgPath, searchDir string, in searchInput) (string, error) {
	args := []string{
		"--no-heading",
		"--line-number",
		"--color", "never",
	}

	if in.ContextLines > 0 {
		args = append(args, fmt.Sprintf("-C%d", in.ContextLines))
	}
	if in.FilePattern != "" {
		args = append(args, "--glob", in.FilePattern)
	}

	args = append(args, in.Pattern, searchDir)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// ripgrep returns exit code 1 for no matches — that's not an error.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return "", nil
		}
		// Exit code 2 means an actual error. Any other non-zero exit is also an error.
		return "", fmt.Errorf("ripgrep error (exit %d): %s",
			cmd.ProcessState.ExitCode(), strings.TrimSpace(string(out)))
	}

	result := s.relativizePaths(string(out))

	// Enforce global max_results cap (ripgrep's --max-count is per-file).
	if in.MaxResults > 0 {
		result = enforceMaxResults(result, in.MaxResults, in.ContextLines > 0)
	}

	return result, nil
}

// enforceMaxResults truncates ripgrep output to at most maxResults match blocks.
// When context lines are used, matches are separated by "--" lines.
func enforceMaxResults(output string, maxResults int, hasContext bool) string {
	if !hasContext {
		// No context: each line is one match.
		lines := strings.SplitN(output, "\n", maxResults+2)
		if len(lines) <= maxResults+1 {
			return output
		}
		return strings.Join(lines[:maxResults], "\n") + "\n"
	}

	// With context: match blocks are separated by "--" lines.
	var buf strings.Builder
	matchCount := 0
	for _, line := range strings.Split(output, "\n") {
		if line == "--" {
			matchCount++
			if matchCount >= maxResults {
				break
			}
			buf.WriteString(line + "\n")
			continue
		}
		buf.WriteString(line + "\n")
	}
	return buf.String()
}

func (s *SearchTool) searchGoNative(ctx context.Context, searchDir string, in searchInput) (string, error) {
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	matchCount := 0

	err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply file pattern filter.
		if in.FilePattern != "" {
			matched, matchErr := filepath.Match(in.FilePattern, d.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}

		// Skip binary files by checking the first 512 bytes for null bytes.
		if isBinaryFile(path) {
			return nil
		}

		relPath, _ := filepath.Rel(s.rootDir, path)

		f, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		var lines []string
		var matchIndices []int

		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			lines = append(lines, line)
			if re.MatchString(line) {
				matchIndices = append(matchIndices, lineNum-1)
			}
		}

		for _, idx := range matchIndices {
			if matchCount >= in.MaxResults {
				return filepath.SkipAll
			}

			start := idx - in.ContextLines
			if start < 0 {
				start = 0
			}
			end := idx + in.ContextLines + 1
			if end > len(lines) {
				end = len(lines)
			}

			for i := start; i < end; i++ {
				buf.WriteString(fmt.Sprintf("%s:%d:%s\n", relPath, i+1, lines[i]))
			}
			matchCount++
		}

		return nil
	})

	return buf.String(), err
}

// isBinaryFile checks if a file is likely binary by looking for null bytes
// in the first 512 bytes.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	// Check for null bytes — a reliable indicator of binary content.
	if !utf8.Valid(buf[:n]) {
		return true
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// relativizePaths makes absolute paths in ripgrep output relative to rootDir.
func (s *SearchTool) relativizePaths(output string) string {
	return strings.ReplaceAll(output, s.rootDir+string(filepath.Separator), "")
}
