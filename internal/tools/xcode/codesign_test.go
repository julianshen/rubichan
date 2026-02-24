package xcode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verify interface compliance at compile time.
var _ tools.Tool = (*CodesignTool)(nil)

func TestCodesignTool_Names(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Equal(t, "codesign_info", NewCodesignInfoTool(pc).Name())
	assert.Equal(t, "codesign_verify", NewCodesignVerifyTool(pc).Name())
}

func TestCodesignTool_Description(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Contains(t, NewCodesignInfoTool(pc).Description(), "signing identities")
	assert.Contains(t, NewCodesignVerifyTool(pc).Description(), "Verif")
}

func TestCodesignTool_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}

	t.Run("info", func(t *testing.T) {
		tool := NewCodesignInfoTool(pc)
		input, _ := json.Marshal(codesignInput{})
		result, err := tool.Execute(context.Background(), input)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "requires macOS")
	})

	t.Run("verify", func(t *testing.T) {
		tool := NewCodesignVerifyTool(pc)
		input, _ := json.Marshal(codesignInput{Path: "/path/to/App.app"})
		result, err := tool.Execute(context.Background(), input)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "requires macOS")
	})
}

func TestCodesignTool_VerifyMissingPath(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewCodesignVerifyTool(pc)

	input, _ := json.Marshal(codesignInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "path is required")
}

func TestCodesignTool_InvalidJSON(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}

	t.Run("info", func(t *testing.T) {
		tool := NewCodesignInfoTool(pc)
		result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "invalid input")
	})

	t.Run("verify", func(t *testing.T) {
		tool := NewCodesignVerifyTool(pc)
		result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "invalid input")
	})
}

func TestCodesignTool_InputSchema(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}

	t.Run("info", func(t *testing.T) {
		var schema map[string]any
		require.NoError(t, json.Unmarshal(NewCodesignInfoTool(pc).InputSchema(), &schema))
		assert.Equal(t, "object", schema["type"])
	})

	t.Run("verify", func(t *testing.T) {
		var schema map[string]any
		require.NoError(t, json.Unmarshal(NewCodesignVerifyTool(pc).InputSchema(), &schema))
		assert.Equal(t, "object", schema["type"])
		// verify schema should require "path"
		required, ok := schema["required"].([]any)
		require.True(t, ok)
		assert.Contains(t, required, "path")
	})
}
