package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSPConfig(t *testing.T) {
	cfg := LSPConfig{
		Name:    "gopls",
		Command: "gopls",
		Args:    []string{"serve"},
		RootURI: "file:///Users/julian/project",
	}
	require.Equal(t, "gopls", cfg.Name)
	require.Equal(t, "gopls", cfg.Command)
	require.Equal(t, []string{"serve"}, cfg.Args)
	require.Equal(t, "file:///Users/julian/project", cfg.RootURI)
}

func TestLSPConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LSPConfig
		wantErr string
	}{
		{
			name: "valid",
			cfg:  LSPConfig{Name: "gopls", Command: "gopls"},
		},
		{
			name:    "missing name",
			cfg:     LSPConfig{Command: "gopls"},
			wantErr: "name is required",
		},
		{
			name:    "missing command",
			cfg:     LSPConfig{Name: "gopls"},
			wantErr: "command is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLSPConfigJSONRoundTrip(t *testing.T) {
	cfg := LSPConfig{
		Name:    "test",
		Command: "cmd",
		Args:    []string{"--flag"},
		RootURI: "file:///project",
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded LSPConfig
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, cfg.Name, decoded.Name)
	assert.Equal(t, cfg.Command, decoded.Command)
	assert.Equal(t, cfg.Args, decoded.Args)
	assert.Equal(t, cfg.RootURI, decoded.RootURI)
}

func TestLSPDiagnostic(t *testing.T) {
	d := LSPDiagnostic{
		Severity: LSPSeverityError,
		Message:  "undefined: foo",
		Source:   "compiler",
		Range: LSPRange{
			Start: LSPPosition{Line: 41, Character: 9},
			End:   LSPPosition{Line: 41, Character: 12},
		},
		FilePath: "/tmp/main.go",
	}
	require.Equal(t, "undefined: foo", d.Message)
	require.Equal(t, 41, d.Range.Start.Line)
	require.Equal(t, 9, d.Range.Start.Character)
	require.Equal(t, LSPSeverityError, d.Severity)
}

func TestLSPDiagnosticSeverityString(t *testing.T) {
	assert.Equal(t, "error", LSPSeverityError.String())
	assert.Equal(t, "warning", LSPSeverityWarning.String())
	assert.Equal(t, "info", LSPSeverityInformation.String())
	assert.Equal(t, "hint", LSPSeverityHint.String())
	assert.Equal(t, "unknown", LSPDiagnosticSeverity(99).String())
	assert.Equal(t, "unknown", LSPDiagnosticSeverity(0).String())
}

func TestLSPDiagnosticJSONRoundTrip(t *testing.T) {
	d := LSPDiagnostic{
		Severity: LSPSeverityWarning,
		Message:  "unused import",
		Source:   "gopls",
		Range: LSPRange{
			Start: LSPPosition{Line: 2, Character: 0},
			End:   LSPPosition{Line: 2, Character: 10},
		},
		Code:     "unused",
		FilePath: "/tmp/main.go",
	}

	data, err := json.Marshal(d)
	require.NoError(t, err)

	var decoded LSPDiagnostic
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, d.Severity, decoded.Severity)
	assert.Equal(t, d.Message, decoded.Message)
	assert.Equal(t, d.Source, decoded.Source)
	assert.Equal(t, d.Range.Start.Line, decoded.Range.Start.Line)
	assert.Equal(t, d.Range.End.Character, decoded.Range.End.Character)
	assert.Equal(t, d.Code, decoded.Code)
	assert.Equal(t, d.FilePath, decoded.FilePath)
}
