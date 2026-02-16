// internal/output/json.go
package output

import "encoding/json"

// JSONFormatter outputs RunResult as JSON.
type JSONFormatter struct{}

// NewJSONFormatter creates a new JSONFormatter.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

// Format marshals the RunResult as indented JSON with a trailing newline.
func (f *JSONFormatter) Format(result *RunResult) ([]byte, error) {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
