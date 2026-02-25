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
var _ tools.Tool = (*XcrunTool)(nil)

func TestXcrunTool_Name(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)
	assert.Equal(t, "xcrun", tool.Name())
}

func TestXcrunTool_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	tool := NewXcrunTool(pc)

	input, _ := json.Marshal(xcrunInput{Tool: "instruments"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "requires macOS")
}

func TestXcrunTool_MissingToolName(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)

	input, _ := json.Marshal(xcrunInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "tool is required")
}

func TestXcrunTool_UnknownTool(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)

	input, _ := json.Marshal(xcrunInput{Tool: "rm"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "tool not allowed")
}

func TestXcrunTool_AllowedTools(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)

	allowedTools := []string{"instruments", "strings", "swift-demangle", "sourcekit-lsp", "simctl"}
	for _, toolName := range allowedTools {
		t.Run(toolName, func(t *testing.T) {
			input, _ := json.Marshal(xcrunInput{Tool: toolName, Args: []string{"--help"}})
			result, err := tool.Execute(context.Background(), input)
			require.NoError(t, err)
			// The tool should attempt to run (not reject as "not allowed").
			// It may fail on the actual command if xcrun isn't available, but
			// the allowlist check should pass.
			assert.NotContains(t, result.Content, "tool not allowed")
		})
	}
}

func TestXcrunTool_InvalidJSON(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestXcrunTool_InputSchema(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.InputSchema(), &schema))
	assert.Equal(t, "object", schema["type"])

	// Verify "tool" is required.
	required, ok := schema["required"].([]any)
	require.True(t, ok)
	assert.Contains(t, required, "tool")
}

func TestXcrunTool_Description(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcrunTool(pc)
	desc := tool.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "xcrun")
}
