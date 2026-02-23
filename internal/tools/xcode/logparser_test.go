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

func TestParseBuildLog_StoresRawLog(t *testing.T) {
	log := "some build output"
	result := ParseBuildLog(log)
	assert.Equal(t, log, result.RawLog)
}

func TestParseBuildLog_ColumnParsed(t *testing.T) {
	log := `/path/file.swift:5:12: error: missing return`
	result := ParseBuildLog(log)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, 12, result.Errors[0].Column)
	assert.Equal(t, "missing return", result.Errors[0].Message)
}

func TestParseTestLog_EmptyInput(t *testing.T) {
	result := ParseTestLog("")
	assert.Equal(t, 0, result.Total)
	assert.Empty(t, result.Cases)
}

func TestParseTestLog_CaseDetails(t *testing.T) {
	log := `Test Case '-[AppTests.LoginTest testValidCredentials]' passed (0.123 seconds).`
	result := ParseTestLog(log)
	require.Len(t, result.Cases, 1)
	assert.Equal(t, "AppTests.LoginTest", result.Cases[0].Suite)
	assert.Equal(t, "testValidCredentials", result.Cases[0].Name)
	assert.True(t, result.Cases[0].Passed)
	assert.InDelta(t, 0.123, result.Cases[0].Duration, 0.0001)
}

func TestTestResult_AllPassed(t *testing.T) {
	passing := TestResult{Total: 3, Passed: 3, Failed: 0}
	assert.True(t, passing.AllPassed())

	failing := TestResult{Total: 3, Passed: 2, Failed: 1}
	assert.False(t, failing.AllPassed())

	empty := TestResult{Total: 0, Passed: 0, Failed: 0}
	assert.False(t, empty.AllPassed())
}

func TestFormatBuildResult_Success(t *testing.T) {
	r := BuildResult{Success: true}
	out := FormatBuildResult(r)
	assert.Contains(t, out, "Build succeeded.")
}

func TestFormatBuildResult_WithDiagnostics(t *testing.T) {
	r := BuildResult{
		Success: false,
		Errors: []BuildDiagnostic{
			{File: "main.swift", Line: 10, Column: 5, Message: "type mismatch"},
		},
		Warnings: []BuildDiagnostic{
			{File: "util.swift", Line: 3, Column: 1, Message: "unused variable"},
		},
	}
	out := FormatBuildResult(r)
	assert.Contains(t, out, "Build failed.")
	assert.Contains(t, out, "ERROR main.swift:10:5: type mismatch")
	assert.Contains(t, out, "WARNING util.swift:3:1: unused variable")
}

func TestFormatTestResult(t *testing.T) {
	r := TestResult{
		Total:  2,
		Passed: 1,
		Failed: 1,
		Cases: []TestCase{
			{Suite: "AppTests", Name: "testFoo", Passed: true, Duration: 0.001},
			{Suite: "AppTests", Name: "testBar", Passed: false, Duration: 0.002},
		},
	}
	out := FormatTestResult(r)
	assert.Contains(t, out, "Tests: 2 total, 1 passed, 1 failed")
	assert.Contains(t, out, "[PASS] AppTests.testFoo")
	assert.Contains(t, out, "[FAIL] AppTests.testBar")
}
