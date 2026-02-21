package output

import (
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/security"
)

// MarkdownFormatter formats a security report as Markdown.
type MarkdownFormatter struct{}

// NewMarkdownFormatter creates a new MarkdownFormatter.
func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{}
}

// Name returns the formatter name.
func (f *MarkdownFormatter) Name() string {
	return "markdown"
}

// severityOrder defines the display order for severity sections.
var severityOrder = []security.Severity{
	security.SeverityCritical,
	security.SeverityHigh,
	security.SeverityMedium,
	security.SeverityLow,
	security.SeverityInfo,
}

// severityLabel returns a human-readable label for a severity.
func severityLabel(s security.Severity) string {
	switch s {
	case security.SeverityCritical:
		return "Critical"
	case security.SeverityHigh:
		return "High"
	case security.SeverityMedium:
		return "Medium"
	case security.SeverityLow:
		return "Low"
	case security.SeverityInfo:
		return "Info"
	default:
		return string(s)
	}
}

// Format renders the report as Markdown.
func (f *MarkdownFormatter) Format(report *security.Report) ([]byte, error) {
	var b strings.Builder

	summary := report.Summary()

	// Header
	b.WriteString("# Security Scan Report\n\n")

	// Summary table
	b.WriteString("## Summary\n\n")
	b.WriteString("| Severity | Count |\n")
	b.WriteString("|----------|-------|\n")
	b.WriteString(fmt.Sprintf("| Critical | %d |\n", summary.Critical))
	b.WriteString(fmt.Sprintf("| High | %d |\n", summary.High))
	b.WriteString(fmt.Sprintf("| Medium | %d |\n", summary.Medium))
	b.WriteString(fmt.Sprintf("| Low | %d |\n", summary.Low))
	b.WriteString(fmt.Sprintf("| Info | %d |\n", summary.Info))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("**Total findings:** %d | **Attack chains:** %d | **Duration:** %dms\n\n",
		summary.Total, summary.Chains, report.Stats.Duration.Milliseconds()))

	// Group findings by severity
	bySeverity := make(map[security.Severity][]security.Finding)
	for _, finding := range report.Findings {
		bySeverity[finding.Severity] = append(bySeverity[finding.Severity], finding)
	}

	// Render finding sections in severity order
	for _, sev := range severityOrder {
		findings, ok := bySeverity[sev]
		if !ok || len(findings) == 0 {
			continue
		}

		b.WriteString(fmt.Sprintf("## %s Findings\n\n", severityLabel(sev)))

		for _, finding := range findings {
			writeFinding(&b, finding)
		}
	}

	// Attack chains
	if len(report.AttackChains) > 0 {
		b.WriteString("## Attack Chains\n\n")
		for _, chain := range report.AttackChains {
			writeChain(&b, chain)
		}
	}

	return []byte(b.String()), nil
}

// formatLocation returns a human-readable location string.
func formatLocation(loc security.Location) string {
	s := fmt.Sprintf("%s:%d", loc.File, loc.StartLine)
	if loc.EndLine > loc.StartLine {
		s = fmt.Sprintf("%s:%d-%d", loc.File, loc.StartLine, loc.EndLine)
	}
	if loc.Function != "" {
		s += fmt.Sprintf(" (%s)", loc.Function)
	}
	return s
}

// writeFinding writes a single finding as Markdown.
func writeFinding(b *strings.Builder, f security.Finding) {
	b.WriteString(fmt.Sprintf("### [%s] %s\n\n", f.ID, f.Title))
	b.WriteString(fmt.Sprintf("- **Severity:** %s | **Confidence:** %s\n",
		severityLabel(f.Severity), severityLabel(security.Severity(f.Confidence))))
	b.WriteString(fmt.Sprintf("- **Location:** %s\n", formatLocation(f.Location)))
	b.WriteString(fmt.Sprintf("- **Category:** %s | **CWE:** %s\n", string(f.Category), f.CWE))
	if f.Description != "" {
		b.WriteString(fmt.Sprintf("- **Description:** %s\n", f.Description))
	}
	if f.Remediation != "" {
		b.WriteString(fmt.Sprintf("- **Remediation:** %s\n", f.Remediation))
	}
	b.WriteString("\n")
}

// writeChain writes a single attack chain as Markdown.
func writeChain(b *strings.Builder, c security.AttackChain) {
	b.WriteString(fmt.Sprintf("### [%s] %s\n\n", c.ID, c.Title))
	b.WriteString(fmt.Sprintf("- **Severity:** %s\n", severityLabel(c.Severity)))
	if c.Impact != "" {
		b.WriteString(fmt.Sprintf("- **Impact:** %s\n", c.Impact))
	}
	if len(c.Steps) > 0 {
		b.WriteString("- **Steps:**\n")
		for i, step := range c.Steps {
			loc := step.Location.File
			if step.Location.StartLine > 0 {
				loc = fmt.Sprintf("%s:%d", step.Location.File, step.Location.StartLine)
			}
			b.WriteString(fmt.Sprintf("  %d. [%s] %s (%s)\n", i+1, step.ID, step.Title, loc))
		}
	}
	b.WriteString("\n")
}
