// internal/output/markdown.go
package output

import (
	"fmt"
	"strings"
)

// MarkdownFormatter outputs RunResult as human-readable Markdown.
type MarkdownFormatter struct{}

// NewMarkdownFormatter creates a new MarkdownFormatter.
func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{}
}

// Format renders the RunResult as Markdown.
func (f *MarkdownFormatter) Format(result *RunResult) ([]byte, error) {
	var b strings.Builder

	if result.Error != "" {
		b.WriteString("## Error\n\n")
		b.WriteString(result.Error)
		b.WriteString("\n")
		return []byte(b.String()), nil
	}

	b.WriteString(result.Response)
	b.WriteString("\n")

	if len(result.ToolCalls) > 0 {
		b.WriteString("\n## Tool Calls\n\n")
		for i, tc := range result.ToolCalls {
			status := "ok"
			if tc.IsError {
				status = "error"
			}
			b.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, tc.Name, status))
		}
	}

	if len(result.SecurityFindings) > 0 {
		b.WriteString("\n## Security Findings\n\n")
		for i, finding := range result.SecurityFindings {
			location := finding.File
			if finding.Line > 0 {
				location = fmt.Sprintf("%s:%d", finding.File, finding.Line)
			}
			b.WriteString(fmt.Sprintf("%d. **[%s]** %s", i+1, finding.Severity, finding.Title))
			if location != "" {
				b.WriteString(fmt.Sprintf(" — `%s`", location))
			}
			b.WriteString("\n")
		}
		if result.SecuritySummary != nil {
			s := result.SecuritySummary
			b.WriteString(fmt.Sprintf("\n**Summary:** %d critical, %d high, %d medium, %d low, %d info\n",
				s.Critical, s.High, s.Medium, s.Low, s.Info))
		}
	}

	turnLabel := "turns"
	if result.TurnCount == 1 {
		turnLabel = "turn"
	}

	var durationStr string
	if result.DurationMs >= 1000 {
		durationStr = fmt.Sprintf("%.1fs", float64(result.DurationMs)/1000)
	} else {
		durationStr = fmt.Sprintf("%dms", result.DurationMs)
	}
	b.WriteString(fmt.Sprintf("\n---\n*Completed in %d %s, %s*\n",
		result.TurnCount, turnLabel, durationStr))

	return []byte(b.String()), nil
}
