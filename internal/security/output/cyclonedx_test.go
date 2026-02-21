package output

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCycloneDXFormatterName(t *testing.T) {
	f := NewCycloneDXFormatter()
	assert.Equal(t, "cyclonedx", f.Name())
}

func TestCycloneDXFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewCycloneDXFormatter()
}

func TestCycloneDXFormatterValidStructure(t *testing.T) {
	f := NewCycloneDXFormatter()

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
				CWE:        "CWE-89",
				Confidence: security.ConfidenceHigh,
			},
			{
				ID:          "F-2",
				Scanner:     "secrets",
				Severity:    security.SeverityMedium,
				Category:    security.CategorySecretsExposure,
				Title:       "Hardcoded Secret",
				Description: "API key in source code",
				Location: security.Location{
					File:      "config.go",
					StartLine: 5,
					EndLine:   5,
				},
				CWE:        "CWE-798",
				Confidence: security.ConfidenceMedium,
			},
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var bom cdxBOM
	err = json.Unmarshal(data, &bom)
	require.NoError(t, err)

	assert.Equal(t, "CycloneDX", bom.BOMFormat)
	assert.Equal(t, "1.5", bom.SpecVersion)
	assert.Equal(t, 1, bom.Version)

	// Metadata
	require.Len(t, bom.Metadata.Tools, 1)
	assert.Equal(t, "rubichan", bom.Metadata.Tools[0].Name)
	assert.Equal(t, "0.1.0", bom.Metadata.Tools[0].Version)

	// Vulnerabilities
	require.Len(t, bom.Vulnerabilities, 2)

	vuln1 := bom.Vulnerabilities[0]
	assert.Equal(t, "F-1", vuln1.ID)
	assert.Equal(t, "SQL Injection", vuln1.Description)
	assert.Equal(t, "rubichan", vuln1.Source.Name)
	require.Len(t, vuln1.Ratings, 1)
	assert.Equal(t, "high", vuln1.Ratings[0].Severity)
	assert.Equal(t, "other", vuln1.Ratings[0].Method)
	require.Len(t, vuln1.CWEs, 1)
	assert.Equal(t, 89, vuln1.CWEs[0])
	require.Len(t, vuln1.Affects, 1)
	assert.Equal(t, "db.go", vuln1.Affects[0].Ref)

	vuln2 := bom.Vulnerabilities[1]
	assert.Equal(t, "F-2", vuln2.ID)
	require.Len(t, vuln2.CWEs, 1)
	assert.Equal(t, 798, vuln2.CWEs[0])
}

func TestCycloneDXFormatterCWEParsing(t *testing.T) {
	f := NewCycloneDXFormatter()

	tests := []struct {
		name     string
		cwe      string
		expected []int
	}{
		{"valid CWE", "CWE-89", []int{89}},
		{"valid CWE three digits", "CWE-798", []int{798}},
		{"invalid CWE", "not-a-cwe", nil},
		{"empty CWE", "", nil},
		{"malformed CWE", "CWE-abc", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &security.Report{
				Findings: []security.Finding{
					{
						ID:         "F-1",
						Severity:   security.SeverityHigh,
						Category:   security.CategoryInjection,
						Title:      "Test",
						Location:   security.Location{File: "a.go", StartLine: 1},
						CWE:        tt.cwe,
						Confidence: security.ConfidenceMedium,
					},
				},
			}

			data, err := f.Format(report)
			require.NoError(t, err)

			var bom cdxBOM
			err = json.Unmarshal(data, &bom)
			require.NoError(t, err)

			require.Len(t, bom.Vulnerabilities, 1)
			if tt.expected == nil {
				assert.Empty(t, bom.Vulnerabilities[0].CWEs)
			} else {
				assert.Equal(t, tt.expected, bom.Vulnerabilities[0].CWEs)
			}
		})
	}
}

func TestCycloneDXFormatterEmptyReport(t *testing.T) {
	f := NewCycloneDXFormatter()
	report := &security.Report{}

	data, err := f.Format(report)
	require.NoError(t, err)

	var bom cdxBOM
	err = json.Unmarshal(data, &bom)
	require.NoError(t, err)

	assert.Equal(t, "CycloneDX", bom.BOMFormat)
	assert.Equal(t, "1.5", bom.SpecVersion)
	assert.Empty(t, bom.Vulnerabilities)
}
