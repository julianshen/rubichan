package output

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/security"
)

// WikiOutput contains the three wiki page contents produced by the formatter.
type WikiOutput struct {
	Overview     string `json:"overview"`
	Findings     string `json:"findings"`
	AttackChains string `json:"attack_chains"`
}

// WikiFormatter formats a security report as wiki pages (marshaled as JSON).
type WikiFormatter struct{}

// NewWikiFormatter creates a new WikiFormatter.
func NewWikiFormatter() *WikiFormatter {
	return &WikiFormatter{}
}

// Name returns the formatter name.
func (f *WikiFormatter) Name() string {
	return "wiki"
}

// Format renders the report as a WikiOutput JSON containing three markdown pages.
func (f *WikiFormatter) Format(report *security.Report) ([]byte, error) {
	wiki := WikiOutput{
		Overview:     buildOverviewPage(report),
		Findings:     buildFindingsPage(report),
		AttackChains: buildAttackChainsPage(report),
	}

	return json.MarshalIndent(wiki, "", "  ")
}

// buildOverviewPage creates the overview.md content with summary stats.
func buildOverviewPage(report *security.Report) string {
	summary := report.Summary()

	var b strings.Builder
	b.WriteString("# Security Overview\n\n")
	b.WriteString("## Scan Statistics\n\n")
	b.WriteString(fmt.Sprintf("- **Duration:** %dms\n", report.Stats.Duration.Milliseconds()))
	b.WriteString(fmt.Sprintf("- **Files scanned:** %d\n", report.Stats.FilesScanned))
	b.WriteString(fmt.Sprintf("- **Chunks analyzed:** %d\n", report.Stats.ChunksAnalyzed))
	b.WriteString("\n")

	b.WriteString("## Findings Summary\n\n")
	b.WriteString("| Severity | Count |\n")
	b.WriteString("|----------|-------|\n")
	b.WriteString(fmt.Sprintf("| Critical | %d |\n", summary.Critical))
	b.WriteString(fmt.Sprintf("| High | %d |\n", summary.High))
	b.WriteString(fmt.Sprintf("| Medium | %d |\n", summary.Medium))
	b.WriteString(fmt.Sprintf("| Low | %d |\n", summary.Low))
	b.WriteString(fmt.Sprintf("| Info | %d |\n", summary.Info))
	b.WriteString(fmt.Sprintf("| **Total** | **%d** |\n", summary.Total))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("**Attack chains detected:** %d\n", summary.Chains))

	return b.String()
}

// buildFindingsPage creates the findings.md content grouped by category.
func buildFindingsPage(report *security.Report) string {
	var b strings.Builder
	b.WriteString("# Security Findings\n\n")

	if len(report.Findings) == 0 {
		b.WriteString("No findings detected.\n")
		return b.String()
	}

	// Group by category
	byCategory := make(map[security.Category][]security.Finding)
	var categoryOrder []security.Category
	for _, f := range report.Findings {
		if _, exists := byCategory[f.Category]; !exists {
			categoryOrder = append(categoryOrder, f.Category)
		}
		byCategory[f.Category] = append(byCategory[f.Category], f)
	}

	for _, cat := range categoryOrder {
		findings := byCategory[cat]
		b.WriteString(fmt.Sprintf("## %s\n\n", string(cat)))

		for _, f := range findings {
			b.WriteString(fmt.Sprintf("### [%s] %s\n\n", f.ID, f.Title))
			b.WriteString(fmt.Sprintf("- **Severity:** %s\n", string(f.Severity)))
			b.WriteString(fmt.Sprintf("- **Confidence:** %s\n", string(f.Confidence)))
			loc := fmt.Sprintf("%s:%d", f.Location.File, f.Location.StartLine)
			if f.Location.EndLine > f.Location.StartLine {
				loc = fmt.Sprintf("%s:%d-%d", f.Location.File, f.Location.StartLine, f.Location.EndLine)
			}
			if f.Location.Function != "" {
				loc += fmt.Sprintf(" (%s)", f.Location.Function)
			}
			b.WriteString(fmt.Sprintf("- **Location:** %s\n", loc))
			if f.CWE != "" {
				b.WriteString(fmt.Sprintf("- **CWE:** %s\n", f.CWE))
			}
			if f.Description != "" {
				b.WriteString(fmt.Sprintf("- **Description:** %s\n", f.Description))
			}
			if f.Remediation != "" {
				b.WriteString(fmt.Sprintf("- **Remediation:** %s\n", f.Remediation))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// buildAttackChainsPage creates the attack-chains.md content.
func buildAttackChainsPage(report *security.Report) string {
	var b strings.Builder
	b.WriteString("# Attack Chains\n\n")

	if len(report.AttackChains) == 0 {
		b.WriteString("No attack chains detected.\n")
		return b.String()
	}

	for _, chain := range report.AttackChains {
		b.WriteString(fmt.Sprintf("## [%s] %s\n\n", chain.ID, chain.Title))
		b.WriteString(fmt.Sprintf("- **Severity:** %s\n", string(chain.Severity)))
		if chain.Impact != "" {
			b.WriteString(fmt.Sprintf("- **Impact:** %s\n", chain.Impact))
		}
		if chain.Likelihood != "" {
			b.WriteString(fmt.Sprintf("- **Likelihood:** %s\n", chain.Likelihood))
		}

		if len(chain.Steps) > 0 {
			b.WriteString("\n### Steps\n\n")
			for i, step := range chain.Steps {
				b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, step.ID, step.Title))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}
