package output

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/julianshen/rubichan/internal/security"
)

// CycloneDX v1.5 BOM structures.

type cdxBOM struct {
	BOMFormat       string             `json:"bomFormat"`
	SpecVersion     string             `json:"specVersion"`
	Version         int                `json:"version"`
	Metadata        cdxMetadata        `json:"metadata"`
	Vulnerabilities []cdxVulnerability `json:"vulnerabilities"`
}

type cdxMetadata struct {
	Tools []cdxTool `json:"tools"`
}

type cdxTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type cdxVulnerability struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Source      cdxSource   `json:"source"`
	Ratings     []cdxRating `json:"ratings"`
	CWEs        []int       `json:"cwes,omitempty"`
	Affects     []cdxAffect `json:"affects"`
}

type cdxSource struct {
	Name string `json:"name"`
}

type cdxRating struct {
	Severity string `json:"severity"`
	Method   string `json:"method"`
}

type cdxAffect struct {
	Ref string `json:"ref"`
}

// CycloneDXFormatter formats a security report as CycloneDX v1.5 BOM JSON.
type CycloneDXFormatter struct{}

// NewCycloneDXFormatter creates a new CycloneDXFormatter.
func NewCycloneDXFormatter() *CycloneDXFormatter {
	return &CycloneDXFormatter{}
}

// Name returns the formatter name.
func (f *CycloneDXFormatter) Name() string {
	return "cyclonedx"
}

// Format renders the report as CycloneDX v1.5 JSON.
func (f *CycloneDXFormatter) Format(report *security.Report) ([]byte, error) {
	bom := cdxBOM{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: cdxMetadata{
			Tools: []cdxTool{
				{Name: "rubichan", Version: "0.1.0"},
			},
		},
		Vulnerabilities: buildVulnerabilities(report.Findings),
	}

	return json.MarshalIndent(bom, "", "  ")
}

// parseCWE extracts the numeric CWE ID from a string like "CWE-89".
// Returns -1 if the string cannot be parsed.
func parseCWE(cwe string) int {
	if !strings.HasPrefix(cwe, "CWE-") {
		return -1
	}
	num, err := strconv.Atoi(cwe[4:])
	if err != nil {
		return -1
	}
	return num
}

// buildVulnerabilities converts findings to CycloneDX vulnerability entries.
func buildVulnerabilities(findings []security.Finding) []cdxVulnerability {
	vulns := make([]cdxVulnerability, 0, len(findings))

	for _, f := range findings {
		vuln := cdxVulnerability{
			ID:          f.ID,
			Description: f.Title,
			Source:      cdxSource{Name: "rubichan"},
			Ratings: []cdxRating{
				{
					Severity: string(f.Severity),
					Method:   "other",
				},
			},
			Affects: []cdxAffect{
				{Ref: f.Location.File},
			},
		}

		if cweNum := parseCWE(f.CWE); cweNum > 0 {
			vuln.CWEs = []int{cweNum}
		}

		vulns = append(vulns, vuln)
	}

	return vulns
}
