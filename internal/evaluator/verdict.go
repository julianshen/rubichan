package evaluator

import (
	"fmt"
	"strings"
)

// VerdictStatus is the outcome of a post-execution evaluation.
type VerdictStatus string

const (
	VerdictSuccess  VerdictStatus = "success"
	VerdictFailed   VerdictStatus = "failed"
	VerdictEscalate VerdictStatus = "escalate"
)

// SeverityLevel indicates the severity of an evidence finding.
type SeverityLevel string

const (
	SeverityError   SeverityLevel = "error"
	SeverityWarning SeverityLevel = "warning"
	SeverityInfo    SeverityLevel = "info"
)

// Confidence value constants for verdict aggregation.
const (
	confidenceFailedCheck  = 0.9
	confidenceMultiWarning = 0.6
	confidenceAllPassed    = 0.95
)

// Evidence is the output of a single Checker.
type Evidence struct {
	CheckerName string
	Passed      bool
	Severity    SeverityLevel
	Finding     string
	Suggestion  string
}

// Verdict is the aggregated result of running all Checkers.
type Verdict struct {
	Status      VerdictStatus
	Confidence  float64 // 0.0–1.0
	Reason      string
	Evidence    []Evidence
	Suggestions []string
}

// ToolOutput carries a tool's execution result to checkers.
type ToolOutput struct {
	ToolName string
	Content  string
	IsError  bool
}

// Checker evaluates one aspect of a ToolOutput.
type Checker interface {
	Name() string
	Check(output ToolOutput) Evidence
}

// CheckerPipeline runs all Checkers in sequence and aggregates a Verdict.
type CheckerPipeline struct {
	checkers []Checker
}

// NewCheckerPipeline creates a pipeline with the given checkers.
func NewCheckerPipeline(checkers ...Checker) *CheckerPipeline {
	return &CheckerPipeline{checkers: checkers}
}

// Evaluate runs all checkers on the tool output and returns an aggregated Verdict.
func (p *CheckerPipeline) Evaluate(output ToolOutput) Verdict {
	ev := make([]Evidence, 0, len(p.checkers))
	for _, c := range p.checkers {
		ev = append(ev, c.Check(output))
	}
	return aggregateVerdict(ev)
}

// FormatVerdict returns a concise text block suitable for injection into LLM conversation.
func FormatVerdict(v Verdict) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[evaluation] status=%s confidence=%.0f%%\n", v.Status, v.Confidence*100)
	fmt.Fprintf(&sb, "reason: %s\n", v.Reason)
	for _, e := range v.Evidence {
		if !e.Passed {
			fmt.Fprintf(&sb, "  • [%s] %s\n", e.Severity, e.Finding)
		}
	}
	if len(v.Suggestions) > 0 {
		fmt.Fprintf(&sb, "suggestions: %s\n", strings.Join(v.Suggestions, "; "))
	}
	return sb.String()
}

// aggregateVerdict combines evidence into a final Verdict.
func aggregateVerdict(ev []Evidence) Verdict {
	var (
		errorCount   int
		warningCount int
		suggestions  []string
	)
	for _, e := range ev {
		if !e.Passed {
			if e.Severity == SeverityError {
				errorCount++
			} else if e.Severity == SeverityWarning {
				warningCount++
			}
			if e.Suggestion != "" {
				suggestions = append(suggestions, e.Suggestion)
			}
		}
	}

	switch {
	case errorCount > 0:
		return Verdict{
			Status:      VerdictFailed,
			Confidence:  confidenceFailedCheck,
			Reason:      "one or more critical checks failed",
			Evidence:    ev,
			Suggestions: suggestions,
		}
	case warningCount > 1:
		return Verdict{
			Status:      VerdictEscalate,
			Confidence:  confidenceMultiWarning,
			Reason:      "multiple warnings detected; review recommended",
			Evidence:    ev,
			Suggestions: suggestions,
		}
	default:
		return Verdict{
			Status:      VerdictSuccess,
			Confidence:  confidenceAllPassed,
			Reason:      "all checks passed",
			Evidence:    ev,
			Suggestions: suggestions,
		}
	}
}
