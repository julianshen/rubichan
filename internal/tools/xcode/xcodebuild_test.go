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

func TestXcodeBuildTool_Name(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Equal(t, "xcode_build", NewXcodeBuildTool("/tmp", pc).Name())
	assert.Equal(t, "xcode_test", NewXcodeTestTool("/tmp", pc).Name())
	assert.Equal(t, "xcode_archive", NewXcodeArchiveTool("/tmp", pc).Name())
	assert.Equal(t, "xcode_clean", NewXcodeCleanTool("/tmp", pc).Name())
}

func TestXcodeBuildTool_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	tool := NewXcodeBuildTool("/tmp", pc)

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "requires macOS")
}

func TestXcodeBuildTool_MissingScheme(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeBuildTool("/tmp", pc)

	input, _ := json.Marshal(xcodebuildInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "scheme is required")
}

func TestXcodeBuildTool_InvalidJSON(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeBuildTool("/tmp", pc)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestXcodeBuildTool_InputSchema(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeBuildTool("/tmp", pc)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.InputSchema(), &schema))
	assert.Equal(t, "object", schema["type"])
}

func TestXcodeBuildTool_Description(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Contains(t, NewXcodeBuildTool("/tmp", pc).Description(), "Build")
	assert.Contains(t, NewXcodeTestTool("/tmp", pc).Description(), "tests")
	assert.Contains(t, NewXcodeArchiveTool("/tmp", pc).Description(), "archive")
	assert.Contains(t, NewXcodeCleanTool("/tmp", pc).Description(), "Clean")
}

func TestXcodeBuildTool_BuildArgs(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}

	t.Run("build with workspace", func(t *testing.T) {
		tool := NewXcodeBuildTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{
			Workspace:     "App.xcworkspace",
			Scheme:        "MyApp",
			Destination:   "platform=iOS Simulator,name=iPhone 15",
			Configuration: "Debug",
		})
		assert.Contains(t, args, "-workspace")
		assert.Contains(t, args, "App.xcworkspace")
		assert.Contains(t, args, "-scheme")
		assert.Contains(t, args, "MyApp")
		assert.Contains(t, args, "-destination")
		assert.Contains(t, args, "-configuration")
		assert.Contains(t, args, "build")
		assert.Contains(t, args, "-quiet")
	})

	t.Run("build with project", func(t *testing.T) {
		tool := NewXcodeBuildTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{
			Project: "App.xcodeproj",
			Scheme:  "MyApp",
		})
		assert.Contains(t, args, "-project")
		assert.Contains(t, args, "App.xcodeproj")
	})

	t.Run("workspace takes precedence over project", func(t *testing.T) {
		tool := NewXcodeBuildTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{
			Workspace: "App.xcworkspace",
			Project:   "App.xcodeproj",
			Scheme:    "MyApp",
		})
		assert.Contains(t, args, "-workspace")
		assert.NotContains(t, args, "-project")
	})

	t.Run("test mode", func(t *testing.T) {
		tool := NewXcodeTestTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{Scheme: "MyApp"})
		assert.Contains(t, args, "test")
		assert.NotContains(t, args, "build")
	})

	t.Run("archive mode with archive path", func(t *testing.T) {
		tool := NewXcodeArchiveTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{
			Scheme:      "MyApp",
			ArchivePath: "/tmp/MyApp.xcarchive",
		})
		assert.Contains(t, args, "archive")
		assert.Contains(t, args, "-archivePath")
		assert.Contains(t, args, "/tmp/MyApp.xcarchive")
	})

	t.Run("clean mode", func(t *testing.T) {
		tool := NewXcodeCleanTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{Scheme: "MyApp"})
		assert.Contains(t, args, "clean")
	})

	t.Run("minimal args", func(t *testing.T) {
		tool := NewXcodeBuildTool("/tmp", pc)
		args := tool.buildArgs(xcodebuildInput{Scheme: "MyApp"})
		assert.Equal(t, []string{"-scheme", "MyApp", "build", "-quiet"}, args)
	})
}

func TestXcodeBuildTool_Execute_BuildSuccess(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeBuildTool(t.TempDir(), pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("BUILD SUCCEEDED\n"), nil
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Build succeeded")
}

func TestXcodeBuildTool_Execute_BuildFailure(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeBuildTool(t.TempDir(), pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("main.swift:10:5: error: use of unresolved identifier 'foo'\n"), fmt.Errorf("exit status 65")
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "Build failed")
	assert.Contains(t, result.Content, "unresolved identifier")
}

func TestXcodeBuildTool_Execute_BuildWithWarnings(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeBuildTool(t.TempDir(), pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			return []byte("main.swift:5:1: warning: unused variable 'x'\nBUILD SUCCEEDED\n"), nil
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Build succeeded")
	assert.Contains(t, result.Content, "WARNING")
}

func TestXcodeBuildTool_Execute_TestModeSuccess(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeTestTool(t.TempDir(), pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			log := "Test Case '-[MyTests testExample]' passed (0.001 seconds)\n" +
				"Executed 1 test, with 0 failures\n"
			return []byte(log), nil
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "1 total")
	assert.Contains(t, result.Content, "PASS")
}

func TestXcodeBuildTool_Execute_TestModeFailure(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeTestTool(t.TempDir(), pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			log := "Test Case '-[MyTests testBroken]' failed (0.002 seconds)\n" +
				"Executed 1 test, with 1 failure\n"
			return []byte(log), fmt.Errorf("exit status 65")
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "1 failed")
	assert.Contains(t, result.Content, "FAIL")
}

func TestXcodeBuildTool_Execute_TestModeWithBuildErrors(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewXcodeTestTool(t.TempDir(), pc)
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			log := "main.swift:10:5: error: use of unresolved identifier 'foo'\n"
			return []byte(log), fmt.Errorf("exit status 65")
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unresolved identifier")
}

func TestXcodeBuildTool_Execute_PathTraversal(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewXcodeBuildTool(rootDir, pc)
	tool.runner = &MockRunner{}

	input, _ := json.Marshal(xcodebuildInput{
		Scheme:    "MyApp",
		Workspace: "../../etc/passwd",
	})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "escapes")
}

func TestXcodeBuildTool_Execute_PassesDirToRunner(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	rootDir := t.TempDir()
	tool := NewXcodeBuildTool(rootDir, pc)

	var capturedDir string
	tool.runner = &MockRunner{
		CombinedOutputFunc: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			capturedDir = dir
			return []byte("BUILD SUCCEEDED\n"), nil
		},
	}

	input, _ := json.Marshal(xcodebuildInput{Scheme: "MyApp"})
	_, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, rootDir, capturedDir)
}

// Verify interface compliance.
var _ tools.Tool = (*XcodeBuildTool)(nil)
