package output

import (
	"encoding/json"

	"github.com/julianshen/rubichan/internal/security"
)

// jsonReport is the top-level JSON output structure.
type jsonReport struct {
	Findings     []jsonFinding     `json:"findings"`
	AttackChains []jsonAttackChain `json:"attack_chains"`
	Summary      jsonSummary       `json:"summary"`
	Stats        jsonStats         `json:"stats"`
}

// jsonFinding mirrors security.Finding with JSON-friendly serialization.
type jsonFinding struct {
	ID          string            `json:"id"`
	Scanner     string            `json:"scanner"`
	Severity    string            `json:"severity"`
	Category    string            `json:"category"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Location    jsonLocation      `json:"location"`
	CWE         string            `json:"cwe"`
	OWASP       string            `json:"owasp"`
	Evidence    string            `json:"evidence"`
	Remediation string            `json:"remediation"`
	Confidence  string            `json:"confidence"`
	References  []string          `json:"references"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	SkillSource string            `json:"skill_source,omitempty"`
}

// jsonLocation mirrors security.Location with JSON-friendly serialization.
type jsonLocation struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Function  string `json:"function"`
}

// jsonAttackChain mirrors security.AttackChain with JSON-friendly serialization.
type jsonAttackChain struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Severity   string        `json:"severity"`
	Steps      []jsonFinding `json:"steps"`
	Impact     string        `json:"impact"`
	Likelihood string        `json:"likelihood"`
}

// jsonSummary holds aggregate counts by severity.
type jsonSummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
	Chains   int `json:"chains"`
	Total    int `json:"total"`
}

// jsonStats holds scan timing and count metrics.
type jsonStats struct {
	DurationMS     int64 `json:"duration_ms"`
	FilesScanned   int   `json:"files_scanned"`
	ChunksAnalyzed int   `json:"chunks_analyzed"`
}

// JSONFormatter formats a security report as JSON.
type JSONFormatter struct{}

// NewJSONFormatter creates a new JSONFormatter.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

// Name returns the formatter name.
func (f *JSONFormatter) Name() string {
	return "json"
}

// Format renders the report as indented JSON.
func (f *JSONFormatter) Format(report *security.Report) ([]byte, error) {
	jr := jsonReport{
		Findings:     convertFindings(report.Findings),
		AttackChains: convertChains(report.AttackChains),
		Summary:      convertSummary(report.Summary()),
		Stats: jsonStats{
			DurationMS:     report.Stats.Duration.Milliseconds(),
			FilesScanned:   report.Stats.FilesScanned,
			ChunksAnalyzed: report.Stats.ChunksAnalyzed,
		},
	}

	return json.MarshalIndent(jr, "", "  ")
}

// convertFindings converts a slice of security.Finding to jsonFinding.
func convertFindings(findings []security.Finding) []jsonFinding {
	if findings == nil {
		return []jsonFinding{}
	}
	result := make([]jsonFinding, len(findings))
	for i, f := range findings {
		result[i] = convertFinding(f)
	}
	return result
}

// convertFinding converts a single Finding to its JSON representation.
func convertFinding(f security.Finding) jsonFinding {
	refs := f.References
	if refs == nil {
		refs = []string{}
	}
	return jsonFinding{
		ID:          f.ID,
		Scanner:     f.Scanner,
		Severity:    string(f.Severity),
		Category:    string(f.Category),
		Title:       f.Title,
		Description: f.Description,
		Location: jsonLocation{
			File:      f.Location.File,
			StartLine: f.Location.StartLine,
			EndLine:   f.Location.EndLine,
			Function:  f.Location.Function,
		},
		CWE:         f.CWE,
		OWASP:       f.OWASP,
		Evidence:    f.Evidence,
		Remediation: f.Remediation,
		Confidence:  string(f.Confidence),
		References:  refs,
		Metadata:    f.Metadata,
		SkillSource: f.SkillSource,
	}
}

// convertChains converts a slice of AttackChain to JSON representation.
func convertChains(chains []security.AttackChain) []jsonAttackChain {
	if chains == nil {
		return []jsonAttackChain{}
	}
	result := make([]jsonAttackChain, len(chains))
	for i, c := range chains {
		result[i] = jsonAttackChain{
			ID:         c.ID,
			Title:      c.Title,
			Severity:   string(c.Severity),
			Steps:      convertFindings(c.Steps),
			Impact:     c.Impact,
			Likelihood: c.Likelihood,
		}
	}
	return result
}

// convertSummary converts a ReportSummary to JSON representation.
func convertSummary(s security.ReportSummary) jsonSummary {
	return jsonSummary{
		Critical: s.Critical,
		High:     s.High,
		Medium:   s.Medium,
		Low:      s.Low,
		Info:     s.Info,
		Chains:   s.Chains,
		Total:    s.Total,
	}
}
