package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFormatterName(t *testing.T) {
	f := NewJSONFormatter()
	assert.Equal(t, "json", f.Name())
}

func TestJSONFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewJSONFormatter()
}

func TestJSONFormatterOutput(t *testing.T) {
	f := NewJSONFormatter()

	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:          "F-1",
				Scanner:     "sast",
				Severity:    security.SeverityHigh,
				Category:    security.CategoryInjection,
				Title:       "SQL Injection",
				Description: "User input used in SQL query",
				Location:    security.Location{File: "db.go", StartLine: 10, EndLine: 20, Function: "Query"},
				CWE:         "CWE-89",
				OWASP:       "A03:2021",
				Evidence:    "db.Query(userInput)",
				Remediation: "Use parameterized queries",
				Confidence:  security.ConfidenceHigh,
				References:  []string{"https://cwe.mitre.org/data/definitions/89.html"},
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
			FindingsCount:  1,
			ChainCount:     1,
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var result map[string]json.RawMessage
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Contains(t, result, "findings")
	assert.Contains(t, result, "attack_chains")
	assert.Contains(t, result, "summary")
	assert.Contains(t, result, "stats")

	// Verify summary values
	var summary map[string]int
	err = json.Unmarshal(result["summary"], &summary)
	require.NoError(t, err)
	assert.Equal(t, 1, summary["high"])
	assert.Equal(t, 1, summary["chains"])
	assert.Equal(t, 1, summary["total"])
}

func TestJSONFormatterEmptyReport(t *testing.T) {
	f := NewJSONFormatter()
	report := &security.Report{}

	data, err := f.Format(report)
	require.NoError(t, err)

	var result map[string]json.RawMessage
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Contains(t, result, "findings")
	assert.Contains(t, result, "summary")
}

func TestJSONFormatterPreservesAllFields(t *testing.T) {
	f := NewJSONFormatter()

	original := security.Finding{
		ID:          "F-42",
		Scanner:     "test-scanner",
		Severity:    security.SeverityCritical,
		Category:    security.CategoryCryptography,
		Title:       "Weak Encryption",
		Description: "AES-ECB used for encryption",
		Location:    security.Location{File: "crypto.go", StartLine: 5, EndLine: 15, Function: "Encrypt"},
		CWE:         "CWE-327",
		OWASP:       "A02:2021",
		Evidence:    "aes.NewCipher(key)",
		Remediation: "Use AES-GCM instead",
		Confidence:  security.ConfidenceHigh,
		References:  []string{"https://example.com"},
		Metadata:    map[string]string{"source": "test"},
		SkillSource: "crypto-skill",
	}

	report := &security.Report{
		Findings: []security.Finding{original},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var result jsonReport
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	require.Len(t, result.Findings, 1)
	rf := result.Findings[0]
	assert.Equal(t, "F-42", rf.ID)
	assert.Equal(t, "test-scanner", rf.Scanner)
	assert.Equal(t, "critical", rf.Severity)
	assert.Equal(t, "cryptography", rf.Category)
	assert.Equal(t, "Weak Encryption", rf.Title)
	assert.Equal(t, "AES-ECB used for encryption", rf.Description)
	assert.Equal(t, "crypto.go", rf.Location.File)
	assert.Equal(t, 5, rf.Location.StartLine)
	assert.Equal(t, 15, rf.Location.EndLine)
	assert.Equal(t, "Encrypt", rf.Location.Function)
	assert.Equal(t, "CWE-327", rf.CWE)
	assert.Equal(t, "A02:2021", rf.OWASP)
	assert.Equal(t, "aes.NewCipher(key)", rf.Evidence)
	assert.Equal(t, "Use AES-GCM instead", rf.Remediation)
	assert.Equal(t, "high", rf.Confidence)
	assert.Equal(t, []string{"https://example.com"}, rf.References)
	assert.Equal(t, "test", rf.Metadata["source"])
	assert.Equal(t, "crypto-skill", rf.SkillSource)
}
