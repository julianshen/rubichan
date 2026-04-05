package evaluator_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/evaluator"
	"github.com/stretchr/testify/assert"
)

// Interface compliance
func TestCheckerInterfaces(t *testing.T) {
	t.Parallel()
	var _ evaluator.Checker = evaluator.NewErrorStatusChecker()
	var _ evaluator.Checker = evaluator.NewErrorPatternChecker()
	var _ evaluator.Checker = evaluator.NewOutputSizeChecker(1024)
}

// ErrorStatusChecker tests
func TestErrorStatusCheckerPassesWhenNoError(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorStatusChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "success output",
		IsError:  false,
	})

	assert.True(t, ev.Passed)
	assert.Equal(t, evaluator.SeverityInfo, ev.Severity)
	assert.Contains(t, ev.Finding, "without error")
}

func TestErrorStatusCheckerFailsWhenError(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorStatusChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "error output",
		IsError:  true,
	})

	assert.False(t, ev.Passed)
	assert.Equal(t, evaluator.SeverityError, ev.Severity)
	assert.NotEmpty(t, ev.Suggestion)
}

// ErrorPatternChecker tests
func TestErrorPatternCheckerPassesWhenNoPatterns(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorPatternChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "normal output without any issues",
		IsError:  false,
	})

	assert.True(t, ev.Passed)
	assert.Equal(t, evaluator.SeverityInfo, ev.Severity)
}

func TestErrorPatternCheckerDetectsFatalError(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorPatternChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "fatal error: something went wrong",
		IsError:  false, // Note: IsError might be false even with error keywords
	})

	assert.False(t, ev.Passed)
	assert.Equal(t, evaluator.SeverityWarning, ev.Severity)
	assert.Contains(t, ev.Finding, "fatal error")
}

func TestErrorPatternCheckerDetectsPanic(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorPatternChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "go",
		Content:  "panic: runtime error",
		IsError:  false,
	})

	assert.False(t, ev.Passed)
	assert.Contains(t, ev.Finding, "panic")
}

func TestErrorPatternCheckerDetectsPermissionDenied(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorPatternChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "permission denied: cannot write to /root",
		IsError:  true,
	})

	assert.False(t, ev.Passed)
	assert.Contains(t, ev.Finding, "permission denied")
}

func TestErrorPatternCheckerCaseInsensitive(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorPatternChecker()
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "FATAL ERROR: uppercase error",
		IsError:  false,
	})

	assert.False(t, ev.Passed)
	assert.Contains(t, ev.Finding, "fatal error")
}

func TestErrorPatternCheckerWithCustomPatterns(t *testing.T) {
	t.Parallel()
	c := evaluator.NewErrorPatternCheckerWithPatterns([]string{"oops", "uh-oh"})
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "oops, something went wrong",
		IsError:  false,
	})

	assert.False(t, ev.Passed)
	assert.Contains(t, ev.Finding, "oops")
}

// OutputSizeChecker tests
func TestOutputSizeCheckerPassesWhenSmall(t *testing.T) {
	t.Parallel()
	c := evaluator.NewOutputSizeChecker(1024)
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "small output",
		IsError:  false,
	})

	assert.True(t, ev.Passed)
	assert.Equal(t, evaluator.SeverityInfo, ev.Severity)
}

func TestOutputSizeCheckerFailsWhenLarge(t *testing.T) {
	t.Parallel()
	c := evaluator.NewOutputSizeChecker(100) // Limit to 100 bytes
	largeOutput := string(make([]byte, 200)) // 200 byte output
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  largeOutput,
		IsError:  false,
	})

	assert.False(t, ev.Passed)
	assert.Equal(t, evaluator.SeverityWarning, ev.Severity)
	assert.Contains(t, ev.Finding, "exceeds limit")
	assert.NotEmpty(t, ev.Suggestion)
}

func TestOutputSizeCheckerUsesDefaultLimit(t *testing.T) {
	t.Parallel()
	c := evaluator.NewOutputSizeChecker(0) // Should use default 1MB
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "small output",
		IsError:  false,
	})

	assert.True(t, ev.Passed)
}

func TestOutputSizeCheckerNegativeLimitUsesDefault(t *testing.T) {
	t.Parallel()
	c := evaluator.NewOutputSizeChecker(-1) // Should use default 1MB
	ev := c.Check(evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "small output",
		IsError:  false,
	})

	assert.True(t, ev.Passed)
}

// DefaultCheckerPipeline tests
func TestDefaultCheckerPipelineContainsAllCheckers(t *testing.T) {
	t.Parallel()
	pipeline := evaluator.DefaultCheckerPipeline()
	output := evaluator.ToolOutput{
		ToolName: "shell",
		Content:  "success",
		IsError:  false,
	}
	v := pipeline.Evaluate(output)

	assert.Greater(t, len(v.Evidence), 0, "Should have evidence from multiple checkers")
	// Should have at least 3 checkers
	assert.GreaterOrEqual(t, len(v.Evidence), 3)
}
