package xcode

import (
	"context"
	"encoding/json"
	"fmt"
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
	assert.Equal(t, "codesign_verify", NewCodesignVerifyTool("/tmp", pc).Name())
}

func TestCodesignTool_Description(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Contains(t, NewCodesignInfoTool(pc).Description(), "signing identities")
	assert.Contains(t, NewCodesignVerifyTool("/tmp", pc).Description(), "Verif")
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
		tool := NewCodesignVerifyTool("/tmp", pc)
		input, _ := json.Marshal(codesignInput{Path: "/path/to/App.app"})
		result, err := tool.Execute(context.Background(), input)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "requires macOS")
	})
}

func TestCodesignTool_VerifyMissingPath(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewCodesignVerifyTool("/tmp", pc)

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
		tool := NewCodesignVerifyTool("/tmp", pc)
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
		require.NoError(t, json.Unmarshal(NewCodesignVerifyTool("/tmp", pc).InputSchema(), &schema))
		assert.Equal(t, "object", schema["type"])
		// verify schema should require "path"
		required, ok := schema["required"].([]any)
		require.True(t, ok)
		assert.Contains(t, required, "path")
	})
}

func TestCodesignTool_Execute_InfoSuccess(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewCodesignInfoTool(pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("1) ABCDEF123 \"Apple Development: test@example.com\"\n   1 valid identities found"), nil
		},
	}

	input, _ := json.Marshal(codesignInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Apple Development")
}

func TestCodesignTool_Execute_InfoEmpty(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewCodesignInfoTool(pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return nil, nil
		},
	}

	input, _ := json.Marshal(codesignInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "No signing identities found")
}

func TestCodesignTool_Execute_InfoFailureWithOutput(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewCodesignInfoTool(pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("security: SecKeychainSearchCopyNext: error"), fmt.Errorf("exit status 1")
		},
	}

	input, _ := json.Marshal(codesignInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "SecKeychainSearchCopyNext")
}

func TestCodesignTool_Execute_InfoFailureNoOutput(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewCodesignInfoTool(pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return nil, fmt.Errorf("exit status 1")
		},
	}

	input, _ := json.Marshal(codesignInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "security find-identity failed")
}

func TestCodesignTool_Execute_VerifySuccess(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewCodesignVerifyTool(rootDir, pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return nil, nil
		},
	}

	input, _ := json.Marshal(codesignInput{Path: "MyApp.app"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Signature verified successfully")
}

func TestCodesignTool_Execute_VerifySuccessWithOutput(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewCodesignVerifyTool(rootDir, pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("MyApp.app: valid on disk"), nil
		},
	}

	input, _ := json.Marshal(codesignInput{Path: "MyApp.app"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "valid on disk")
}

func TestCodesignTool_Execute_VerifyFailure(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewCodesignVerifyTool(rootDir, pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("MyApp.app: a sealed resource is missing or invalid"), fmt.Errorf("exit status 3")
		},
	}

	input, _ := json.Marshal(codesignInput{Path: "MyApp.app"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "sealed resource")
}

func TestCodesignTool_Execute_VerifyFailureNoOutput(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewCodesignVerifyTool(rootDir, pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return nil, fmt.Errorf("exit status 3")
		},
	}

	input, _ := json.Marshal(codesignInput{Path: "MyApp.app"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "codesign verify failed")
}

func TestCodesignTool_Execute_VerifyPathTraversal(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewCodesignVerifyTool(rootDir, pc)
	tool.runner = &MockRunner{}

	input, _ := json.Marshal(codesignInput{Path: "../../etc/passwd"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "escapes")
}

func TestCodesignTool_Execute_VerifyPassesCleanedPath(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewCodesignVerifyTool(rootDir, pc)

	var capturedArgs []string
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			capturedArgs = args
			return nil, nil
		},
	}

	input, _ := json.Marshal(codesignInput{Path: "MyApp.app"})
	_, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	// The last arg should be the absolute cleaned path, not relative.
	lastArg := capturedArgs[len(capturedArgs)-1]
	assert.Contains(t, lastArg, rootDir)
}
