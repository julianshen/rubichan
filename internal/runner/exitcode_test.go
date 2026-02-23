package runner

import (
	"testing"

	"github.com/julianshen/rubichan/internal/output"
	"github.com/stretchr/testify/assert"
)

func TestExitCodeFromFindings_NoFindings(t *testing.T) {
	code := ExitCodeFromFindings(nil, "high")
	assert.Equal(t, 0, code)
}

func TestExitCodeFromFindings_BelowThreshold(t *testing.T) {
	findings := []output.SecurityFinding{
		{Severity: "low", Title: "minor issue"},
		{Severity: "info", Title: "informational"},
	}
	code := ExitCodeFromFindings(findings, "high")
	assert.Equal(t, 0, code)
}

func TestExitCodeFromFindings_AtThreshold(t *testing.T) {
	findings := []output.SecurityFinding{
		{Severity: "high", Title: "serious issue"},
	}
	code := ExitCodeFromFindings(findings, "high")
	assert.Equal(t, 1, code)
}

func TestExitCodeFromFindings_AboveThreshold(t *testing.T) {
	findings := []output.SecurityFinding{
		{Severity: "critical", Title: "critical issue"},
	}
	code := ExitCodeFromFindings(findings, "high")
	assert.Equal(t, 1, code)
}

func TestExitCodeFromFindings_MixedSeverities(t *testing.T) {
	findings := []output.SecurityFinding{
		{Severity: "low", Title: "minor"},
		{Severity: "medium", Title: "moderate"},
		{Severity: "high", Title: "serious"},
	}
	code := ExitCodeFromFindings(findings, "critical")
	assert.Equal(t, 0, code, "no critical findings should mean exit 0")
}

func TestExitCodeFromFindings_CriticalThreshold(t *testing.T) {
	findings := []output.SecurityFinding{
		{Severity: "critical", Title: "critical bug"},
	}
	code := ExitCodeFromFindings(findings, "critical")
	assert.Equal(t, 1, code)
}

func TestExitCodeFromFindings_EmptyFailOn(t *testing.T) {
	findings := []output.SecurityFinding{
		{Severity: "info", Title: "just info"},
	}
	// Empty failOn should default to no gating (exit 0).
	code := ExitCodeFromFindings(findings, "")
	assert.Equal(t, 0, code)
}
