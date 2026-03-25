package output

import (
	"strings"
	"testing"
)

var _ Formatter = (*GitHubAnnotationsFormatter)(nil)

func TestAnnotationSingleFinding(t *testing.T) {
	f := NewGitHubAnnotationsFormatter()
	result := &RunResult{
		SecurityFindings: []SecurityFinding{
			{ID: "F1", Severity: "medium", Title: "Missing validation", File: "api.go", Line: 25},
		},
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "::warning file=api.go,line=25") {
		t.Errorf("output = %q, want ::warning annotation", s)
	}
	if !strings.Contains(s, "Missing validation") {
		t.Error("output missing finding title")
	}
}

func TestAnnotationSeverityMapping(t *testing.T) {
	tests := []struct {
		severity string
		want     string
	}{
		{"critical", "::error"},
		{"high", "::error"},
		{"medium", "::warning"},
		{"low", "::notice"},
		{"info", "::notice"},
	}
	for _, tt := range tests {
		f := NewGitHubAnnotationsFormatter()
		result := &RunResult{
			SecurityFindings: []SecurityFinding{
				{Severity: tt.severity, Title: "test"},
			},
		}
		out, _ := f.Format(result)
		if !strings.Contains(string(out), tt.want) {
			t.Errorf("severity=%q: output = %q, want %q", tt.severity, string(out), tt.want)
		}
	}
}

func TestAnnotationEscaping(t *testing.T) {
	f := NewGitHubAnnotationsFormatter()
	result := &RunResult{
		SecurityFindings: []SecurityFinding{
			{Severity: "high", Title: "Line1\nLine2\rLine3%special", File: "a.go", Line: 1},
		},
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	// Single finding should produce exactly one annotation line (plus trailing newline).
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 annotation line, got %d: %q", len(lines), s)
	}
	if !strings.Contains(s, "%25") {
		t.Error("% should be escaped to %25")
	}
	if !strings.Contains(s, "%0A") {
		t.Error("\\n should be escaped to %0A")
	}
	if !strings.Contains(s, "%0D") {
		t.Error("\\r should be escaped to %0D")
	}
}

func TestAnnotationNoFile(t *testing.T) {
	f := NewGitHubAnnotationsFormatter()
	result := &RunResult{
		SecurityFindings: []SecurityFinding{
			{Severity: "low", Title: "General warning"},
		},
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	s := string(out)
	if strings.Contains(s, "file=") {
		t.Error("annotation should not have file= when no file specified")
	}
	if !strings.Contains(s, "::notice") {
		t.Error("output missing ::notice")
	}
}

func TestAnnotationMultiple(t *testing.T) {
	f := NewGitHubAnnotationsFormatter()
	result := &RunResult{
		SecurityFindings: []SecurityFinding{
			{Severity: "high", Title: "Issue 1", File: "a.go", Line: 1},
			{Severity: "medium", Title: "Issue 2", File: "b.go", Line: 2},
			{Severity: "low", Title: "Issue 3", File: "c.go", Line: 3},
		},
	}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 annotation lines, got %d", len(lines))
	}
}

func TestAnnotationEmptyFindings(t *testing.T) {
	f := NewGitHubAnnotationsFormatter()
	result := &RunResult{}
	out, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if len(out) != 0 {
		t.Errorf("output = %q, want empty for no findings", string(out))
	}
}
