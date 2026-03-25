package output

import (
	"fmt"
	"strings"
)

// GitHubAnnotationsFormatter emits GitHub Actions workflow commands
// (::error, ::warning, ::notice) so security findings appear as inline
// annotations in the PR file view.
type GitHubAnnotationsFormatter struct{}

// NewGitHubAnnotationsFormatter creates a new GitHubAnnotationsFormatter.
func NewGitHubAnnotationsFormatter() *GitHubAnnotationsFormatter {
	return &GitHubAnnotationsFormatter{}
}

// Format renders each security finding as a GitHub workflow command.
func (f *GitHubAnnotationsFormatter) Format(result *RunResult) ([]byte, error) {
	if len(result.SecurityFindings) == 0 {
		return nil, nil
	}

	var b strings.Builder
	for _, finding := range result.SecurityFindings {
		level := annotationLevel(finding.Severity)
		params := annotationParams(finding)
		msg := escapeAnnotationMessage(finding.Title)
		fmt.Fprintf(&b, "::%s %s::%s\n", level, params, msg)
	}
	return []byte(b.String()), nil
}

// annotationLevel maps severity to GitHub annotation level.
func annotationLevel(severity string) string {
	switch severity {
	case "critical", "high":
		return "error"
	case "medium":
		return "warning"
	default:
		return "notice"
	}
}

// annotationParams builds the file=,line= parameter string.
func annotationParams(f SecurityFinding) string {
	var parts []string
	if f.File != "" {
		parts = append(parts, fmt.Sprintf("file=%s", f.File))
	}
	if f.Line > 0 {
		parts = append(parts, fmt.Sprintf("line=%d", f.Line))
	}
	if f.Title != "" {
		parts = append(parts, fmt.Sprintf("title=%s", escapeAnnotationValue(f.Title)))
	}
	return strings.Join(parts, ",")
}

// escapeAnnotationMessage escapes special characters in annotation messages
// per the GitHub workflow command spec.
func escapeAnnotationMessage(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeAnnotationValue escapes special characters in annotation parameter values.
func escapeAnnotationValue(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}
