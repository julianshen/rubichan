package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSearchDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create test files
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "util.go"), []byte("package src\n\nfunc Util() string {\n\treturn \"util\"\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("This is a readme file.\nIt has multiple lines.\n"), 0o644))

	return dir
}

func TestSearchTool_Name(t *testing.T) {
	s := NewSearchTool(t.TempDir())
	assert.Equal(t, "search", s.Name())
}

func TestSearchTool_Description(t *testing.T) {
	s := NewSearchTool(t.TempDir())
	assert.Contains(t, s.Description(), "Search")
}

func TestSearchTool_InputSchema(t *testing.T) {
	s := NewSearchTool(t.TempDir())
	var schema map[string]any
	require.NoError(t, json.Unmarshal(s.InputSchema(), &schema))
	assert.Equal(t, "object", schema["type"])
}

func TestSearchTool_PatternMatch(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func main"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "main.go")
	assert.Contains(t, result.Content, "func main")
}

func TestSearchTool_FilePatternFilter(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func", FilePattern: "*.go"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "main.go")
	assert.Contains(t, result.Content, "util.go")
	assert.NotContains(t, result.Content, "readme.txt")
}

func TestSearchTool_SubdirPath(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "Util", Path: "src"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "util.go")
}

func TestSearchTool_MaxResults(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func", MaxResults: 1})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "func")
	// Verify the global cap: count match lines (file:line:content format).
	matchLines := 0
	for _, line := range strings.Split(result.Content, "\n") {
		if strings.Contains(line, ":") && strings.Contains(line, "func") {
			matchLines++
		}
	}
	assert.Equal(t, 1, matchLines, "should enforce global max_results cap")
}

func TestSearchTool_ContextLines(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "Println", ContextLines: 1})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "main.go")
}

func TestSearchTool_NoMatches(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "nonexistent_xyz_pattern"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "no matches")
}

func TestSearchTool_InvalidRegex(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "[invalid"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}

func TestSearchTool_PathTraversal(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func", Path: "../.."})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path traversal")
}

func TestSearchTool_EmptyPattern(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: ""})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "pattern is required")
}

func TestSearchTool_InvalidJSON(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	result, err := s.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestSearchTool_GoNativeFallback(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	// Directly call the Go-native search to verify it works.
	result, err := s.searchGoNative(context.Background(), dir, searchInput{
		Pattern:    "func",
		MaxResults: 200,
	})
	require.NoError(t, err)
	assert.Contains(t, result, "main.go")
	assert.Contains(t, result, "util.go")
}

func TestSearchTool_GoNativeSkipsDotGit(t *testing.T) {
	dir := setupSearchDir(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("func hidden"), 0o644))

	s := NewSearchTool(dir)
	result, err := s.searchGoNative(context.Background(), dir, searchInput{
		Pattern:    "hidden",
		MaxResults: 200,
	})
	require.NoError(t, err)
	assert.Empty(t, result, ".git directory should be skipped")
}

func TestSearchTool_AbsolutePathDenied(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func", Path: "/etc"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path traversal")
}

func TestSearchTool_SymlinkTraversal(t *testing.T) {
	dir := setupSearchDir(t)
	// Create a symlink inside the root that points outside.
	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "secret.go"), []byte("func secret()"), 0o644))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(dir, "escape")))

	s := NewSearchTool(dir)
	input, _ := json.Marshal(searchInput{Pattern: "secret", Path: "escape"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path traversal denied")
}

func TestSearchTool_GoNativeSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	// Create a binary file with null bytes.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.dat"), []byte("func\x00binary\x00data"), 0o644))
	// Create a text file with the same pattern.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "text.go"), []byte("func textOnly\n"), 0o644))

	s := NewSearchTool(dir)
	result, err := s.searchGoNative(context.Background(), dir, searchInput{
		Pattern:    "func",
		MaxResults: 200,
	})
	require.NoError(t, err)
	assert.Contains(t, result, "text.go")
	assert.NotContains(t, result, "binary.dat")
}

func TestSearchTool_GoNativeContextCancellation(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := s.searchGoNative(ctx, dir, searchInput{
		Pattern:    "func",
		MaxResults: 200,
	})
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSearchTool_OutputTruncation(t *testing.T) {
	dir := t.TempDir()
	// Create a large file that will produce output exceeding 30KB.
	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString("func largeFunction" + strings.Repeat("x", 30) + "()\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "large.go"), []byte(content.String()), 0o644))

	s := NewSearchTool(dir)
	input, _ := json.Marshal(searchInput{Pattern: "func large", MaxResults: 2000})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.LessOrEqual(t, len(result.Content), maxOutputBytes+50) // +50 for truncation message
	assert.Contains(t, result.Content, "truncated")
}

func TestSearchTool_PathNotFound(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func", Path: "nonexistent"})
	result, err := s.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path not found")
}

func TestEnforceMaxResults_NoContext(t *testing.T) {
	output := "file1.go:1:func a()\nfile2.go:2:func b()\nfile3.go:3:func c()\n"
	result := enforceMaxResults(output, 2, false)
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	assert.Equal(t, 2, len(lines))
}

func TestEnforceMaxResults_WithContext(t *testing.T) {
	output := "file1.go:1:line1\nfile1.go:2:match1\nfile1.go:3:line3\n--\nfile2.go:5:line5\nfile2.go:6:match2\nfile2.go:7:line7\n--\nfile3.go:10:match3\n"
	result := enforceMaxResults(output, 2, true)
	assert.Contains(t, result, "match1")
	assert.Contains(t, result, "match2")
	assert.NotContains(t, result, "match3")
}

func TestSearchTool_GoNativeMaxResultsWithContext(t *testing.T) {
	dir := t.TempDir()
	// Create a file with many matches.
	var content strings.Builder
	for i := 0; i < 20; i++ {
		content.WriteString("func handler" + strings.Repeat("x", 5) + "()\n")
		content.WriteString("// non-matching line\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handlers.go"), []byte(content.String()), 0o644))

	s := NewSearchTool(dir)
	result, err := s.searchGoNative(context.Background(), dir, searchInput{
		Pattern:      "func handler",
		MaxResults:   3,
		ContextLines: 1,
	})
	require.NoError(t, err)
	// Count the match lines (the lines that actually match the pattern).
	matchCount := 0
	for _, line := range strings.Split(result, "\n") {
		if strings.Contains(line, "func handler") {
			matchCount++
		}
	}
	assert.Equal(t, 3, matchCount)
}

func TestSearchTool_GoNativeFilePatternNoMatch(t *testing.T) {
	dir := setupSearchDir(t)
	s := NewSearchTool(dir)

	result, err := s.searchGoNative(context.Background(), dir, searchInput{
		Pattern:     "func",
		FilePattern: "*.py",
		MaxResults:  200,
	})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	textFile := filepath.Join(dir, "text.txt")
	require.NoError(t, os.WriteFile(textFile, []byte("hello world"), 0o644))
	assert.False(t, isBinaryFile(textFile))

	binaryFile := filepath.Join(dir, "binary.bin")
	require.NoError(t, os.WriteFile(binaryFile, []byte("hello\x00world"), 0o644))
	assert.True(t, isBinaryFile(binaryFile))

	emptyFile := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(emptyFile, nil, 0o644))
	assert.False(t, isBinaryFile(emptyFile))
}
