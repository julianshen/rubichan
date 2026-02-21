package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiFormatterName(t *testing.T) {
	f := NewWikiFormatter()
	assert.Equal(t, "wiki", f.Name())
}

func TestWikiFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewWikiFormatter()
}

func TestWikiFormatterOutput(t *testing.T) {
	f := NewWikiFormatter()

	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:          "F-1",
				Scanner:     "sast",
				Severity:    security.SeverityHigh,
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
				ID:          "F-2",
				Scanner:     "secrets",
				Severity:    security.SeverityMedium,
				Category:    security.CategorySecretsExposure,
				Title:       "Hardcoded Secret",
				Description: "API key in source",
				Location: security.Location{
					File:      "config.go",
					StartLine: 5,
					EndLine:   5,
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
					{ID: "F-1", Title: "SQL Injection"},
				},
				Impact:     "Remote code execution",
				Likelihood: "high",
			},
		},
		Stats: security.ScanStats{
			Duration:       500 * time.Millisecond,
			FilesScanned:   42,
			ChunksAnalyzed: 10,
			FindingsCount:  2,
			ChainCount:     1,
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var wiki WikiOutput
	err = json.Unmarshal(data, &wiki)
	require.NoError(t, err)

	// Overview page
	assert.Contains(t, wiki.Overview, "Security Overview")
	assert.Contains(t, wiki.Overview, "42")  // files scanned
	assert.Contains(t, wiki.Overview, "500") // duration ms

	// Findings page
	assert.Contains(t, wiki.Findings, "SQL Injection")
	assert.Contains(t, wiki.Findings, "Hardcoded Secret")
	assert.Contains(t, wiki.Findings, "injection")
	assert.Contains(t, wiki.Findings, "secrets-exposure")

	// Attack chains page
	assert.Contains(t, wiki.AttackChains, "Unauthenticated Injection")
	assert.Contains(t, wiki.AttackChains, "Remote code execution")
}

func TestWikiFormatterEmptyReport(t *testing.T) {
	f := NewWikiFormatter()
	report := &security.Report{}

	data, err := f.Format(report)
	require.NoError(t, err)

	var wiki WikiOutput
	err = json.Unmarshal(data, &wiki)
	require.NoError(t, err)

	// All pages should still have some content
	assert.Contains(t, wiki.Overview, "Security Overview")
	assert.Contains(t, wiki.Findings, "Findings")
	assert.Contains(t, wiki.AttackChains, "Attack Chains")
}
