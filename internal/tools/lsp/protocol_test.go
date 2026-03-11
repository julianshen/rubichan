package lsp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticSeverityString(t *testing.T) {
	tests := []struct {
		severity DiagnosticSeverity
		want     string
	}{
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{SeverityInformation, "info"},
		{SeverityHint, "hint"},
		{DiagnosticSeverity(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.severity.String())
	}
}

func TestSymbolKindString(t *testing.T) {
	assert.Equal(t, "function", SymbolKindFunction.String())
	assert.Equal(t, "interface", SymbolKindInterface.String())
	assert.Equal(t, "struct", SymbolKindStruct.String())
	assert.Equal(t, "unknown", SymbolKind(999).String())
}

func TestDiagnosticJSON(t *testing.T) {
	d := Diagnostic{
		Range: Range{
			Start: Position{Line: 10, Character: 5},
			End:   Position{Line: 10, Character: 15},
		},
		Severity: SeverityError,
		Source:   "gopls",
		Message:  "undefined: foo",
	}

	data, err := json.Marshal(d)
	require.NoError(t, err)

	var got Diagnostic
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, d.Range, got.Range)
	assert.Equal(t, d.Severity, got.Severity)
	assert.Equal(t, d.Source, got.Source)
	assert.Equal(t, d.Message, got.Message)
}

func TestLocationJSON(t *testing.T) {
	loc := Location{
		URI:   "file:///home/user/main.go",
		Range: Range{Start: Position{Line: 5, Character: 0}, End: Position{Line: 5, Character: 10}},
	}

	data, err := json.Marshal(loc)
	require.NoError(t, err)

	var got Location
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, loc, got)
}

func TestPublishDiagnosticsParamsJSON(t *testing.T) {
	params := PublishDiagnosticsParams{
		URI: "file:///home/user/main.go",
		Diagnostics: []Diagnostic{
			{
				Range:    Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 5}},
				Severity: SeverityWarning,
				Message:  "unused variable",
			},
		},
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var got PublishDiagnosticsParams
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, params.URI, got.URI)
	assert.Len(t, got.Diagnostics, 1)
	assert.Equal(t, "unused variable", got.Diagnostics[0].Message)
}

func TestInitializeParamsJSON(t *testing.T) {
	params := InitializeParams{
		ProcessID: 1234,
		RootURI:   "file:///workspace",
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Hover: &HoverClientCapabilities{
					ContentFormat: []string{"markdown", "plaintext"},
				},
			},
		},
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var got InitializeParams
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 1234, got.ProcessID)
	assert.Equal(t, "file:///workspace", got.RootURI)
	require.NotNil(t, got.Capabilities.TextDocument.Hover)
	assert.Equal(t, []string{"markdown", "plaintext"}, got.Capabilities.TextDocument.Hover.ContentFormat)
}

func TestHoverJSON(t *testing.T) {
	h := Hover{
		Contents: MarkupContent{Kind: "markdown", Value: "```go\nfunc Foo() string\n```"},
	}

	data, err := json.Marshal(h)
	require.NoError(t, err)

	var got Hover
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "markdown", got.Contents.Kind)
	assert.Contains(t, got.Contents.Value, "func Foo()")
}

func TestWorkspaceEditJSON(t *testing.T) {
	edit := WorkspaceEdit{
		Changes: map[string][]TextEdit{
			"file:///main.go": {
				{Range: Range{Start: Position{Line: 5, Character: 0}, End: Position{Line: 5, Character: 3}}, NewText: "bar"},
			},
		},
	}

	data, err := json.Marshal(edit)
	require.NoError(t, err)

	var got WorkspaceEdit
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got.Changes["file:///main.go"], 1)
	assert.Equal(t, "bar", got.Changes["file:///main.go"][0].NewText)
}

func TestCallHierarchyItemJSON(t *testing.T) {
	item := CallHierarchyItem{
		Name:           "HandleRequest",
		Kind:           SymbolKindFunction,
		URI:            "file:///server.go",
		Range:          Range{Start: Position{Line: 20, Character: 0}, End: Position{Line: 30, Character: 1}},
		SelectionRange: Range{Start: Position{Line: 20, Character: 5}, End: Position{Line: 20, Character: 18}},
		Detail:         "package main",
	}

	data, err := json.Marshal(item)
	require.NoError(t, err)

	var got CallHierarchyItem
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "HandleRequest", got.Name)
	assert.Equal(t, SymbolKindFunction, got.Kind)
	assert.Equal(t, "package main", got.Detail)
}
