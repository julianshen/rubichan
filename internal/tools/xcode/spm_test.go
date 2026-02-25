package xcode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSPMTool_Names(t *testing.T) {
	assert.Equal(t, "swift_build", NewSwiftBuildTool("/tmp").Name())
	assert.Equal(t, "swift_test", NewSwiftTestTool("/tmp").Name())
	assert.Equal(t, "swift_resolve", NewSwiftResolveTool("/tmp").Name())
	assert.Equal(t, "swift_add_dep", NewSwiftAddDepTool("/tmp").Name())
}

func TestSPMTool_Description(t *testing.T) {
	assert.Contains(t, NewSwiftBuildTool("/tmp").Description(), "Build")
	assert.Contains(t, NewSwiftTestTool("/tmp").Description(), "test")
	assert.Contains(t, NewSwiftResolveTool("/tmp").Description(), "Resolve")
	assert.Contains(t, NewSwiftAddDepTool("/tmp").Description(), "dependency")
}

func TestSPMTool_InputSchema(t *testing.T) {
	allTools := []*SPMTool{
		NewSwiftBuildTool("/tmp"),
		NewSwiftTestTool("/tmp"),
		NewSwiftResolveTool("/tmp"),
		NewSwiftAddDepTool("/tmp"),
	}

	for _, tool := range allTools {
		t.Run(tool.Name(), func(t *testing.T) {
			var schema map[string]any
			require.NoError(t, json.Unmarshal(tool.InputSchema(), &schema))
			assert.Equal(t, "object", schema["type"])
		})
	}
}

func TestSwiftAddDepTool_InputSchemaRequiredFields(t *testing.T) {
	tool := NewSwiftAddDepTool("/tmp")
	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.InputSchema(), &schema))
	required, ok := schema["required"].([]any)
	require.True(t, ok)
	assert.Contains(t, required, "url")
	assert.Contains(t, required, "from_version")
}

func TestSwiftBuildTool_InvalidJSON(t *testing.T) {
	tool := NewSwiftBuildTool("/tmp")
	result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestSwiftAddDepTool_MissingURL(t *testing.T) {
	tool := NewSwiftAddDepTool("/tmp")
	input, _ := json.Marshal(spmInput{FromVersion: "1.0.0"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "url is required")
}

func TestSwiftAddDepTool_MissingFromVersion(t *testing.T) {
	tool := NewSwiftAddDepTool("/tmp")
	input, _ := json.Marshal(spmInput{URL: "https://github.com/example/repo"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "from_version is required")
}

func TestSwiftAddDepTool_NonHTTPSURL(t *testing.T) {
	tool := NewSwiftAddDepTool("/tmp")
	input, _ := json.Marshal(spmInput{URL: "file:///etc/passwd", FromVersion: "1.0.0"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "https://")
}

func TestSPMTool_BuildArgs(t *testing.T) {
	t.Run("swift_build minimal", func(t *testing.T) {
		tool := NewSwiftBuildTool("/tmp")
		args := tool.buildArgs(spmInput{})
		assert.Equal(t, []string{"build"}, args)
	})

	t.Run("swift_build with package path and config", func(t *testing.T) {
		tool := NewSwiftBuildTool("/tmp")
		args := tool.buildArgs(spmInput{
			PackagePath:   "/path/to/pkg",
			Configuration: "release",
		})
		assert.Equal(t, []string{"build", "--package-path", "/path/to/pkg", "-c", "release"}, args)
	})

	t.Run("swift_test minimal", func(t *testing.T) {
		tool := NewSwiftTestTool("/tmp")
		args := tool.buildArgs(spmInput{})
		assert.Equal(t, []string{"test"}, args)
	})

	t.Run("swift_test with config", func(t *testing.T) {
		tool := NewSwiftTestTool("/tmp")
		args := tool.buildArgs(spmInput{Configuration: "debug"})
		assert.Equal(t, []string{"test", "-c", "debug"}, args)
	})

	t.Run("swift_resolve minimal", func(t *testing.T) {
		tool := NewSwiftResolveTool("/tmp")
		args := tool.buildArgs(spmInput{})
		assert.Equal(t, []string{"package", "resolve"}, args)
	})

	t.Run("swift_resolve with package path", func(t *testing.T) {
		tool := NewSwiftResolveTool("/tmp")
		args := tool.buildArgs(spmInput{PackagePath: "/path/to/pkg"})
		assert.Equal(t, []string{"package", "resolve", "--package-path", "/path/to/pkg"}, args)
	})

	t.Run("swift_add_dep full", func(t *testing.T) {
		tool := NewSwiftAddDepTool("/tmp")
		args := tool.buildArgs(spmInput{
			URL:         "https://github.com/example/repo",
			FromVersion: "1.0.0",
			PackagePath: "/path/to/pkg",
		})
		assert.Equal(t, []string{
			"package", "add-dependency",
			"https://github.com/example/repo",
			"--from", "1.0.0",
			"--package-path", "/path/to/pkg",
		}, args)
	})

	t.Run("swift_add_dep without package path", func(t *testing.T) {
		tool := NewSwiftAddDepTool("/tmp")
		args := tool.buildArgs(spmInput{
			URL:         "https://github.com/example/repo",
			FromVersion: "2.0.0",
		})
		assert.Equal(t, []string{
			"package", "add-dependency",
			"https://github.com/example/repo",
			"--from", "2.0.0",
		}, args)
	})
}

// Verify interface compliance.
var _ tools.Tool = (*SPMTool)(nil)
