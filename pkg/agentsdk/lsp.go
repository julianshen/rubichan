package agentsdk

import "fmt"

// LSPConfig defines a language server connection.
type LSPConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	RootURI string   `json:"root_uri,omitempty"`
}

// Validate checks that the LSPConfig fields are consistent.
func (c *LSPConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("lsp config: name is required")
	}
	if c.Command == "" {
		return fmt.Errorf("lsp config %q: command is required", c.Name)
	}
	return nil
}

// LSPDiagnosticSeverity represents the severity of a diagnostic.
type LSPDiagnosticSeverity int

const (
	LSPSeverityError       LSPDiagnosticSeverity = 1
	LSPSeverityWarning     LSPDiagnosticSeverity = 2
	LSPSeverityInformation LSPDiagnosticSeverity = 3
	LSPSeverityHint        LSPDiagnosticSeverity = 4
)

var severityNames = [...]string{"", "error", "warning", "info", "hint"}

// String returns a human-readable label for the severity.
func (s LSPDiagnosticSeverity) String() string {
	if s >= 1 && s <= 4 {
		return severityNames[s]
	}
	return "unknown"
}

// LSPPosition represents a zero-based line and character offset.
type LSPPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// LSPRange represents a range in a text document.
type LSPRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

// LSPDiagnostic represents a single diagnostic from a language server.
type LSPDiagnostic struct {
	Severity LSPDiagnosticSeverity `json:"severity"`
	Message  string                `json:"message"`
	Source   string                `json:"source,omitempty"`
	Range    LSPRange              `json:"range"`
	Code     string                `json:"code,omitempty"`
	FilePath string                `json:"file_path"`
}
