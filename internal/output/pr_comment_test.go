package output

import (
	"encoding/json"
	"strings"
	"testing"
)

var _ Formatter = (*PRCommentFormatter)(nil)

func TestPRCommentFormatterBasic(t *testing.T) {
	f := NewPRCommentFormatter()
	result := &RunResult{
		Prompt:     "Review this code",
		Response:   "The code looks clean with no major issues.",
		Mode:       "code-review",
		TurnCount:  3,
		DurationMs: 1500,
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "code-review") {
		t.Error("output missing mode")
	}
	if !strings.Contains(s, "The code looks clean") {
		t.Error("output missing response")
	}
}

func TestPRCommentFormatterWithFindings(t *testing.T) {
	f := NewPRCommentFormatter()
	result := &RunResult{
		Response: "Found issues",
		Mode:     "code-review",
		SecurityFindings: []SecurityFinding{
			{ID: "F1", Severity: "critical", Title: "SQL injection", File: "db.go", Line: 10},
			{ID: "F2", Severity: "medium", Title: "Missing input validation", File: "api.go", Line: 25},
		},
		SecuritySummary: &SecuritySummaryData{Critical: 1, Medium: 1},
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "SQL injection") {
		t.Error("output missing critical finding")
	}
	if !strings.Contains(s, "Missing input validation") {
		t.Error("output missing medium finding")
	}
	if !strings.Contains(s, "critical") {
		t.Error("output missing severity label")
	}
}

func TestPRCommentFormatterWithToolCalls(t *testing.T) {
	f := NewPRCommentFormatter()
	result := &RunResult{
		Response: "Analysis complete",
		Mode:     "code-review",
		ToolCalls: []ToolCallLog{
			{ID: "tc1", Name: "read_file", Input: json.RawMessage(`{"path":"main.go"}`), Result: "file contents..."},
		},
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "<details>") {
		t.Error("output missing collapsible details for tool calls")
	}
	if !strings.Contains(s, "read_file") {
		t.Error("output missing tool name")
	}
}

func TestPRCommentFormatterLongOutput(t *testing.T) {
	f := NewPRCommentFormatter()
	// Verify long responses are returned in full (truncation is handled by the bridge layer).
	longResponse := strings.Repeat("x", 70000)
	result := &RunResult{
		Response: longResponse,
		Mode:     "generic",
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(string(out), longResponse) {
		t.Error("output should contain the full response")
	}
}

func TestPRCommentFormatterErrorResult(t *testing.T) {
	f := NewPRCommentFormatter()
	result := &RunResult{
		Error: "context deadline exceeded",
		Mode:  "code-review",
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "context deadline exceeded") {
		t.Error("output missing error message")
	}
	if !strings.Contains(s, "Error") {
		t.Error("output missing error header")
	}
}

func TestPRCommentFormatterEmptyResult(t *testing.T) {
	f := NewPRCommentFormatter()
	result := &RunResult{
		Response: "All good, no issues found.",
		Mode:     "code-review",
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "All good") {
		t.Error("output missing response")
	}
	// Should NOT have findings section when there are none.
	if strings.Contains(s, "Security Findings") {
		t.Error("output should not have findings section when empty")
	}
}
