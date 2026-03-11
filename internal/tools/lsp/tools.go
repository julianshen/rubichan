package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

// lspTool provides a shared implementation of the tools.Tool interface,
// reducing boilerplate across the individual LSP tool constructors. Each
// tool only needs to supply a name, description, schema, and run function.
type lspTool struct {
	manager     *Manager
	name        string
	description string
	schema      json.RawMessage
	run         func(context.Context, *Manager, json.RawMessage) (tools.ToolResult, error)
}

func (t *lspTool) Name() string                 { return t.name }
func (t *lspTool) Description() string          { return t.description }
func (t *lspTool) InputSchema() json.RawMessage { return t.schema }

func (t *lspTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	return t.run(ctx, t.manager, input)
}

// AllTools returns all LSP tools for registration in the tool registry.
func AllTools(manager *Manager) []tools.Tool {
	return []tools.Tool{
		NewDiagnosticsTool(manager),
		NewDefinitionTool(manager),
		NewReferencesTool(manager),
		NewHoverTool(manager),
		NewRenameTool(manager),
		NewCompletionsTool(manager),
		NewCodeActionTool(manager),
		NewSymbolsTool(manager),
		NewCallHierarchyTool(manager),
	}
}

// userPosToLSP converts 1-based user line/column to 0-based LSP position.
// Returns an error if values are out of range.
func userPosToLSP(line, column int) (Position, error) {
	if line < 1 {
		return Position{}, fmt.Errorf("line must be >= 1, got %d", line)
	}
	if column < 1 {
		return Position{}, fmt.Errorf("column must be >= 1, got %d", column)
	}
	return Position{Line: line - 1, Character: column - 1}, nil
}

// --- lsp_diagnostics ---

type diagnosticsInput struct {
	File            string `json:"file"`
	IncludeWarnings bool   `json:"include_warnings,omitempty"`
}

// NewDiagnosticsTool returns a tool that fetches LSP diagnostics for a file.
func NewDiagnosticsTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_diagnostics",
		description: "Get compiler diagnostics (errors, warnings) for a file from the language server.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file to check"},
				"include_warnings": {"type": "boolean", "description": "Include warnings and hints (default: errors only)"}
			},
			"required": ["file"]
		}`),
		run: runDiagnostics,
	}
}

func runDiagnostics(_ context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in diagnosticsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	uri := pathToURI(in.File)
	diags := m.DiagnosticsFor(uri, in.IncludeWarnings)

	if len(diags) == 0 {
		severity := "errors"
		if in.IncludeWarnings {
			severity = "diagnostics"
		}
		return tools.ToolResult{Content: fmt.Sprintf("No %s found in %s", severity, in.File)}, nil
	}

	result := m.summarizer.SummarizeDiagnostics(diags, 0)
	return tools.ToolResult{Content: result.Text}, nil
}

// --- lsp_definition ---

type positionInput struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

func (in *positionInput) validate() (Position, error) {
	return userPosToLSP(in.Line, in.Column)
}

// NewDefinitionTool returns a tool that finds the definition of a symbol.
func NewDefinitionTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_definition",
		description: "Go to the definition of the symbol at the given position. Returns the file and line where the symbol is defined.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"}
			},
			"required": ["file", "line", "column"]
		}`),
		run: runDefinition,
	}
}

func runDefinition(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in positionInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(in.File)},
		Position:     pos,
	}

	result, err := client.Call(ctx, "textDocument/definition", params)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	// Result can be a single Location or []Location.
	var locs []Location
	if err := json.Unmarshal(result, &locs); err != nil {
		var loc Location
		if err := json.Unmarshal(result, &loc); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
		}
		locs = []Location{loc}
	}

	if len(locs) == 0 {
		return tools.ToolResult{Content: "no definition found"}, nil
	}

	text := formatLocations(locs)
	return tools.ToolResult{Content: text}, nil
}

// --- lsp_references ---

type referencesInput struct {
	positionInput
	MaxResults int `json:"max_results,omitempty"`
}

// NewReferencesTool returns a tool that finds all references to a symbol.
func NewReferencesTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_references",
		description: "Find all references to the symbol at the given position across the workspace. Results are summarized if there are many.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"},
				"max_results": {"type": "integer", "description": "Maximum results to return (default: 50)"}
			},
			"required": ["file", "line", "column"]
		}`),
		run: runReferences,
	}
}

func runReferences(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in referencesInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	params := ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(in.File)},
		Position:     pos,
		Context:      ReferenceContext{IncludeDeclaration: true},
	}

	result, err := client.Call(ctx, "textDocument/references", params)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	var locs []Location
	if err := json.Unmarshal(result, &locs); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
	}

	if len(locs) == 0 {
		return tools.ToolResult{Content: "no references found"}, nil
	}

	summary := m.summarizer.SummarizeLocations(locs, in.MaxResults)
	return tools.ToolResult{Content: summary.Text}, nil
}

// --- lsp_hover ---

// NewHoverTool returns a tool that gets hover information (type signature + docs).
func NewHoverTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_hover",
		description: "Get type signature and documentation for the symbol at the given position. More token-efficient than reading the full definition file.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"}
			},
			"required": ["file", "line", "column"]
		}`),
		run: runHover,
	}
}

func runHover(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in positionInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(in.File)},
		Position:     pos,
	}

	result, err := client.Call(ctx, "textDocument/hover", params)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	if result == nil || string(result) == "null" {
		return tools.ToolResult{Content: "no hover information available"}, nil
	}

	var hover Hover
	if err := json.Unmarshal(result, &hover); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
	}

	return tools.ToolResult{Content: hover.Contents.Value}, nil
}

// --- lsp_rename ---

type renameInput struct {
	positionInput
	NewName string `json:"new_name"`
}

// NewRenameTool returns a tool that renames a symbol across the workspace.
func NewRenameTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_rename",
		description: "Rename a symbol at the given position across the entire workspace. Returns the set of file changes needed.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"},
				"new_name": {"type": "string", "description": "New name for the symbol"}
			},
			"required": ["file", "line", "column", "new_name"]
		}`),
		run: runRename,
	}
}

func runRename(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in renameInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.NewName == "" {
		return tools.ToolResult{Content: "new_name is required", IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	params := RenameParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(in.File)},
		Position:     pos,
		NewName:      in.NewName,
	}

	result, err := client.Call(ctx, "textDocument/rename", params)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	var edit WorkspaceEdit
	if err := json.Unmarshal(result, &edit); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
	}

	return tools.ToolResult{Content: formatWorkspaceEdit(edit)}, nil
}

// --- lsp_completions ---

type completionsInput struct {
	positionInput
	MaxResults int `json:"max_results,omitempty"`
}

// NewCompletionsTool returns a tool that gets code completions at a position.
func NewCompletionsTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_completions",
		description: "Get code completion suggestions at the given position. Useful for verifying that a symbol exists before using it.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"},
				"max_results": {"type": "integer", "description": "Maximum results to return (default: 50)"}
			},
			"required": ["file", "line", "column"]
		}`),
		run: runCompletions,
	}
}

func runCompletions(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in completionsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(in.File)},
		Position:     pos,
	}

	result, err := client.Call(ctx, "textDocument/completion", params)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	// Result can be CompletionList or []CompletionItem.
	var items []CompletionItem
	var list CompletionList
	if err := json.Unmarshal(result, &list); err == nil {
		items = list.Items
	} else if err := json.Unmarshal(result, &items); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
	}

	if len(items) == 0 {
		return tools.ToolResult{Content: "no completions available"}, nil
	}

	summary := m.summarizer.SummarizeCompletions(items, in.MaxResults)
	return tools.ToolResult{Content: summary.Text}, nil
}

// --- lsp_code_action ---

// NewCodeActionTool returns a tool that gets available code actions at a position.
func NewCodeActionTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_code_action",
		description: "Get available quick fixes and refactoring actions at the given position (e.g., auto-imports, extract function, organize imports).",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"}
			},
			"required": ["file", "line", "column"]
		}`),
		run: runCodeAction,
	}
}

func runCodeAction(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in positionInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	uri := pathToURI(in.File)
	params := CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range:        Range{Start: pos, End: pos},
		Context: CodeActionContext{
			Diagnostics: m.DiagnosticsFor(uri, true),
		},
	}

	result, err := client.Call(ctx, "textDocument/codeAction", params)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	var actions []CodeAction
	if err := json.Unmarshal(result, &actions); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
	}

	if len(actions) == 0 {
		return tools.ToolResult{Content: "no code actions available"}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d code actions available:\n\n", len(actions))
	for i, action := range actions {
		kind := ""
		if action.Kind != "" {
			kind = fmt.Sprintf(" [%s]", action.Kind)
		}
		fmt.Fprintf(&sb, "%d. %s%s\n", i+1, action.Title, kind)
	}

	return tools.ToolResult{Content: sb.String()}, nil
}

// --- lsp_symbols ---

type symbolsInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// NewSymbolsTool returns a tool that searches for symbols across the workspace.
func NewSymbolsTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_symbols",
		description: "Search for symbols (functions, types, variables) across the workspace by name. More precise than grep for code navigation.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "Symbol name or prefix to search for"},
				"max_results": {"type": "integer", "description": "Maximum results to return (default: 50)"}
			},
			"required": ["query"]
		}`),
		run: runSymbols,
	}
}

func runSymbols(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in symbolsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	// Try all available servers — workspace/symbol is cross-file.
	available := m.registry.Available()
	if len(available) == 0 {
		return tools.ToolResult{Content: "no language servers available"}, nil
	}

	var allSymbols []SymbolInformation
	var serverErrors []string
	for _, lang := range available {
		client, _, err := m.ServerFor(ctx, lang)
		if err != nil {
			serverErrors = append(serverErrors, fmt.Sprintf("%s: %s", lang, err))
			continue
		}

		result, err := client.Call(ctx, "workspace/symbol", WorkspaceSymbolParams{Query: in.Query})
		if err != nil {
			serverErrors = append(serverErrors, fmt.Sprintf("%s: %s", lang, err))
			continue
		}

		var symbols []SymbolInformation
		if err := json.Unmarshal(result, &symbols); err == nil {
			allSymbols = append(allSymbols, symbols...)
		}
	}

	if len(allSymbols) == 0 {
		msg := fmt.Sprintf("no symbols matching %q found", in.Query)
		if len(serverErrors) > 0 {
			msg += fmt.Sprintf(" (errors: %s)", strings.Join(serverErrors, "; "))
		}
		return tools.ToolResult{Content: msg}, nil
	}

	summary := m.summarizer.SummarizeSymbols(allSymbols, in.MaxResults)
	return tools.ToolResult{Content: summary.Text}, nil
}

// --- lsp_call_hierarchy ---

type callHierarchyInput struct {
	positionInput
	Direction string `json:"direction"` // "incoming" or "outgoing"
}

// NewCallHierarchyTool returns a tool that shows incoming or outgoing calls.
func NewCallHierarchyTool(m *Manager) tools.Tool {
	return &lspTool{
		manager:     m,
		name:        "lsp_call_hierarchy",
		description: "Show incoming callers or outgoing callees of the function at the given position. Useful for impact analysis and understanding call chains.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file": {"type": "string", "description": "Path to the file"},
				"line": {"type": "integer", "description": "Line number (1-based)"},
				"column": {"type": "integer", "description": "Column number (1-based)"},
				"direction": {"type": "string", "enum": ["incoming", "outgoing"], "description": "Direction: incoming (who calls this?) or outgoing (what does this call?)"}
			},
			"required": ["file", "line", "column", "direction"]
		}`),
		run: runCallHierarchy,
	}
}

func runCallHierarchy(ctx context.Context, m *Manager, input json.RawMessage) (tools.ToolResult, error) {
	var in callHierarchyInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Direction != "incoming" && in.Direction != "outgoing" {
		return tools.ToolResult{Content: "direction must be 'incoming' or 'outgoing'", IsError: true}, nil
	}

	pos, err := in.validate()
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client, _, err := m.ServerForFile(ctx, in.File)
	if err != nil {
		return lspUnavailableResult(in.File, err), nil
	}

	if err := m.EnsureFileOpen(ctx, client, in.File); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("failed to open file: %s", err), IsError: true}, nil
	}

	// Step 1: Prepare — get the CallHierarchyItem for the position.
	prepareParams := CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(in.File)},
		Position:     pos,
	}

	prepareResult, err := client.Call(ctx, "textDocument/prepareCallHierarchy", prepareParams)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
	}

	var items []CallHierarchyItem
	if err := json.Unmarshal(prepareResult, &items); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
	}

	if len(items) == 0 {
		return tools.ToolResult{Content: "no call hierarchy available at this position"}, nil
	}

	item := items[0]

	// Step 2: Get incoming or outgoing calls.
	var sb strings.Builder
	if in.Direction == "incoming" {
		result, err := client.Call(ctx, "callHierarchy/incomingCalls", CallHierarchyIncomingCallsParams{Item: item})
		if err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
		}

		var calls []CallHierarchyIncomingCall
		if err := json.Unmarshal(result, &calls); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
		}

		if len(calls) == 0 {
			return tools.ToolResult{Content: fmt.Sprintf("no incoming calls to %s", item.Name)}, nil
		}

		fmt.Fprintf(&sb, "%d callers of %s:\n\n", len(calls), item.Name)
		for _, call := range calls {
			file := uriToPath(call.From.URI)
			fmt.Fprintf(&sb, "  %s (%s)  %s:%d\n", call.From.Name, call.From.Kind.String(), file, call.From.SelectionRange.Start.Line+1)
		}
	} else {
		result, err := client.Call(ctx, "callHierarchy/outgoingCalls", CallHierarchyOutgoingCallsParams{Item: item})
		if err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("LSP error: %s", err), IsError: true}, nil
		}

		var calls []CallHierarchyOutgoingCall
		if err := json.Unmarshal(result, &calls); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("unexpected response from server: %s", err), IsError: true}, nil
		}

		if len(calls) == 0 {
			return tools.ToolResult{Content: fmt.Sprintf("no outgoing calls from %s", item.Name)}, nil
		}

		fmt.Fprintf(&sb, "%d callees of %s:\n\n", len(calls), item.Name)
		for _, call := range calls {
			file := uriToPath(call.To.URI)
			fmt.Fprintf(&sb, "  %s (%s)  %s:%d\n", call.To.Name, call.To.Kind.String(), file, call.To.SelectionRange.Start.Line+1)
		}
	}

	return tools.ToolResult{Content: sb.String()}, nil
}

// --- helpers ---

// lspUnavailableResult returns a graceful error result when no LSP server is available.
func lspUnavailableResult(file string, err error) tools.ToolResult {
	return tools.ToolResult{
		Content: fmt.Sprintf("LSP not available for %s: %s", file, err),
		IsError: true,
	}
}

// formatWorkspaceEdit formats a workspace edit as a human-readable summary.
func formatWorkspaceEdit(edit WorkspaceEdit) string {
	if len(edit.Changes) == 0 {
		return "no changes"
	}

	var sb strings.Builder
	totalEdits := 0
	for uri, edits := range edit.Changes {
		file := uriToPath(uri)
		fmt.Fprintf(&sb, "%s (%d edits):\n", file, len(edits))
		for _, e := range edits {
			fmt.Fprintf(&sb, "  line %d:%d → %q\n", e.Range.Start.Line+1, e.Range.Start.Character+1, e.NewText)
		}
		totalEdits += len(edits)
	}
	fmt.Fprintf(&sb, "\nTotal: %d edits across %d files\n", totalEdits, len(edit.Changes))
	return sb.String()
}
