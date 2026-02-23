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
		searchDir = abs
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
		result, err = s.searchGoNative(searchDir, in)
	}

	if err != nil {
		return ToolResult{Content: fmt.Sprintf("search error: %s", err), IsError: true}, nil
	}

	if result == "" {
		return ToolResult{Content: "no matches found"}, nil
	}

	// Truncate to 30KB like shell tool.
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... output truncated"
	}

	return ToolResult{Content: result}, nil
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
	if in.MaxResults > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", in.MaxResults))
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
		// Exit code 2 means an actual error.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 2 {
			return "", fmt.Errorf("ripgrep error: %s", strings.TrimSpace(string(out)))
		}
	}

	// Make paths relative to rootDir for cleaner output.
	return s.relativizePaths(string(out)), nil
}

func (s *SearchTool) searchGoNative(searchDir string, in searchInput) (string, error) {
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	matchCount := 0

	err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, walkErr error) error {
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

// relativizePaths makes absolute paths in ripgrep output relative to rootDir.
func (s *SearchTool) relativizePaths(output string) string {
	return strings.ReplaceAll(output, s.rootDir+string(filepath.Separator), "")
}
