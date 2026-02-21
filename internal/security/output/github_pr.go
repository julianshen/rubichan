package output

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/security"
)

// PRComment represents a single review comment on a PR.
type PRComment struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
}

// PRReview represents a complete PR review with summary and inline comments.
type PRReview struct {
	Body     string      `json:"body"`
	Comments []PRComment `json:"comments"`
}

// GitHubPRFormatter formats a security report as a PR review structure.
type GitHubPRFormatter struct{}

// NewGitHubPRFormatter creates a new GitHubPRFormatter.
func NewGitHubPRFormatter() *GitHubPRFormatter {
	return &GitHubPRFormatter{}
}

// Name returns the formatter name.
func (f *GitHubPRFormatter) Name() string {
	return "github-pr"
}

// severityEmoji returns an emoji prefix for the given severity.
func severityEmoji(s security.Severity) string {
	switch s {
	case security.SeverityCritical:
		return "\U0001f6a8" // rotating light
	case security.SeverityHigh:
		return "\u274c" // cross mark
	case security.SeverityMedium:
		return "\u26a0\ufe0f" // warning
	case security.SeverityLow:
		return "\u2139\ufe0f" // information
	default:
		return "\U0001f4ac" // speech bubble
	}
}

// Format marshals the PR review as JSON.
func (f *GitHubPRFormatter) Format(report *security.Report) ([]byte, error) {
	review := PRReview{
		Body:     buildSummaryBody(report),
		Comments: buildComments(report.Findings),
	}

	return json.MarshalIndent(review, "", "  ")
}

// buildSummaryBody creates a markdown summary for the PR review body.
func buildSummaryBody(report *security.Report) string {
	summary := report.Summary()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Security Scan: %d findings\n\n", summary.Total))

	if summary.Total == 0 {
		b.WriteString("No security findings detected.\n")
		return b.String()
	}

	b.WriteString("| Severity | Count |\n")
	b.WriteString("|----------|-------|\n")

	if summary.Critical > 0 {
		b.WriteString(fmt.Sprintf("| %s critical | %d |\n", severityEmoji(security.SeverityCritical), summary.Critical))
	}
	if summary.High > 0 {
		b.WriteString(fmt.Sprintf("| %s high | %d |\n", severityEmoji(security.SeverityHigh), summary.High))
	}
	if summary.Medium > 0 {
		b.WriteString(fmt.Sprintf("| %s medium | %d |\n", severityEmoji(security.SeverityMedium), summary.Medium))
	}
	if summary.Low > 0 {
		b.WriteString(fmt.Sprintf("| %s low | %d |\n", severityEmoji(security.SeverityLow), summary.Low))
	}
	if summary.Info > 0 {
		b.WriteString(fmt.Sprintf("| %s info | %d |\n", severityEmoji(security.SeverityInfo), summary.Info))
	}

	if summary.Chains > 0 {
		b.WriteString(fmt.Sprintf("\n**Attack chains detected:** %d\n", summary.Chains))
	}

	return b.String()
}

// buildComments creates a PRComment for each finding.
func buildComments(findings []security.Finding) []PRComment {
	comments := make([]PRComment, 0, len(findings))

	for _, f := range findings {
		var body strings.Builder
		body.WriteString(fmt.Sprintf("%s **[%s] %s**\n\n", severityEmoji(f.Severity), string(f.Severity), f.Title))

		if f.Description != "" {
			body.WriteString(f.Description)
			body.WriteString("\n\n")
		}

		if f.CWE != "" {
			body.WriteString(fmt.Sprintf("**CWE:** %s\n", f.CWE))
		}

		if f.Remediation != "" {
			body.WriteString(fmt.Sprintf("\n**Remediation:** %s\n", f.Remediation))
		}

		comments = append(comments, PRComment{
			Path:     f.Location.File,
			Line:     f.Location.StartLine,
			Body:     body.String(),
			Severity: string(f.Severity),
		})
	}

	return comments
}
