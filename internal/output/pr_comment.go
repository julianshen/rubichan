package output

import (
	"fmt"
	"strings"
)

// PRCommentFormatter formats a RunResult as a unified PR comment combining
// the LLM review response and security findings into a single markdown body.
type PRCommentFormatter struct{}

// NewPRCommentFormatter creates a new PRCommentFormatter.
func NewPRCommentFormatter() *PRCommentFormatter {
	return &PRCommentFormatter{}
}

// Format renders the RunResult as a PR comment markdown string.
func (f *PRCommentFormatter) Format(result *RunResult) ([]byte, error) {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("## Rubichan %s\n\n", result.Mode))

	// Error section
	if result.Error != "" {
		b.WriteString(fmt.Sprintf("### Error\n\n%s\n\n", result.Error))
	}

	// Response
	if result.Response != "" {
		b.WriteString(result.Response)
		b.WriteString("\n\n")
	}

	// Security findings
	if len(result.SecurityFindings) > 0 {
		b.WriteString("### Security Findings\n\n")
		if result.SecuritySummary != nil {
			b.WriteString(formatSummaryBadges(result.SecuritySummary))
			b.WriteString("\n\n")
		}
		b.WriteString("| Severity | Title | File | Line |\n")
		b.WriteString("|----------|-------|------|------|\n")
		for _, f := range result.SecurityFindings {
			b.WriteString(fmt.Sprintf("| %s %s | %s | %s | %d |\n",
				severityIndicator(f.Severity), f.Severity, f.Title, f.File, f.Line))
		}
		b.WriteString("\n")
	}

	// Tool calls (collapsible)
	if len(result.ToolCalls) > 0 {
		b.WriteString("<details>\n")
		b.WriteString("<summary>Tool calls</summary>\n\n")
		for _, tc := range result.ToolCalls {
			b.WriteString(fmt.Sprintf("- **%s** (`%s`)\n", tc.Name, tc.ID))
		}
		b.WriteString("\n</details>\n\n")
	}

	// Footer
	b.WriteString(fmt.Sprintf("---\n*%d turns, %dms*\n", result.TurnCount, result.DurationMs))

	return []byte(b.String()), nil
}

func severityIndicator(severity string) string {
	switch severity {
	case "critical":
		return "\U0001f6a8"
	case "high":
		return "\u274c"
	case "medium":
		return "\u26a0\ufe0f"
	case "low":
		return "\u2139\ufe0f"
	default:
		return "\U0001f4ac"
	}
}

func formatSummaryBadges(s *SecuritySummaryData) string {
	var parts []string
	if s.Critical > 0 {
		parts = append(parts, fmt.Sprintf("**%d critical**", s.Critical))
	}
	if s.High > 0 {
		parts = append(parts, fmt.Sprintf("**%d high**", s.High))
	}
	if s.Medium > 0 {
		parts = append(parts, fmt.Sprintf("%d medium", s.Medium))
	}
	if s.Low > 0 {
		parts = append(parts, fmt.Sprintf("%d low", s.Low))
	}
	if s.Info > 0 {
		parts = append(parts, fmt.Sprintf("%d info", s.Info))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}
