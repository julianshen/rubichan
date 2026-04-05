package evaluator_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/evaluator"
	"github.com/stretchr/testify/assert"
)

func TestVerdictPipelineAllPassed(t *testing.T) {
	t.Parallel()
	c1 := &mockChecker{name: "c1", pass: true}
	c2 := &mockChecker{name: "c2", pass: true}
	pipeline := evaluator.NewCheckerPipeline(c1, c2)

	v := pipeline.Evaluate(evaluator.ToolOutput{ToolName: "test", Content: "output", IsError: false})

	assert.Equal(t, evaluator.VerdictSuccess, v.Status)
	assert.GreaterOrEqual(t, v.Confidence, 0.9)
	assert.Contains(t, v.Reason, "passed")
}

func TestVerdictPipelineOneError(t *testing.T) {
	t.Parallel()
	c1 := &mockChecker{name: "c1", pass: true}
	c2 := &mockCheckerWithSuggestion{
		mockChecker: mockChecker{name: "c2", pass: false},
		suggestion:  "try again",
	}
	pipeline := evaluator.NewCheckerPipeline(c1, c2)

	v := pipeline.Evaluate(evaluator.ToolOutput{ToolName: "test", Content: "output", IsError: false})

	assert.Equal(t, evaluator.VerdictFailed, v.Status)
	assert.GreaterOrEqual(t, v.Confidence, 0.8)
	assert.Contains(t, v.Suggestions, "try again")
}

func TestVerdictPipelineMultipleWarnings(t *testing.T) {
	t.Parallel()
	c1 := &mockCheckerWithSeverity{
		mockChecker: mockChecker{name: "c1", pass: false},
		severity:    evaluator.SeverityWarning,
		suggestion:  "fix1",
	}
	c2 := &mockCheckerWithSeverity{
		mockChecker: mockChecker{name: "c2", pass: false},
		severity:    evaluator.SeverityWarning,
		suggestion:  "fix2",
	}
	pipeline := evaluator.NewCheckerPipeline(c1, c2)

	v := pipeline.Evaluate(evaluator.ToolOutput{ToolName: "test", Content: "output", IsError: false})

	assert.Equal(t, evaluator.VerdictEscalate, v.Status)
	assert.LessOrEqual(t, v.Confidence, 0.7)
	assert.Len(t, v.Suggestions, 2)
}

func TestFormatVerdictSuccessFormat(t *testing.T) {
	t.Parallel()
	v := evaluator.Verdict{
		Status:      evaluator.VerdictSuccess,
		Confidence:  0.95,
		Reason:      "all good",
		Evidence:    []evaluator.Evidence{},
		Suggestions: []string{},
	}
	formatted := evaluator.FormatVerdict(v)
	assert.Contains(t, formatted, "[evaluation]")
	assert.Contains(t, formatted, "success")
	assert.Contains(t, formatted, "95%")
}

func TestFormatVerdictFailedIncludesFindings(t *testing.T) {
	t.Parallel()
	v := evaluator.Verdict{
		Status:     evaluator.VerdictFailed,
		Confidence: 0.85,
		Reason:     "check failed",
		Evidence: []evaluator.Evidence{
			{CheckerName: "c1", Passed: false, Severity: evaluator.SeverityError, Finding: "error text"},
		},
		Suggestions: []string{"retry"},
	}
	formatted := evaluator.FormatVerdict(v)
	assert.Contains(t, formatted, "failed")
	assert.Contains(t, formatted, "error text")
	assert.Contains(t, formatted, "retry")
}

// Mock checkers for testing
type mockChecker struct {
	name string
	pass bool
}

func (m *mockChecker) Name() string { return m.name }
func (m *mockChecker) Check(output evaluator.ToolOutput) evaluator.Evidence {
	return evaluator.Evidence{
		CheckerName: m.name,
		Passed:      m.pass,
		Severity:    evaluator.SeverityInfo,
	}
}

type mockCheckerWithSuggestion struct {
	mockChecker
	suggestion string
}

func (m *mockCheckerWithSuggestion) Check(output evaluator.ToolOutput) evaluator.Evidence {
	return evaluator.Evidence{
		CheckerName: m.name,
		Passed:      m.pass,
		Severity:    evaluator.SeverityError,
		Finding:     "error found",
		Suggestion:  m.suggestion,
	}
}

type mockCheckerWithSeverity struct {
	mockChecker
	severity   evaluator.SeverityLevel
	suggestion string
}

func (m *mockCheckerWithSeverity) Check(output evaluator.ToolOutput) evaluator.Evidence {
	return evaluator.Evidence{
		CheckerName: m.name,
		Passed:      m.pass,
		Severity:    m.severity,
		Finding:     "finding " + m.name,
		Suggestion:  m.suggestion,
	}
}
