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
	diagnosticRe  = regexp.MustCompile(`^(.+?):(\d+):(\d+): (error|warning): (.+)$`)
	testCaseRe    = regexp.MustCompile(`Test Case '-\[(\S+) (\S+)\]' (passed|failed) \((\d+\.\d+) seconds\)`)
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

// AllPassed returns true if all tests passed.
func (r TestResult) AllPassed() bool {
	return r.Failed == 0 && r.Total > 0
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
