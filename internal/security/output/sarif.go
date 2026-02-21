package output

import (
	"encoding/json"

	"github.com/julianshen/rubichan/internal/security"
)

// SARIF v2.1.0 structures.

type sarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Rules   []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string          `json:"id"`
	ShortDescription sarifMessageStr `json:"shortDescription"`
}

type sarifMessageStr struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessageStr `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

const sarifSchemaURL = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"

// SARIFFormatter formats a security report as SARIF v2.1.0 JSON.
type SARIFFormatter struct{}

// NewSARIFFormatter creates a new SARIFFormatter.
func NewSARIFFormatter() *SARIFFormatter {
	return &SARIFFormatter{}
}

// Name returns the formatter name.
func (f *SARIFFormatter) Name() string {
	return "sarif"
}

// Format renders the report as SARIF v2.1.0 JSON.
func (f *SARIFFormatter) Format(report *security.Report) ([]byte, error) {
	rules := buildRules(report.Findings)
	results := buildResults(report.Findings)

	doc := sarifDocument{
		Schema:  sarifSchemaURL,
		Version: "2.1.0",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:    "rubichan",
						Version: "0.1.0",
						Rules:   rules,
					},
				},
				Results: results,
			},
		},
	}

	return json.MarshalIndent(doc, "", "  ")
}

// severityToLevel maps security severity to SARIF level.
func severityToLevel(s security.Severity) string {
	switch s {
	case security.SeverityCritical, security.SeverityHigh:
		return "error"
	case security.SeverityMedium:
		return "warning"
	default:
		return "note"
	}
}

// buildRules creates unique SARIF rules from findings.
func buildRules(findings []security.Finding) []sarifRule {
	seen := make(map[string]bool)
	var rules []sarifRule

	for _, f := range findings {
		if f.CWE == "" || seen[f.CWE] {
			continue
		}
		seen[f.CWE] = true
		rules = append(rules, sarifRule{
			ID:               f.CWE,
			ShortDescription: sarifMessageStr{Text: f.Title},
		})
	}

	return rules
}

// buildResults creates SARIF results from findings.
func buildResults(findings []security.Finding) []sarifResult {
	results := make([]sarifResult, 0, len(findings))

	for _, f := range findings {
		result := sarifResult{
			RuleID:  f.CWE,
			Level:   severityToLevel(f.Severity),
			Message: sarifMessageStr{Text: f.Title},
			Locations: []sarifLocation{
				{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifactLocation{URI: f.Location.File},
						Region: sarifRegion{
							StartLine: f.Location.StartLine,
							EndLine:   f.Location.EndLine,
						},
					},
				},
			},
		}
		results = append(results, result)
	}

	return results
}
