package evaluator

import (
	"fmt"
	"strings"
)

// ErrorStatusChecker passes when IsError is false.
type ErrorStatusChecker struct{}

// NewErrorStatusChecker creates a new error status checker.
func NewErrorStatusChecker() *ErrorStatusChecker {
	return &ErrorStatusChecker{}
}

// Name returns the checker name.
func (c *ErrorStatusChecker) Name() string {
	return "error_status"
}

// Check evaluates whether a tool's IsError flag indicates success.
func (c *ErrorStatusChecker) Check(o ToolOutput) Evidence {
	if !o.IsError {
		return Evidence{
			CheckerName: c.Name(),
			Passed:      true,
			Severity:    "info",
			Finding:     "tool completed without error",
		}
	}
	return Evidence{
		CheckerName: c.Name(),
		Passed:      false,
		Severity:    "error",
		Finding:     "tool reported an error",
		Suggestion:  "review error content and retry with corrected parameters",
	}
}

// ErrorPatternChecker scans Content for common error keywords.
type ErrorPatternChecker struct {
	patterns []string
}

var defaultErrorPatterns = []string{
	"fatal error", "panic:", "segmentation fault",
	"permission denied", "command not found",
	"no such file or directory", "connection refused",
}

// NewErrorPatternChecker creates a new error pattern checker with default patterns.
func NewErrorPatternChecker() *ErrorPatternChecker {
	return &ErrorPatternChecker{patterns: defaultErrorPatterns}
}

// NewErrorPatternCheckerWithPatterns creates a new error pattern checker with custom patterns.
func NewErrorPatternCheckerWithPatterns(patterns []string) *ErrorPatternChecker {
	if len(patterns) == 0 {
		patterns = defaultErrorPatterns
	}
	return &ErrorPatternChecker{patterns: patterns}
}

// Name returns the checker name.
func (c *ErrorPatternChecker) Name() string {
	return "error_pattern"
}

// Check evaluates whether tool Content contains error keywords.
func (c *ErrorPatternChecker) Check(o ToolOutput) Evidence {
	lower := strings.ToLower(o.Content)
	for _, p := range c.patterns {
		if strings.Contains(lower, p) {
			return Evidence{
				CheckerName: c.Name(),
				Passed:      false,
				Severity:    "warning",
				Finding:     fmt.Sprintf("detected error pattern: %q", p),
				Suggestion:  "review output for the flagged pattern",
			}
		}
	}
	return Evidence{
		CheckerName: c.Name(),
		Passed:      true,
		Severity:    "info",
		Finding:     "no error patterns detected",
	}
}

// OutputSizeChecker warns when Content exceeds a size limit.
type OutputSizeChecker struct {
	maxBytes int
}

const defaultMaxOutputBytes = 1 << 20 // 1 MB

// NewOutputSizeChecker creates a new output size checker with the given limit.
// If maxBytes <= 0, uses the default limit of 1 MB.
func NewOutputSizeChecker(maxBytes int) *OutputSizeChecker {
	if maxBytes <= 0 {
		maxBytes = defaultMaxOutputBytes
	}
	return &OutputSizeChecker{maxBytes: maxBytes}
}

// Name returns the checker name.
func (c *OutputSizeChecker) Name() string {
	return "output_size"
}

// Check evaluates whether tool Content size is within limits.
func (c *OutputSizeChecker) Check(o ToolOutput) Evidence {
	n := len(o.Content)
	if n > c.maxBytes {
		return Evidence{
			CheckerName: c.Name(),
			Passed:      false,
			Severity:    "warning",
			Finding:     fmt.Sprintf("output size %d bytes exceeds limit %d", n, c.maxBytes),
			Suggestion:  "use pagination or filter to reduce output size",
		}
	}
	return Evidence{
		CheckerName: c.Name(),
		Passed:      true,
		Severity:    "info",
		Finding:     fmt.Sprintf("output size %d bytes within limit", n),
	}
}

// DefaultCheckerPipeline builds a pipeline with all built-in checkers.
func DefaultCheckerPipeline() *CheckerPipeline {
	return NewCheckerPipeline(
		NewErrorStatusChecker(),
		NewErrorPatternChecker(),
		NewOutputSizeChecker(defaultMaxOutputBytes),
	)
}
