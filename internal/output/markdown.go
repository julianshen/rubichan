// internal/output/markdown.go
package output

import (
	"encoding/json"
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
		b.WriteString("\n\n")
	}

	body := strings.TrimSpace(result.Response)
	if body == "" {
		body = strings.TrimSpace(result.Summary)
	}
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}

	if strings.TrimSpace(result.EvidenceSummary) != "" {
		b.WriteString("\n## Evidence\n\n")
		b.WriteString(strings.TrimSpace(result.EvidenceSummary))
		b.WriteString("\n")
	}

	if len(result.ToolCalls) > 0 {
		b.WriteString("\n## Tool Calls\n\n")
		for i, tc := range result.ToolCalls {
			status := "ok"
			if tc.IsError {
				status = "error"
			}
			summary := formatToolCallSummary(tc)
			if summary != "" {
				b.WriteString(fmt.Sprintf("%d. **%s** %s (%s)\n", i+1, tc.Name, summary, status))
			} else {
				b.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, tc.Name, status))
			}
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

// formatToolCallSummary extracts a concise description from a tool call's
// JSON input for display in the markdown tool call list.
func formatToolCallSummary(tc ToolCallLog) string {
	if len(tc.Input) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(tc.Input, &obj); err != nil {
		return ""
	}

	// Pick the most informative field based on tool name.
	switch tc.Name {
	case "shell":
		return mdQuote(jsonString(obj["command"]))
	case "file":
		op := jsonString(obj["operation"])
		path := jsonString(obj["path"])
		if op != "" && path != "" {
			return fmt.Sprintf("%s `%s`", op, path)
		}
		return mdQuote(path)
	case "search":
		pattern := jsonString(obj["pattern"])
		if pattern != "" {
			return fmt.Sprintf("`%s`", pattern)
		}
	}

	// For prefixed tools, try common field names.
	for _, key := range []string{"command", "path", "url", "query", "pattern", "ref", "file"} {
		if v := jsonString(obj[key]); v != "" {
			return mdQuote(v)
		}
	}
	return ""
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func mdQuote(s string) string {
	if s == "" {
		return ""
	}
	const maxLen = 80
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return fmt.Sprintf("`%s`", s)
}
