// internal/output/json.go
package output

import "encoding/json"

// JSONFormatter outputs RunResult as JSON.
type JSONFormatter struct{}

// NewJSONFormatter creates a new JSONFormatter.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

// Format marshals the RunResult as indented JSON.
func (f *JSONFormatter) Format(result *RunResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}
