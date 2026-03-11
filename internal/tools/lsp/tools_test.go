package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPosToLSP(t *testing.T) {
	pos, err := userPosToLSP(1, 1)
	require.NoError(t, err)
	assert.Equal(t, Position{Line: 0, Character: 0}, pos)

	pos, err = userPosToLSP(10, 5)
	require.NoError(t, err)
	assert.Equal(t, Position{Line: 9, Character: 4}, pos)
}

func TestUserPosToLSPInvalid(t *testing.T) {
	_, err := userPosToLSP(0, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "line must be >= 1")

	_, err = userPosToLSP(1, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "column must be >= 1")

	_, err = userPosToLSP(-1, 5)
	assert.Error(t, err)
}

func TestAllToolsCount(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")
	tools := AllTools(m)
	assert.Len(t, tools, 9)
}

func TestAllToolsNames(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")
	tools := AllTools(m)

	expected := []string{
		"lsp_diagnostics", "lsp_definition", "lsp_references",
		"lsp_hover", "lsp_rename", "lsp_completions",
		"lsp_code_action", "lsp_symbols", "lsp_call_hierarchy",
	}

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	assert.Equal(t, expected, names)
}

func TestDiagnosticsToolNoErrors(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	input, _ := json.Marshal(diagnosticsInput{File: "/test/main.go"})
	result, err := runDiagnostics(context.Background(), m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No errors found")
	assert.False(t, result.IsError)
}

func TestDiagnosticsToolWithResults(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	uri := pathToURI("/test/main.go")
	m.diagMu.Lock()
	m.diags[uri] = []Diagnostic{
		{Range: Range{Start: Position{Line: 5}}, Severity: SeverityError, Message: "undefined: foo"},
	}
	m.diagMu.Unlock()

	input, _ := json.Marshal(diagnosticsInput{File: "/test/main.go"})
	result, err := runDiagnostics(context.Background(), m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "undefined: foo")
}

func TestDiagnosticsToolInvalidInput(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	result, err := runDiagnostics(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestDefinitionToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)

	// Create a temp file for ensureFileOpen.
	tmpFile := createTempFile(t, "main.go", "package main\n")

	// Server goroutine: read didOpen notification, then read definition request.
	go func() {
		// didOpen notification.
		_, _ = readRequest(mt.server)
		// definition request.
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		locs := []Location{{URI: "file:///test/other.go", Range: Range{Start: Position{Line: 9, Character: 4}}}}
		_ = writeResponse(mt.server, req.ID, locs)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 5, Column: 10})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runDefinition(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "other.go:10:5")
}

func TestDefinitionToolInvalidPosition(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(positionInput{File: "/test/main.go", Line: 0, Column: 1})
	result, err := runDefinition(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "line must be >= 1")
}

func TestDefinitionToolNoServer(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	m := NewManager(reg, "/test")

	input, _ := json.Marshal(positionInput{File: "/test/main.go", Line: 1, Column: 1})
	result, err := runDefinition(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "LSP not available")
}

func TestHoverToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		hover := Hover{Contents: MarkupContent{Kind: "markdown", Value: "func Println(a ...any)"}}
		_ = writeResponse(mt.server, req.ID, hover)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runHover(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "func Println(a ...any)", result.Content)
}

func TestHoverToolNull(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, nil)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runHover(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no hover information")
}

func TestReferencesToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		locs := []Location{
			{URI: "file:///a.go", Range: Range{Start: Position{Line: 0, Character: 0}}},
			{URI: "file:///b.go", Range: Range{Start: Position{Line: 5, Character: 3}}},
		}
		_ = writeResponse(mt.server, req.ID, locs)
	}()

	input, _ := json.Marshal(referencesInput{positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runReferences(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "a.go:1:1")
	assert.Contains(t, result.Content, "b.go:6:4")
}

func TestReferencesToolNoResults(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []Location{})
	}()

	input, _ := json.Marshal(referencesInput{positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runReferences(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no references found")
}

func TestRenameToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		edit := WorkspaceEdit{
			Changes: map[string][]TextEdit{
				"file:///main.go": {
					{Range: Range{Start: Position{Line: 0, Character: 5}}, NewText: "newName"},
				},
			},
		}
		_ = writeResponse(mt.server, req.ID, edit)
	}()

	input, _ := json.Marshal(renameInput{positionInput: positionInput{File: tmpFile, Line: 1, Column: 6}, NewName: "newName"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runRename(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "newName")
	assert.Contains(t, result.Content, "1 edits across 1 files")
}

func TestRenameToolEmptyName(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(renameInput{positionInput: positionInput{File: "/test/main.go", Line: 1, Column: 1}, NewName: ""})
	result, err := runRename(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "new_name is required")
}

func TestCompletionsToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		list := CompletionList{
			Items: []CompletionItem{
				{Label: "Println", Detail: "func(a ...any)"},
				{Label: "Printf", Detail: "func(format string, a ...any)"},
			},
		}
		_ = writeResponse(mt.server, req.ID, list)
	}()

	input, _ := json.Marshal(completionsInput{positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCompletions(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Println")
	assert.Contains(t, result.Content, "Printf")
}

func TestCompletionsToolEmpty(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, CompletionList{Items: []CompletionItem{}})
	}()

	input, _ := json.Marshal(completionsInput{positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCompletions(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no completions available")
}

func TestCodeActionToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		actions := []CodeAction{
			{Title: "Add import", Kind: "quickfix"},
			{Title: "Extract function", Kind: "refactor.extract"},
		}
		_ = writeResponse(mt.server, req.ID, actions)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCodeAction(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "2 code actions available")
	assert.Contains(t, result.Content, "Add import [quickfix]")
	assert.Contains(t, result.Content, "Extract function [refactor.extract]")
}

func TestCodeActionToolNone(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []CodeAction{})
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCodeAction(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no code actions available")
}

func TestSymbolsToolNoServers(t *testing.T) {
	reg := NewRegistry()
	reg.lookPath = func(name string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	m := NewManager(reg, "/test")

	input, _ := json.Marshal(symbolsInput{Query: "Foo"})
	result, err := runSymbols(context.Background(), m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no language servers available")
}

func TestCallHierarchyToolInvalidDirection(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(callHierarchyInput{
		positionInput: positionInput{File: "/test/main.go", Line: 1, Column: 1}, Direction: "sideways",
	})
	result, err := runCallHierarchy(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "direction must be")
}

func TestCallHierarchyToolIncomingSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen

		// Prepare request.
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		items := []CallHierarchyItem{
			{Name: "HandleRequest", Kind: SymbolKindFunction, URI: "file:///main.go",
				Range: Range{Start: Position{Line: 10}}, SelectionRange: Range{Start: Position{Line: 10}}},
		}
		_ = writeResponse(mt.server, req.ID, items)

		// Incoming calls request.
		req, err = readRequest(mt.server)
		if err != nil {
			return
		}
		calls := []CallHierarchyIncomingCall{
			{From: CallHierarchyItem{
				Name: "main", Kind: SymbolKindFunction, URI: "file:///main.go",
				SelectionRange: Range{Start: Position{Line: 5}},
			}},
		}
		_ = writeResponse(mt.server, req.ID, calls)
	}()

	input, _ := json.Marshal(callHierarchyInput{
		positionInput: positionInput{File: tmpFile, Line: 11, Column: 1}, Direction: "incoming",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCallHierarchy(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "1 callers of HandleRequest")
	assert.Contains(t, result.Content, "main (function)")
}

func TestCallHierarchyToolOutgoingSuccess(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen

		// Prepare request.
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		items := []CallHierarchyItem{
			{Name: "main", Kind: SymbolKindFunction, URI: "file:///main.go",
				Range: Range{Start: Position{Line: 0}}, SelectionRange: Range{Start: Position{Line: 0}}},
		}
		_ = writeResponse(mt.server, req.ID, items)

		// Outgoing calls request.
		req, err = readRequest(mt.server)
		if err != nil {
			return
		}
		calls := []CallHierarchyOutgoingCall{
			{To: CallHierarchyItem{
				Name: "Println", Kind: SymbolKindFunction, URI: "file:///fmt/print.go",
				SelectionRange: Range{Start: Position{Line: 100}},
			}},
		}
		_ = writeResponse(mt.server, req.ID, calls)
	}()

	input, _ := json.Marshal(callHierarchyInput{
		positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}, Direction: "outgoing",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCallHierarchy(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "1 callees of main")
	assert.Contains(t, result.Content, "Println (function)")
}

func TestLspUnavailableResult(t *testing.T) {
	result := lspUnavailableResult("/test/main.go", fmt.Errorf("server not found"))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "LSP not available")
	assert.Contains(t, result.Content, "server not found")
}

func TestFormatWorkspaceEdit(t *testing.T) {
	edit := WorkspaceEdit{
		Changes: map[string][]TextEdit{
			"file:///main.go": {
				{Range: Range{Start: Position{Line: 0, Character: 5}}, NewText: "newName"},
				{Range: Range{Start: Position{Line: 10, Character: 0}}, NewText: "newName"},
			},
		},
	}

	text := formatWorkspaceEdit(edit)
	assert.Contains(t, text, "2 edits")
	assert.Contains(t, text, "1 files")
}

func TestFormatWorkspaceEditEmpty(t *testing.T) {
	edit := WorkspaceEdit{}
	assert.Equal(t, "no changes", formatWorkspaceEdit(edit))
}

func TestSymbolsToolSuccess(t *testing.T) {
	m, mt := newTestManager(t)

	// Make go appear as "available" in the registry.
	m.registry.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}

	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		symbols := []SymbolInformation{
			{Name: "HandleRequest", Kind: SymbolKindFunction,
				Location: Location{URI: "file:///server.go", Range: Range{Start: Position{Line: 10}}}},
			{Name: "Server", Kind: SymbolKindStruct,
				Location: Location{URI: "file:///server.go", Range: Range{Start: Position{Line: 5}}}},
		}
		_ = writeResponse(mt.server, req.ID, symbols)
	}()

	input, _ := json.Marshal(symbolsInput{Query: "Handle"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runSymbols(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "HandleRequest")
	assert.Contains(t, result.Content, "Server")
}

func TestSymbolsToolNoResults(t *testing.T) {
	m, mt := newTestManager(t)

	m.registry.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found")
	}

	go func() {
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []SymbolInformation{})
	}()

	input, _ := json.Marshal(symbolsInput{Query: "NonExistent"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runSymbols(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no symbols matching")
}

func TestSymbolsToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runSymbols(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestDefinitionToolSingleLocation(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		// Return a single Location instead of []Location.
		loc := Location{URI: "file:///single.go", Range: Range{Start: Position{Line: 0, Character: 0}}}
		_ = writeResponse(mt.server, req.ID, loc)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runDefinition(ctx, m, input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "single.go:1:1")
}

func TestDefinitionToolLSPError(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		// Send error response.
		resp := struct {
			JSONRPC string       `json:"jsonrpc"`
			ID      int64        `json:"id"`
			Error   jsonrpcError `json:"error"`
		}{JSONRPC: "2.0", ID: req.ID, Error: jsonrpcError{Code: -32601, Message: "method not found"}}
		body, _ := json.Marshal(resp)
		fmt.Fprintf(mt.server, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runDefinition(ctx, m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "LSP error")
}

func TestHoverToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runHover(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestReferencesToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runReferences(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestRenameToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runRename(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestCompletionsToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runCompletions(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestCodeActionToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runCodeAction(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestCallHierarchyToolInvalidInput(t *testing.T) {
	m, _ := newTestManager(t)

	result, err := runCallHierarchy(context.Background(), m, json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestReferencesToolInvalidPosition(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(referencesInput{positionInput: positionInput{File: "/test/main.go", Line: 0, Column: 1}})
	result, err := runReferences(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "line must be >= 1")
}

func TestRenameToolInvalidPosition(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(renameInput{positionInput: positionInput{File: "/test/main.go", Line: 1, Column: 0}, NewName: "x"})
	result, err := runRename(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "column must be >= 1")
}

func TestCompletionsToolInvalidPosition(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(completionsInput{positionInput: positionInput{File: "/test/main.go", Line: -1, Column: 1}})
	result, err := runCompletions(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestCodeActionToolInvalidPosition(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(positionInput{File: "/test/main.go", Line: 0, Column: 1})
	result, err := runCodeAction(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestCallHierarchyToolInvalidPosition(t *testing.T) {
	m, _ := newTestManager(t)

	input, _ := json.Marshal(callHierarchyInput{positionInput: positionInput{File: "/test/main.go", Line: 0, Column: 1}, Direction: "incoming"})
	result, err := runCallHierarchy(context.Background(), m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestCompletionsToolArrayResponse(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		// Return as []CompletionItem instead of CompletionList.
		items := []CompletionItem{
			{Label: "Foo"},
		}
		_ = writeResponse(mt.server, req.ID, items)
	}()

	input, _ := json.Marshal(completionsInput{positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCompletions(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Foo")
}

func TestDefinitionToolEmptyLocations(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []Location{})
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runDefinition(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no definition found")
}

func TestHoverToolLSPError(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		resp := struct {
			JSONRPC string       `json:"jsonrpc"`
			ID      int64        `json:"id"`
			Error   jsonrpcError `json:"error"`
		}{JSONRPC: "2.0", ID: req.ID, Error: jsonrpcError{Code: -32601, Message: "method not found"}}
		body, _ := json.Marshal(resp)
		fmt.Fprintf(mt.server, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}()

	input, _ := json.Marshal(positionInput{File: tmpFile, Line: 1, Column: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runHover(ctx, m, input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "LSP error")
}

func TestCallHierarchyToolNoPrepareItems(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []CallHierarchyItem{})
	}()

	input, _ := json.Marshal(callHierarchyInput{
		positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}, Direction: "incoming",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCallHierarchy(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no call hierarchy available")
}

func TestCallHierarchyToolNoIncomingCalls(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		items := []CallHierarchyItem{{Name: "foo", Kind: SymbolKindFunction}}
		_ = writeResponse(mt.server, req.ID, items)

		req, err = readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []CallHierarchyIncomingCall{})
	}()

	input, _ := json.Marshal(callHierarchyInput{
		positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}, Direction: "incoming",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCallHierarchy(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no incoming calls to foo")
}

func TestCallHierarchyToolNoOutgoingCalls(t *testing.T) {
	m, mt := newTestManager(t)
	tmpFile := createTempFile(t, "main.go", "package main\n")

	go func() {
		_, _ = readRequest(mt.server) // didOpen
		req, err := readRequest(mt.server)
		if err != nil {
			return
		}
		items := []CallHierarchyItem{{Name: "bar", Kind: SymbolKindFunction}}
		_ = writeResponse(mt.server, req.ID, items)

		req, err = readRequest(mt.server)
		if err != nil {
			return
		}
		_ = writeResponse(mt.server, req.ID, []CallHierarchyOutgoingCall{})
	}()

	input, _ := json.Marshal(callHierarchyInput{
		positionInput: positionInput{File: tmpFile, Line: 1, Column: 1}, Direction: "outgoing",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := runCallHierarchy(ctx, m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "no outgoing calls from bar")
}

func TestDiagnosticsToolIncludeWarnings(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")

	uri := pathToURI("/test/main.go")
	m.diagMu.Lock()
	m.diags[uri] = []Diagnostic{
		{Severity: SeverityWarning, Message: "unused import", Source: "compiler"},
	}
	m.diagMu.Unlock()

	// Without warnings — should show nothing.
	input, _ := json.Marshal(diagnosticsInput{File: "/test/main.go"})
	result, err := runDiagnostics(context.Background(), m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No errors found")

	// With warnings.
	input, _ = json.Marshal(diagnosticsInput{File: "/test/main.go", IncludeWarnings: true})
	result, err = runDiagnostics(context.Background(), m, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "unused import")
}

func TestURIPercentEncoding(t *testing.T) {
	// Path with spaces should be percent-encoded.
	uri := pathToURI("/path/with spaces/file.go")
	assert.Contains(t, uri, "with%20spaces")

	// Round-trip.
	path := uriToPath(uri)
	assert.Equal(t, "/path/with spaces/file.go", path)
}

func TestLspToolInterface(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, "/test")
	tool := NewDiagnosticsTool(m)

	assert.Equal(t, "lsp_diagnostics", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.NotNil(t, tool.InputSchema())

	// Execute with valid input.
	input, _ := json.Marshal(diagnosticsInput{File: "/test/main.go"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No errors found")
}
