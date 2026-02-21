package output

import (
	"strings"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownFormatterName(t *testing.T) {
	f := NewMarkdownFormatter()
	assert.Equal(t, "markdown", f.Name())
}

func TestMarkdownFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewMarkdownFormatter()
}

func TestMarkdownFormatterOutput(t *testing.T) {
	f := NewMarkdownFormatter()

	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:          "F-1",
				Scanner:     "sast",
				Severity:    security.SeverityCritical,
				Category:    security.CategoryInjection,
				Title:       "SQL Injection",
				Description: "User input in SQL query",
				Location: security.Location{
					File:      "db.go",
					StartLine: 10,
					EndLine:   20,
					Function:  "Query",
				},
				CWE:         "CWE-89",
				Remediation: "Use parameterized queries",
				Confidence:  security.ConfidenceHigh,
			},
			{
				ID:       "F-2",
				Scanner:  "secrets",
				Severity: security.SeverityHigh,
				Category: security.CategorySecretsExposure,
				Title:    "Hardcoded API Key",
				Location: security.Location{
					File:      "config.go",
					StartLine: 5,
					EndLine:   5,
					Function:  "Init",
				},
				CWE:        "CWE-798",
				Confidence: security.ConfidenceMedium,
			},
		},
		AttackChains: []security.AttackChain{
			{
				ID:       "C-1",
				Title:    "Unauthenticated Injection",
				Severity: security.SeverityCritical,
				Steps: []security.Finding{
					{ID: "F-1", Title: "SQL Injection", Location: security.Location{File: "db.go", StartLine: 10}},
				},
				Impact: "Remote code execution",
			},
		},
		Stats: security.ScanStats{
			Duration:       500 * time.Millisecond,
			FilesScanned:   42,
			ChunksAnalyzed: 10,
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)
	out := string(data)

	// Check header
	assert.Contains(t, out, "# Security Scan Report")

	// Check summary table
	assert.Contains(t, out, "## Summary")
	assert.Contains(t, out, "| Severity | Count |")
	assert.Contains(t, out, "| Critical | 1 |")
	assert.Contains(t, out, "| High | 1 |")

	// Check finding sections
	assert.Contains(t, out, "## Critical Findings")
	assert.Contains(t, out, "### [F-1] SQL Injection")
	assert.Contains(t, out, "## High Findings")
	assert.Contains(t, out, "### [F-2] Hardcoded API Key")

	// Check location format
	assert.Contains(t, out, "db.go:10-20 (Query)")

	// Check attack chains section
	assert.Contains(t, out, "## Attack Chains")
	assert.Contains(t, out, "### [C-1] Unauthenticated Injection")
}

func TestMarkdownFormatterEmptyReport(t *testing.T) {
	f := NewMarkdownFormatter()
	report := &security.Report{}

	data, err := f.Format(report)
	require.NoError(t, err)
	out := string(data)

	assert.Contains(t, out, "# Security Scan Report")
	assert.Contains(t, out, "## Summary")
	assert.Contains(t, out, "**Total findings:** 0")
}

func TestMarkdownFormatterGroupsBySeverity(t *testing.T) {
	f := NewMarkdownFormatter()

	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:         "F-1",
				Severity:   security.SeverityMedium,
				Category:   security.CategoryMisconfiguration,
				Title:      "Medium Finding",
				Location:   security.Location{File: "a.go", StartLine: 1, EndLine: 1},
				Confidence: security.ConfidenceMedium,
			},
			{
				ID:         "F-2",
				Severity:   security.SeverityCritical,
				Category:   security.CategoryInjection,
				Title:      "Critical Finding",
				Location:   security.Location{File: "b.go", StartLine: 1, EndLine: 1},
				Confidence: security.ConfidenceHigh,
			},
			{
				ID:         "F-3",
				Severity:   security.SeverityHigh,
				Category:   security.CategorySecretsExposure,
				Title:      "High Finding",
				Location:   security.Location{File: "c.go", StartLine: 1, EndLine: 1},
				Confidence: security.ConfidenceHigh,
			},
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)
	out := string(data)

	// Critical should appear before High, which should appear before Medium
	critIdx := strings.Index(out, "## Critical Findings")
	highIdx := strings.Index(out, "## High Findings")
	medIdx := strings.Index(out, "## Medium Findings")

	assert.Greater(t, highIdx, critIdx, "High should come after Critical")
	assert.Greater(t, medIdx, highIdx, "Medium should come after High")

	// No Low or Info sections
	assert.NotContains(t, out, "## Low Findings")
	assert.NotContains(t, out, "## Info Findings")
}
