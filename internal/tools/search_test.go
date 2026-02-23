package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	// With max_results=1, there should be exactly one file match section
	assert.Contains(t, result.Content, "func")
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
	result, err := s.searchGoNative(dir, searchInput{
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
	result, err := s.searchGoNative(dir, searchInput{
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
