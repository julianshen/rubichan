package xcode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverTool_Name(t *testing.T) {
	d := NewDiscoverTool("/tmp")
	assert.Equal(t, "xcode_discover", d.Name())
}

func TestDiscoverTool_XcodeProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "MyApp.xcodeproj"), 0o755))

	d := NewDiscoverTool(dir)
	input, _ := json.Marshal(map[string]string{})
	result, err := d.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "xcodeproj")
	assert.Contains(t, result.Content, "MyApp")
}

func TestDiscoverTool_SwiftPackage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Package.swift"), []byte("// swift-tools-version:5.9"), 0o644))

	d := NewDiscoverTool(dir)
	input, _ := json.Marshal(map[string]string{})
	result, err := d.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "spm")
}

func TestDiscoverTool_NoProject(t *testing.T) {
	dir := t.TempDir()

	d := NewDiscoverTool(dir)
	input, _ := json.Marshal(map[string]string{})
	result, err := d.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "no Apple project")
}

func TestDiscoverTool_Workspace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "MyApp.xcworkspace"), 0o755))

	d := NewDiscoverTool(dir)
	input, _ := json.Marshal(map[string]string{})
	result, err := d.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "xcworkspace")
}

func TestDiscoverTool_Description(t *testing.T) {
	d := NewDiscoverTool("/tmp")
	assert.Contains(t, d.Description(), "Detect Apple project type")
}

func TestDiscoverTool_InputSchema(t *testing.T) {
	d := NewDiscoverTool("/tmp")
	schema := d.InputSchema()
	assert.Contains(t, string(schema), `"type": "object"`)
}

func TestDiscoverTool_InvalidInput(t *testing.T) {
	d := NewDiscoverTool("/tmp")
	result, err := d.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestDiscoverTool_SubdirectoryPath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(filepath.Join(sub, "Inner.xcodeproj"), 0o755))

	d := NewDiscoverTool(dir)
	input, _ := json.Marshal(map[string]string{"path": "subdir"})
	result, err := d.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "xcodeproj")
	assert.Contains(t, result.Content, "Inner")
}

func TestDiscoverProject_UnreadableDir(t *testing.T) {
	info := DiscoverProject("/nonexistent/path/that/does/not/exist")
	assert.Equal(t, "none", info.Type)
}

func TestDiscoverTool_WorkspaceOverridesProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "MyApp.xcodeproj"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "MyApp.xcworkspace"), 0o755))

	d := NewDiscoverTool(dir)
	input, _ := json.Marshal(map[string]string{})
	result, err := d.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "xcworkspace")
}

func TestDiscoverTool_SwiftFilesCollected(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.swift"), []byte("print(1)"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helper.swift"), []byte("func f(){}"), 0o644))

	info := DiscoverProject(dir)
	assert.Len(t, info.SwiftFiles, 2)
	assert.Contains(t, info.SwiftFiles, "main.swift")
	assert.Contains(t, info.SwiftFiles, "helper.swift")
}

// Verify it implements tools.Tool interface.
var _ tools.Tool = (*DiscoverTool)(nil)
