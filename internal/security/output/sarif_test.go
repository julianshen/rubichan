package output

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSARIFFormatterName(t *testing.T) {
	f := NewSARIFFormatter()
	assert.Equal(t, "sarif", f.Name())
}

func TestSARIFFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewSARIFFormatter()
}

func TestSARIFFormatterValidStructure(t *testing.T) {
	f := NewSARIFFormatter()

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
		},
	}

	data, err := f.Format(report)
	require.NoError(t, err)

	var sarif sarifDocument
	err = json.Unmarshal(data, &sarif)
	require.NoError(t, err)

	assert.Equal(t, "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json", sarif.Schema)
	assert.Equal(t, "2.1.0", sarif.Version)
	require.Len(t, sarif.Runs, 1)

	run := sarif.Runs[0]
	assert.Equal(t, "rubichan", run.Tool.Driver.Name)
	assert.Equal(t, "0.1.0", run.Tool.Driver.Version)

	// Check rules
	require.Len(t, run.Tool.Driver.Rules, 1)
	assert.Equal(t, "CWE-89", run.Tool.Driver.Rules[0].ID)

	// Check results
	require.Len(t, run.Results, 1)
	result := run.Results[0]
	assert.Equal(t, "CWE-89", result.RuleID)
	assert.Equal(t, "error", result.Level)
	assert.Equal(t, "SQL Injection", result.Message.Text)

	require.Len(t, result.Locations, 1)
	loc := result.Locations[0]
	assert.Equal(t, "db.go", loc.PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 10, loc.PhysicalLocation.Region.StartLine)
	assert.Equal(t, 20, loc.PhysicalLocation.Region.EndLine)
}

func TestSARIFFormatterSeverityMapping(t *testing.T) {
	f := NewSARIFFormatter()

	tests := []struct {
		severity security.Severity
		expected string
	}{
		{security.SeverityCritical, "error"},
		{security.SeverityHigh, "error"},
		{security.SeverityMedium, "warning"},
		{security.SeverityLow, "note"},
		{security.SeverityInfo, "note"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			report := &security.Report{
				Findings: []security.Finding{
					{
						ID:         "F-1",
						Severity:   tt.severity,
						Category:   security.CategoryInjection,
						Title:      "Test",
						Location:   security.Location{File: "a.go", StartLine: 1, EndLine: 1},
						CWE:        "CWE-1",
						Confidence: security.ConfidenceMedium,
					},
				},
			}

			data, err := f.Format(report)
			require.NoError(t, err)

			var sarif sarifDocument
			err = json.Unmarshal(data, &sarif)
			require.NoError(t, err)

			require.Len(t, sarif.Runs[0].Results, 1)
			assert.Equal(t, tt.expected, sarif.Runs[0].Results[0].Level)
		})
	}
}

func TestSARIFFormatterEmptyReport(t *testing.T) {
	f := NewSARIFFormatter()
	report := &security.Report{}

	data, err := f.Format(report)
	require.NoError(t, err)

	var sarif sarifDocument
	err = json.Unmarshal(data, &sarif)
	require.NoError(t, err)

	assert.Equal(t, "2.1.0", sarif.Version)
	require.Len(t, sarif.Runs, 1)
	assert.Empty(t, sarif.Runs[0].Results)
	assert.Empty(t, sarif.Runs[0].Tool.Driver.Rules)
}
