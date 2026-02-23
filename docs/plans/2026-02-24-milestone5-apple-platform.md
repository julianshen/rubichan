# Milestone 5: Apple Platform + Polish — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement Xcode/Apple development tools (FR-6), assemble the apple-dev built-in skill, integrate security findings into wiki output, and write a skill authoring guide.

**Architecture:** New `internal/tools/xcode/` package with each tool file implementing the `tools.Tool` interface (same pattern as `internal/tools/shell.go` and `internal/tools/search.go`). The apple-dev skill in `internal/skills/builtin/appledev/` follows the `CoreToolsBackend` pattern from `internal/skills/builtin/core_tools.go`. Wiki-security integration modifies `internal/wiki/assembler.go` to accept a security report.

**Tech Stack:** Go, `os/exec` for CLI wrappers, `encoding/json` for parsing xcodebuild/simctl JSON output, `//go:embed` for prompt files, existing `tools.Tool` interface and `tools.Registry`.

---

## PR 1: Platform Gating + Project Discovery + Log Parser

### Task 1: Platform checker interface and implementation

**Files:**
- Create: `internal/tools/xcode/platform.go`
- Create: `internal/tools/xcode/platform_test.go`

**Step 1: Write the failing test**

```go
// internal/tools/xcode/platform_test.go
package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRealPlatformChecker_IsDarwin(t *testing.T) {
	pc := NewRealPlatformChecker()
	// We can't assert a specific value since tests run on different OSes,
	// but we can verify the method returns without panic.
	_ = pc.IsDarwin()
}

func TestMockPlatformChecker_Darwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/Applications/Xcode.app/Contents/Developer"}
	assert.True(t, pc.IsDarwin())
	path, err := pc.XcodePath()
	assert.NoError(t, err)
	assert.Equal(t, "/Applications/Xcode.app/Contents/Developer", path)
}

func TestMockPlatformChecker_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	assert.False(t, pc.IsDarwin())
	_, err := pc.XcodePath()
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/xcode/ -run TestRealPlatformChecker -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Write minimal implementation**

```go
// internal/tools/xcode/platform.go
package xcode

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// PlatformChecker abstracts platform detection for testability.
type PlatformChecker interface {
	IsDarwin() bool
	XcodePath() (string, error)
}

// RealPlatformChecker uses runtime.GOOS and xcode-select.
type RealPlatformChecker struct{}

func NewRealPlatformChecker() *RealPlatformChecker {
	return &RealPlatformChecker{}
}

func (r *RealPlatformChecker) IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

func (r *RealPlatformChecker) XcodePath() (string, error) {
	if !r.IsDarwin() {
		return "", fmt.Errorf("Xcode is only available on macOS")
	}
	out, err := exec.Command("xcode-select", "-p").Output()
	if err != nil {
		return "", fmt.Errorf("xcode-select failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MockPlatformChecker is used in tests.
type MockPlatformChecker struct {
	Darwin       bool
	XcodeBinPath string
}

func (m *MockPlatformChecker) IsDarwin() bool {
	return m.Darwin
}

func (m *MockPlatformChecker) XcodePath() (string, error) {
	if !m.Darwin {
		return "", fmt.Errorf("Xcode is only available on macOS")
	}
	return m.XcodeBinPath, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/xcode/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/xcode/platform.go internal/tools/xcode/platform_test.go
git commit -m "[BEHAVIORAL] Add PlatformChecker interface for Xcode tool gating"
```

---

### Task 2: Build log parser

**Files:**
- Create: `internal/tools/xcode/logparser.go`
- Create: `internal/tools/xcode/logparser_test.go`

**Step 1: Write the failing test**

```go
// internal/tools/xcode/logparser_test.go
package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBuildLog_Errors(t *testing.T) {
	log := `/path/main.swift:10:5: error: use of unresolved identifier 'foo'
/path/main.swift:20:3: warning: result unused
** BUILD FAILED **`

	result := ParseBuildLog(log)
	assert.False(t, result.Success)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "/path/main.swift", result.Errors[0].File)
	assert.Equal(t, 10, result.Errors[0].Line)
	assert.Contains(t, result.Errors[0].Message, "unresolved identifier")
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, 20, result.Warnings[0].Line)
}

func TestParseBuildLog_Success(t *testing.T) {
	log := `** BUILD SUCCEEDED **`

	result := ParseBuildLog(log)
	assert.True(t, result.Success)
	assert.Empty(t, result.Errors)
}

func TestParseTestLog_Results(t *testing.T) {
	log := `Test Case '-[MyTests.FooTest testBar]' passed (0.001 seconds).
Test Case '-[MyTests.FooTest testBaz]' failed (0.002 seconds).
Test Suite 'All tests' passed at 2024-01-01 00:00:00.
	 Executed 2 tests, with 1 failure (0 unexpected) in 0.003 (0.004) seconds`

	result := ParseTestLog(log)
	assert.Equal(t, 2, result.Total)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Cases, 2)
	assert.True(t, result.Cases[0].Passed)
	assert.False(t, result.Cases[1].Passed)
}

func TestParseBuildLog_EmptyInput(t *testing.T) {
	result := ParseBuildLog("")
	assert.False(t, result.Success)
	assert.Empty(t, result.Errors)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/xcode/ -run TestParseBuildLog -v`
Expected: FAIL — ParseBuildLog not defined

**Step 3: Write minimal implementation**

```go
// internal/tools/xcode/logparser.go
package xcode

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BuildResult holds parsed xcodebuild output.
type BuildResult struct {
	Success  bool
	Errors   []BuildDiagnostic
	Warnings []BuildDiagnostic
	RawLog   string
}

// BuildDiagnostic represents a single error or warning from xcodebuild.
type BuildDiagnostic struct {
	File    string
	Line    int
	Column  int
	Message string
}

// TestResult holds parsed test output.
type TestResult struct {
	Total  int
	Passed int
	Failed int
	Cases  []TestCase
}

// TestCase represents a single test case result.
type TestCase struct {
	Suite    string
	Name     string
	Passed   bool
	Duration float64
}

var (
	diagnosticRe = regexp.MustCompile(`^(.+?):(\d+):(\d+): (error|warning): (.+)$`)
	testCaseRe   = regexp.MustCompile(`Test Case '-\[(\S+) (\S+)\]' (passed|failed) \((\d+\.\d+) seconds\)`)
	testSummaryRe = regexp.MustCompile(`Executed (\d+) tests?, with (\d+) failures?`)
)

// ParseBuildLog extracts errors and warnings from xcodebuild output.
func ParseBuildLog(log string) BuildResult {
	result := BuildResult{RawLog: log}

	for _, line := range strings.Split(log, "\n") {
		if matches := diagnosticRe.FindStringSubmatch(line); matches != nil {
			lineNum, _ := strconv.Atoi(matches[2])
			colNum, _ := strconv.Atoi(matches[3])
			diag := BuildDiagnostic{
				File:    matches[1],
				Line:    lineNum,
				Column:  colNum,
				Message: matches[5],
			}
			if matches[4] == "error" {
				result.Errors = append(result.Errors, diag)
			} else {
				result.Warnings = append(result.Warnings, diag)
			}
		}
		if strings.Contains(line, "BUILD SUCCEEDED") {
			result.Success = true
		}
	}
	return result
}

// ParseTestLog extracts test case results from xcodebuild test output.
func ParseTestLog(log string) TestResult {
	var result TestResult

	for _, line := range strings.Split(log, "\n") {
		if matches := testCaseRe.FindStringSubmatch(line); matches != nil {
			dur, _ := strconv.ParseFloat(matches[4], 64)
			tc := TestCase{
				Suite:    matches[1],
				Name:     matches[2],
				Passed:   matches[3] == "passed",
				Duration: dur,
			}
			result.Cases = append(result.Cases, tc)
		}
		if matches := testSummaryRe.FindStringSubmatch(line); matches != nil {
			result.Total, _ = strconv.Atoi(matches[1])
			result.Failed, _ = strconv.Atoi(matches[2])
			result.Passed = result.Total - result.Failed
		}
	}
	return result
}

// FormatBuildResult returns a human-readable summary of a build.
func FormatBuildResult(r BuildResult) string {
	var b strings.Builder
	if r.Success {
		b.WriteString("Build succeeded.\n")
	} else {
		b.WriteString("Build failed.\n")
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "  ERROR %s:%d:%d: %s\n", e.File, e.Line, e.Column, e.Message)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "  WARNING %s:%d:%d: %s\n", w.File, w.Line, w.Column, w.Message)
	}
	return b.String()
}

// FormatTestResult returns a human-readable summary of test results.
func FormatTestResult(r TestResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tests: %d total, %d passed, %d failed\n", r.Total, r.Passed, r.Failed)
	for _, tc := range r.Cases {
		status := "PASS"
		if !tc.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "  [%s] %s.%s (%.3fs)\n", status, tc.Suite, tc.Name, tc.Duration)
	}
	return b.String()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/xcode/ -run "TestParseBuildLog|TestParseTestLog" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/xcode/logparser.go internal/tools/xcode/logparser_test.go
git commit -m "[BEHAVIORAL] Add xcodebuild log parser for errors, warnings, and test results"
```

---

### Task 3: Project discovery tool

**Files:**
- Create: `internal/tools/xcode/discover.go`
- Create: `internal/tools/xcode/discover_test.go`

**Step 1: Write the failing test**

```go
// internal/tools/xcode/discover_test.go
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

// Verify it implements tools.Tool interface.
var _ tools.Tool = (*DiscoverTool)(nil)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/xcode/ -run TestDiscoverTool -v`
Expected: FAIL — DiscoverTool not defined

**Step 3: Write minimal implementation**

```go
// internal/tools/xcode/discover.go
package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

// ProjectInfo holds discovered Apple project information.
type ProjectInfo struct {
	Type       string   `json:"type"`       // "xcodeproj", "xcworkspace", "spm", "none"
	Name       string   `json:"name"`       // project name
	Path       string   `json:"path"`       // path to project file
	SwiftFiles []string `json:"swift_files"` // .swift files found
}

type discoverInput struct {
	Path string `json:"path,omitempty"`
}

// DiscoverTool detects Apple project types in a directory.
type DiscoverTool struct {
	rootDir string
}

func NewDiscoverTool(rootDir string) *DiscoverTool {
	return &DiscoverTool{rootDir: rootDir}
}

func (d *DiscoverTool) Name() string { return "xcode_discover" }

func (d *DiscoverTool) Description() string {
	return "Detect Apple project type (.xcodeproj, .xcworkspace, Package.swift) in a directory."
}

func (d *DiscoverTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Subdirectory to scan (optional, defaults to project root)"
			}
		}
	}`)
}

func (d *DiscoverTool) Execute(_ context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in discoverInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	scanDir := d.rootDir
	if in.Path != "" {
		scanDir = filepath.Join(d.rootDir, in.Path)
	}

	info := DiscoverProject(scanDir)
	out, _ := json.MarshalIndent(info, "", "  ")
	if info.Type == "none" {
		return tools.ToolResult{Content: fmt.Sprintf("no Apple project found in %s\n%s", scanDir, string(out))}, nil
	}
	return tools.ToolResult{Content: string(out)}, nil
}

// DiscoverProject scans a directory for Apple project files. Exported for
// use in auto-activation checks in main.go.
func DiscoverProject(dir string) ProjectInfo {
	info := ProjectInfo{Type: "none"}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return info
	}

	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".xcworkspace") && e.IsDir():
			info.Type = "xcworkspace"
			info.Name = strings.TrimSuffix(name, ".xcworkspace")
			info.Path = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".xcodeproj") && e.IsDir() && info.Type != "xcworkspace":
			info.Type = "xcodeproj"
			info.Name = strings.TrimSuffix(name, ".xcodeproj")
			info.Path = filepath.Join(dir, name)
		case name == "Package.swift" && info.Type == "none":
			info.Type = "spm"
			info.Name = filepath.Base(dir)
			info.Path = filepath.Join(dir, name)
		}
	}

	// Collect swift files (non-recursive, top-level only).
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".swift") {
			info.SwiftFiles = append(info.SwiftFiles, e.Name())
		}
	}

	return info
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/xcode/ -run TestDiscoverTool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/xcode/discover.go internal/tools/xcode/discover_test.go
git commit -m "[BEHAVIORAL] Add Xcode project discovery tool"
```

---

## PR 2: xcodebuild Tools

### Task 4: XcodeBuild tool (build, test, archive, clean)

**Files:**
- Create: `internal/tools/xcode/xcodebuild.go`
- Create: `internal/tools/xcode/xcodebuild_test.go`

**Context:** Each xcodebuild operation (build, test, archive, clean) is a separate `tools.Tool` implementation. They share the same struct with a mode field. All are platform-gated via `PlatformChecker`. Tests use `MockPlatformChecker` to test error paths without requiring macOS.

**Step 1: Write the failing tests**

```go
// internal/tools/xcode/xcodebuild_test.go
package xcode

import (
	"context"
	"encoding/json"
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

// Verify interface compliance.
var _ tools.Tool = (*XcodeBuildTool)(nil)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/xcode/ -run TestXcodeBuildTool -v`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// internal/tools/xcode/xcodebuild.go
package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

type xcodebuildInput struct {
	Project       string `json:"project,omitempty"`
	Workspace     string `json:"workspace,omitempty"`
	Scheme        string `json:"scheme"`
	Destination   string `json:"destination,omitempty"`
	Configuration string `json:"configuration,omitempty"`
	ArchivePath   string `json:"archive_path,omitempty"`
}

type xcodebuildMode string

const (
	modeBuild   xcodebuildMode = "build"
	modeTest    xcodebuildMode = "test"
	modeArchive xcodebuildMode = "archive"
	modeClean   xcodebuildMode = "clean"
)

// XcodeBuildTool wraps xcodebuild for a specific operation mode.
type XcodeBuildTool struct {
	rootDir  string
	platform PlatformChecker
	mode     xcodebuildMode
}

func NewXcodeBuildTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeBuild}
}

func NewXcodeTestTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeTest}
}

func NewXcodeArchiveTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeArchive}
}

func NewXcodeCleanTool(rootDir string, pc PlatformChecker) *XcodeBuildTool {
	return &XcodeBuildTool{rootDir: rootDir, platform: pc, mode: modeClean}
}

func (x *XcodeBuildTool) Name() string {
	return "xcode_" + string(x.mode)
}

func (x *XcodeBuildTool) Description() string {
	switch x.mode {
	case modeBuild:
		return "Build an Xcode project or workspace. Parses output for errors and warnings."
	case modeTest:
		return "Run tests for an Xcode project. Returns structured test results."
	case modeArchive:
		return "Create an archive for distribution. Requires code signing."
	case modeClean:
		return "Clean build artifacts for a scheme."
	default:
		return "Xcode build operation."
	}
}

func (x *XcodeBuildTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"project":       {"type": "string", "description": ".xcodeproj path (optional if workspace set)"},
			"workspace":     {"type": "string", "description": ".xcworkspace path (optional if project set)"},
			"scheme":        {"type": "string", "description": "Build scheme name"},
			"destination":   {"type": "string", "description": "Build destination (e.g. platform=iOS Simulator,name=iPhone 15)"},
			"configuration": {"type": "string", "description": "Build configuration (Debug/Release)"},
			"archive_path":  {"type": "string", "description": "Archive output path (archive mode only)"}
		},
		"required": ["scheme"]
	}`)
}

func (x *XcodeBuildTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	if !x.platform.IsDarwin() {
		return tools.ToolResult{Content: "xcodebuild requires macOS with Xcode installed", IsError: true}, nil
	}

	var in xcodebuildInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Scheme == "" {
		return tools.ToolResult{Content: "scheme is required", IsError: true}, nil
	}

	args := x.buildArgs(in)
	cmd := exec.CommandContext(ctx, "xcodebuild", args...)
	cmd.Dir = x.rootDir

	out, err := cmd.CombinedOutput()
	output := string(out)

	if x.mode == modeTest {
		testResult := ParseTestLog(output)
		buildResult := ParseBuildLog(output)
		var b strings.Builder
		b.WriteString(FormatTestResult(testResult))
		if len(buildResult.Errors) > 0 {
			b.WriteString("\n")
			b.WriteString(FormatBuildResult(buildResult))
		}
		return tools.ToolResult{Content: b.String(), IsError: !testResult.Passed()}, nil
	}

	buildResult := ParseBuildLog(output)
	if err != nil && !buildResult.Success {
		return tools.ToolResult{Content: FormatBuildResult(buildResult), IsError: true}, nil
	}
	return tools.ToolResult{Content: FormatBuildResult(buildResult)}, nil
}

func (x *XcodeBuildTool) buildArgs(in xcodebuildInput) []string {
	var args []string

	if in.Workspace != "" {
		args = append(args, "-workspace", in.Workspace)
	} else if in.Project != "" {
		args = append(args, "-project", in.Project)
	}

	args = append(args, "-scheme", in.Scheme)

	if in.Destination != "" {
		args = append(args, "-destination", in.Destination)
	}
	if in.Configuration != "" {
		args = append(args, "-configuration", in.Configuration)
	}

	switch x.mode {
	case modeBuild:
		args = append(args, "build")
	case modeTest:
		args = append(args, "test")
	case modeArchive:
		args = append(args, "archive")
		if in.ArchivePath != "" {
			args = append(args, "-archivePath", in.ArchivePath)
		}
	case modeClean:
		args = append(args, "clean")
	}

	args = append(args, "-quiet")
	return args
}
```

Note: `TestResult` needs a `Passed()` method. Add to `logparser.go`:

```go
// Passed returns true if all tests passed.
func (r TestResult) Passed() bool {
	return r.Failed == 0 && r.Total > 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/xcode/ -run TestXcodeBuildTool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/xcode/xcodebuild.go internal/tools/xcode/xcodebuild_test.go internal/tools/xcode/logparser.go
git commit -m "[BEHAVIORAL] Add xcodebuild tools (build, test, archive, clean)"
```

---

## PR 3: Simulator Management

### Task 5: Simctl tool

**Files:**
- Create: `internal/tools/xcode/simctl.go`
- Create: `internal/tools/xcode/simctl_test.go`

**Context:** Each simctl operation is a separate `tools.Tool` implementation sharing a `SimctlTool` struct with a mode field. `sim_list` parses JSON from `xcrun simctl list -j`. All are platform-gated.

**Step 1: Write the failing tests**

```go
// internal/tools/xcode/simctl_test.go
package xcode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimctlTool_Names(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Equal(t, "sim_list", NewSimListTool(pc).Name())
	assert.Equal(t, "sim_boot", NewSimBootTool(pc).Name())
	assert.Equal(t, "sim_shutdown", NewSimShutdownTool(pc).Name())
	assert.Equal(t, "sim_install", NewSimInstallTool(pc).Name())
	assert.Equal(t, "sim_launch", NewSimLaunchTool(pc).Name())
	assert.Equal(t, "sim_screenshot", NewSimScreenshotTool(pc).Name())
}

func TestSimctlTool_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	tool := NewSimListTool(pc)

	input, _ := json.Marshal(map[string]string{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "requires macOS")
}

func TestSimctlTool_BootMissingDevice(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimBootTool(pc)

	input, _ := json.Marshal(simctlInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "device is required")
}

func TestParseSimctlDevices(t *testing.T) {
	jsonData := `{
		"devices": {
			"com.apple.CoreSimulator.SimRuntime.iOS-17-0": [
				{"name": "iPhone 15", "udid": "ABC-123", "state": "Shutdown", "isAvailable": true},
				{"name": "iPhone 15 Pro", "udid": "DEF-456", "state": "Booted", "isAvailable": true}
			]
		}
	}`
	devices := ParseSimctlDevices([]byte(jsonData))
	require.Len(t, devices, 2)
	assert.Equal(t, "iPhone 15", devices[0].Name)
	assert.Equal(t, "Shutdown", devices[0].State)
	assert.Equal(t, "iOS-17-0", devices[0].Runtime)
}

// Verify interface compliance.
var _ tools.Tool = (*SimctlTool)(nil)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/xcode/ -run "TestSimctlTool|TestParseSimctl" -v`
Expected: FAIL

**Step 3: Write implementation**

The implementation follows the same pattern as `XcodeBuildTool`: struct with mode, platform gate, input validation, `exec.CommandContext` to run `xcrun simctl`, parse output. Each mode constructs the appropriate `xcrun simctl <subcommand>` args. `sim_list` parses JSON output into `[]SimDevice`. Other modes execute and return stdout.

`ParseSimctlDevices` unmarshals the `xcrun simctl list -j devices` JSON into a flat `[]SimDevice` slice, extracting the runtime name from the dictionary key.

**Step 4: Run tests, Step 5: Commit**

```bash
git add internal/tools/xcode/simctl.go internal/tools/xcode/simctl_test.go
git commit -m "[BEHAVIORAL] Add simulator management tools (sim_list, sim_boot, sim_shutdown, sim_install, sim_launch, sim_screenshot)"
```

---

## PR 4: Swift Package Manager

### Task 6: SPM tool

**Files:**
- Create: `internal/tools/xcode/spm.go`
- Create: `internal/tools/xcode/spm_test.go`

**Context:** SPM tools are cross-platform (work on Linux too). They use `swift` CLI directly, not `xcodebuild`. Platform gating: only `swift_add_dep` requires approval (modifies Package.swift). The `swift` binary must be on PATH.

**Step 1: Write the failing tests**

```go
// internal/tools/xcode/spm_test.go
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

func TestSwiftBuildTool_InvalidJSON(t *testing.T) {
	tool := NewSwiftBuildTool("/tmp")
	result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestSwiftAddDepTool_MissingURL(t *testing.T) {
	tool := NewSwiftAddDepTool("/tmp")
	input, _ := json.Marshal(spmInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "url is required")
}

func TestSwiftBuildTool_InputSchema(t *testing.T) {
	tool := NewSwiftBuildTool("/tmp")
	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.InputSchema(), &schema))
	assert.Equal(t, "object", schema["type"])
}

var _ tools.Tool = (*SPMTool)(nil)
```

**Step 2-5:** Same rhythm: fail, implement `SPMTool` struct with mode (`build`/`test`/`resolve`/`add-dep`), each executing `swift build`, `swift test`, `swift package resolve`, or modifying Package.swift. Commit.

```bash
git add internal/tools/xcode/spm.go internal/tools/xcode/spm_test.go
git commit -m "[BEHAVIORAL] Add Swift Package Manager tools (swift_build, swift_test, swift_resolve, swift_add_dep)"
```

---

## PR 5: Code Signing + xcrun

### Task 7: Code signing tool

**Files:**
- Create: `internal/tools/xcode/codesign.go`
- Create: `internal/tools/xcode/codesign_test.go`

**Context:** `codesign_info` runs `security find-identity -v -p codesigning` for identities and `security cms -D -i <profile>` for provisioning profiles. `codesign_verify` runs `codesign -dv --verbose=4 <path>`. Both are darwin-only.

Tests follow the same pattern: platform gate, input validation, interface compliance.

```bash
git add internal/tools/xcode/codesign.go internal/tools/xcode/codesign_test.go
git commit -m "[BEHAVIORAL] Add code signing introspection tools"
```

### Task 8: xcrun dispatch tool

**Files:**
- Create: `internal/tools/xcode/xcrun.go`
- Create: `internal/tools/xcode/xcrun_test.go`

**Context:** Generic `xcrun` wrapper. Input: `{tool, args[]}`. Allowlisted tools: `instruments`, `strings`, `swift-demangle`, `sourcekit-lsp`, `simctl`. Rejects unknown tools to prevent arbitrary command execution.

```bash
git add internal/tools/xcode/xcrun.go internal/tools/xcode/xcrun_test.go
git commit -m "[BEHAVIORAL] Add xcrun dispatch tool with tool allowlist"
```

---

## PR 6: apple-dev Skill Assembly

### Task 9: Apple system prompt

**Files:**
- Create: `internal/skills/builtin/appledev/prompt.go`
- Create: `internal/skills/builtin/appledev/prompt_test.go`

**Context:** Embed `system.md` via `//go:embed`. Export `SystemPrompt() string`.

```go
// internal/skills/builtin/appledev/prompt.go
package appledev

import _ "embed"

//go:embed system.md
var systemPrompt string

// SystemPrompt returns the Apple platform system prompt for injection.
func SystemPrompt() string {
	return systemPrompt
}
```

Create `internal/skills/builtin/appledev/system.md` with content from spec section (Apple platform expertise: build system, signing, Swift concurrency, SwiftUI patterns). Keep under 5000 tokens.

```bash
git add internal/skills/builtin/appledev/
git commit -m "[BEHAVIORAL] Add apple-dev system prompt with embedded markdown"
```

### Task 10: Apple-dev skill backend

**Files:**
- Create: `internal/skills/builtin/appledev/backend.go`
- Create: `internal/skills/builtin/appledev/backend_test.go`

**Context:** Follow the `CoreToolsBackend` pattern from `internal/skills/builtin/core_tools.go`. The backend creates all xcode tools during `Load()`, filtered by platform.

```go
// Manifest returns the skill manifest for apple-dev.
func Manifest() skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "apple-dev",
		Version:     "1.0.0",
		Description: "Xcode CLI tools, Swift/iOS best practices, and Apple platform security scanning",
		Types:       []skills.SkillType{skills.SkillTypeTool, skills.SkillTypePrompt},
	}
}

// Backend implements skills.SkillBackend for apple-dev.
type Backend struct {
	WorkDir  string
	Platform xcode.PlatformChecker
	tools    []tools.Tool
}

func (b *Backend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	b.tools = append(b.tools, xcode.NewDiscoverTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftBuildTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftTestTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftResolveTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftAddDepTool(b.WorkDir))

	if b.Platform.IsDarwin() {
		b.tools = append(b.tools, xcode.NewXcodeBuildTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewXcodeTestTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewXcodeArchiveTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewXcodeCleanTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewSimListTool(b.Platform))
		// ... other darwin-only tools
	}
	return nil
}
```

```bash
git add internal/skills/builtin/appledev/backend.go internal/skills/builtin/appledev/backend_test.go
git commit -m "[BEHAVIORAL] Add apple-dev skill backend with platform-gated tool registration"
```

### Task 11: Wire apple-dev into main.go

**Files:**
- Modify: `cmd/rubichan/main.go`

**Context:** In both `runInteractive()` and `runHeadless()`:
1. Call `xcode.DiscoverProject(cwd)` to check for Apple projects
2. If found or `--skills=apple-dev`, register Xcode tools via `appledev.Backend`
3. Inject `appledev.SystemPrompt()` into agent options

Add `agent.WithExtraSystemPrompt(name, content string) AgentOption` to `internal/agent/agent.go` (similar to `WithAgentMD` but supports multiple named prompt sections).

```bash
git add cmd/rubichan/main.go internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Wire apple-dev skill into main with auto-activation"
```

---

## PR 7: Wiki-Security Integration

### Task 12: Pass security report to wiki assembler

**Files:**
- Modify: `internal/wiki/assembler.go`
- Modify: `internal/wiki/assembler_test.go`
- Modify: `cmd/rubichan/wiki.go`

**Step 1: Write the failing test**

```go
// In assembler_test.go, add:
func TestAssembleWithSecurityFindings(t *testing.T) {
	analysis := minimalAnalysis()
	findings := []security.Finding{
		{ID: "SEC-001", Scanner: "secrets", Severity: security.SeverityHigh,
			Title: "API key exposed", Location: security.Location{File: "config.go", StartLine: 10}},
		{ID: "SEC-002", Scanner: "sast", Severity: security.SeverityMedium,
			Title: "SQL injection", Location: security.Location{File: "db.go", StartLine: 5}},
	}

	docs, err := Assemble(analysis, nil, nil, findings)
	require.NoError(t, err)

	var secDoc *Document
	for i := range docs {
		if docs[i].Path == "security/overview.md" {
			secDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, secDoc)
	assert.Contains(t, secDoc.Content, "API key exposed")
	assert.Contains(t, secDoc.Content, "config.go:10")
	assert.Contains(t, secDoc.Content, "1 high")
	assert.NotContains(t, secDoc.Content, "pending")
}
```

**Step 2-3:** Change `Assemble` signature to accept `findings []security.Finding`. Change `buildSecurityPage()` to `buildSecurityPage(findings []security.Finding)`. When findings are non-empty, render a summary table + per-finding details instead of the placeholder.

Update `wiki.go` to create and run the security engine, passing findings to `Assemble`.

**Step 4-5:** Run tests, commit.

```bash
git add internal/wiki/assembler.go internal/wiki/assembler_test.go cmd/rubichan/wiki.go
git commit -m "[BEHAVIORAL] Wire security findings into wiki security page"
```

---

## PR 8: Skill Authoring Guide

### Task 13: Write skill authoring documentation

**Files:**
- Create: `docs/skill-authoring.md`

**Content outline:**
1. Quick Start — create a minimal prompt skill (SKILL.yaml + system prompt)
2. Skill Types — tool, prompt, workflow, security-rule, transform with examples
3. Starlark Guide — available builtins, SDK functions (`register_tool`, `llm_complete`, `register_hook`), sandbox constraints
4. Permissions — `file:read`, `shell:exec`, `net:fetch`, `env:read` explained, approval flow
5. Testing — local testing with `rubichan --skills=./my-skill/`
6. Publishing — registry, versioning, SemVer ranges, `rubichan skill publish`
7. Reference — link to `pkg/skillsdk/` godoc, spec.md section 4

Source material: `spec.md` sections 4.1-4.12, `pkg/skillsdk/sdk.go`, `examples/skills/ddd-expert/`.

```bash
git add docs/skill-authoring.md
git commit -m "[BEHAVIORAL] Add skill authoring guide"
```

---

## Verification Checklist

After all PRs merged:

- [ ] `go test ./... -cover` — all packages >90%
- [ ] `go build ./cmd/rubichan` — builds clean
- [ ] On macOS with Xcode: all 16+ tools registered, `xcode_discover` detects project
- [ ] On Linux (or with mock): only SPM tools + discover registered, clear "requires macOS" message
- [ ] Wiki command produces real security findings page (not "pending")
- [ ] `docs/skill-authoring.md` exists and covers all skill types
- [ ] Auto-activation: placing `.xcodeproj` in cwd triggers apple-dev tools
