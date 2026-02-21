package output

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubPRFormatterName(t *testing.T) {
	f := NewGitHubPRFormatter()
	assert.Equal(t, "github-pr", f.Name())
}

func TestGitHubPRFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewGitHubPRFormatter()
}

func TestGitHubPRFormatterCreatesComments(t *testing.T) {
	f := NewGitHubPRFormatter()

	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:          "F-1",
				Scanner:     "sast",
				Severity:    security.SeverityCritical,
				Category:    security.CategoryInjection,
				Title:       "SQL Injection",
				Description: "User input used in SQL query without sanitization",
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
				ID:          "F-2",
				Scanner:     "secrets",
				Severity:    security.SeverityHigh,
				Category:    security.CategorySecretsExposure,
				Title:       "Hardcoded API Key",
				Description: "API key found in source code",
				Location: security.Location{
					File:      "config.go",
					StartLine: 5,
					EndLine:   5,
					Function:  "Init",
				},
				CWE:         "CWE-798",
				Remediation: "Move secrets to environment variables",
				Confidence:  security.ConfidenceMedium,
			},
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var review PRReview
	err = json.Unmarshal(data, &review)
	require.NoError(t, err)

	require.Len(t, review.Comments, 2)

	// First comment
	c1 := review.Comments[0]
	assert.Equal(t, "db.go", c1.Path)
	assert.Equal(t, 10, c1.Line)
	assert.Equal(t, "critical", c1.Severity)
	assert.Contains(t, c1.Body, "SQL Injection")
	assert.Contains(t, c1.Body, "Use parameterized queries")

	// Second comment
	c2 := review.Comments[1]
	assert.Equal(t, "config.go", c2.Path)
	assert.Equal(t, 5, c2.Line)
	assert.Equal(t, "high", c2.Severity)
}

func TestGitHubPRFormatterSummaryBody(t *testing.T) {
	f := NewGitHubPRFormatter()

	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:         "F-1",
				Severity:   security.SeverityCritical,
				Category:   security.CategoryInjection,
				Title:      "Critical Issue",
				Location:   security.Location{File: "a.go", StartLine: 1},
				Confidence: security.ConfidenceHigh,
			},
			{
				ID:         "F-2",
				Severity:   security.SeverityHigh,
				Category:   security.CategorySecretsExposure,
				Title:      "High Issue",
				Location:   security.Location{File: "b.go", StartLine: 1},
				Confidence: security.ConfidenceMedium,
			},
			{
				ID:         "F-3",
				Severity:   security.SeverityLow,
				Category:   security.CategoryMisconfiguration,
				Title:      "Low Issue",
				Location:   security.Location{File: "c.go", StartLine: 1},
				Confidence: security.ConfidenceLow,
			},
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var review PRReview
	err = json.Unmarshal(data, &review)
	require.NoError(t, err)

	assert.Contains(t, review.Body, "3 findings")
	assert.Contains(t, review.Body, "critical | 1")
	assert.Contains(t, review.Body, "high | 1")
}

func TestGitHubPRFormatterEmptyReport(t *testing.T) {
	f := NewGitHubPRFormatter()
	report := &security.Report{}

	data, err := f.Format(report)
	require.NoError(t, err)

	var review PRReview
	err = json.Unmarshal(data, &review)
	require.NoError(t, err)

	assert.Empty(t, review.Comments)
	assert.Contains(t, review.Body, "0 findings")
}
