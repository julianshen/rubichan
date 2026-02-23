// internal/output/formatter.go
package output

import "encoding/json"

// RunResult holds the collected output from a headless agent run.
type RunResult struct {
	Prompt           string               `json:"prompt"`
	Response         string               `json:"response"`
	ToolCalls        []ToolCallLog        `json:"tool_calls,omitempty"`
	TurnCount        int                  `json:"turn_count"`
	DurationMs       int64                `json:"duration_ms"`
	Mode             string               `json:"mode"`
	Error            string               `json:"error,omitempty"`
	SecurityFindings []SecurityFinding    `json:"security_findings,omitempty"`
	SecuritySummary  *SecuritySummaryData `json:"security_summary,omitempty"`
}

// SecurityFinding is a simplified security finding for output formatting.
type SecurityFinding struct {
	ID       string `json:"id"`
	Scanner  string `json:"scanner"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// SecuritySummaryData provides aggregate counts of security findings.
type SecuritySummaryData struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

// ToolCallLog records a single tool invocation during a run.
type ToolCallLog struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Result  string          `json:"result"`
	IsError bool            `json:"is_error,omitempty"`
}

// Formatter formats a RunResult into output bytes.
type Formatter interface {
	Format(result *RunResult) ([]byte, error)
}
