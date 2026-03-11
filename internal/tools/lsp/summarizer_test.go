package lsp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSummarizeLocationsNoTruncation(t *testing.T) {
	s := DefaultSummarizer()
	locs := []Location{
		{URI: "file:///main.go", Range: Range{Start: Position{Line: 10, Character: 5}}},
		{URI: "file:///util.go", Range: Range{Start: Position{Line: 20, Character: 0}}},
	}

	result := s.SummarizeLocations(locs, 0)
	assert.Equal(t, 2, result.Total)
	assert.Equal(t, 2, result.Shown)
	assert.False(t, result.Truncated)
	assert.Contains(t, result.Text, "main.go:11:6")
	assert.Contains(t, result.Text, "util.go:21:1")
}

func TestSummarizeLocationsTruncated(t *testing.T) {
	s := DefaultSummarizer()
	var locs []Location
	for i := 0; i < 100; i++ {
		locs = append(locs, Location{
			URI:   fmt.Sprintf("file:///file%d.go", i%10),
			Range: Range{Start: Position{Line: i, Character: 0}},
		})
	}

	result := s.SummarizeLocations(locs, 5)
	assert.Equal(t, 100, result.Total)
	assert.Equal(t, 5, result.Shown)
	assert.True(t, result.Truncated)
	assert.Contains(t, result.Text, "100 references across")
	assert.Contains(t, result.Text, "95 more references not shown")
}

func TestSummarizeCompletionsNoTruncation(t *testing.T) {
	s := DefaultSummarizer()
	items := []CompletionItem{
		{Label: "Println", Detail: "func(a ...any)"},
		{Label: "Printf", Detail: "func(format string, a ...any)"},
	}

	result := s.SummarizeCompletions(items, 0)
	assert.Equal(t, 2, result.Total)
	assert.Equal(t, 2, result.Shown)
	assert.False(t, result.Truncated)
	assert.Contains(t, result.Text, "Println")
	assert.Contains(t, result.Text, "Printf")
}

func TestSummarizeCompletionsTruncated(t *testing.T) {
	s := DefaultSummarizer()
	var items []CompletionItem
	for i := 0; i < 100; i++ {
		items = append(items, CompletionItem{Label: fmt.Sprintf("Item%d", i)})
	}

	result := s.SummarizeCompletions(items, 10)
	assert.Equal(t, 100, result.Total)
	assert.Equal(t, 10, result.Shown)
	assert.True(t, result.Truncated)
	assert.Contains(t, result.Text, "Showing 10 of 100")
	assert.Contains(t, result.Text, "90 more completions not shown")
}

func TestSummarizeSymbolsNoTruncation(t *testing.T) {
	s := DefaultSummarizer()
	symbols := []SymbolInformation{
		{Name: "HandleRequest", Kind: SymbolKindFunction, Location: Location{URI: "file:///server.go", Range: Range{Start: Position{Line: 10}}}},
		{Name: "Server", Kind: SymbolKindStruct, Location: Location{URI: "file:///server.go", Range: Range{Start: Position{Line: 5}}}},
	}

	result := s.SummarizeSymbols(symbols, 0)
	assert.Equal(t, 2, result.Total)
	assert.False(t, result.Truncated)
	assert.Contains(t, result.Text, "[function] HandleRequest")
	assert.Contains(t, result.Text, "[struct] Server")
}

func TestSummarizeSymbolsTruncated(t *testing.T) {
	s := DefaultSummarizer()
	var symbols []SymbolInformation
	for i := 0; i < 80; i++ {
		symbols = append(symbols, SymbolInformation{
			Name: fmt.Sprintf("Func%d", i), Kind: SymbolKindFunction,
			Location: Location{URI: "file:///file.go", Range: Range{Start: Position{Line: i}}},
		})
	}

	result := s.SummarizeSymbols(symbols, 20)
	assert.Equal(t, 80, result.Total)
	assert.Equal(t, 20, result.Shown)
	assert.True(t, result.Truncated)
	assert.Contains(t, result.Text, "80 symbols found")
}

func TestSummarizeDiagnosticsNoTruncation(t *testing.T) {
	s := DefaultSummarizer()
	diags := []Diagnostic{
		{Range: Range{Start: Position{Line: 5}}, Severity: SeverityError, Message: "undefined: foo"},
		{Range: Range{Start: Position{Line: 10}}, Severity: SeverityWarning, Message: "unused var"},
	}

	result := s.SummarizeDiagnostics(diags, 0)
	assert.Equal(t, 2, result.Total)
	assert.False(t, result.Truncated)
	assert.Contains(t, result.Text, "undefined: foo")
	assert.Contains(t, result.Text, "unused var")
}

func TestSummarizeDiagnosticsErrorsPrioritized(t *testing.T) {
	s := DefaultSummarizer()
	var diags []Diagnostic
	// 5 errors + 30 warnings.
	for i := 0; i < 5; i++ {
		diags = append(diags, Diagnostic{
			Range: Range{Start: Position{Line: i}}, Severity: SeverityError,
			Message: fmt.Sprintf("error %d", i),
		})
	}
	for i := 0; i < 30; i++ {
		diags = append(diags, Diagnostic{
			Range: Range{Start: Position{Line: i + 10}}, Severity: SeverityWarning,
			Message: fmt.Sprintf("warning %d", i),
		})
	}

	result := s.SummarizeDiagnostics(diags, 10)
	assert.Equal(t, 35, result.Total)
	assert.Equal(t, 10, result.Shown)
	assert.True(t, result.Truncated)
	// All 5 errors should be shown.
	for i := 0; i < 5; i++ {
		assert.Contains(t, result.Text, fmt.Sprintf("error %d", i))
	}
	// Only 5 warnings should fit in the budget.
	assert.Contains(t, result.Text, "Warnings/Info (5 of 30)")
}

func TestFormatDiagnosticWithSource(t *testing.T) {
	d := Diagnostic{
		Range:    Range{Start: Position{Line: 5, Character: 10}},
		Severity: SeverityError,
		Message:  "undefined: foo",
		Source:   "compiler",
	}
	text := formatDiagnostic(d)
	assert.Contains(t, text, "error")
	assert.Contains(t, text, "undefined: foo")
	assert.Contains(t, text, "(compiler)")
	assert.Contains(t, text, "line 6:11")
}

func TestFormatDiagnosticWithoutSource(t *testing.T) {
	d := Diagnostic{
		Range:    Range{Start: Position{Line: 0, Character: 0}},
		Severity: SeverityWarning,
		Message:  "unused variable",
	}
	text := formatDiagnostic(d)
	assert.Contains(t, text, "warning")
	assert.Contains(t, text, "unused variable")
	assert.NotContains(t, text, "()")
}

func TestSummarizeDiagnosticsAllErrors(t *testing.T) {
	s := DefaultSummarizer()
	var diags []Diagnostic
	for i := 0; i < 25; i++ {
		diags = append(diags, Diagnostic{
			Range:    Range{Start: Position{Line: i}},
			Severity: SeverityError,
			Message:  fmt.Sprintf("error %d", i),
		})
	}

	result := s.SummarizeDiagnostics(diags, 10)
	// All errors should be shown even if > maxItems.
	assert.Equal(t, 25, result.Shown)
	assert.False(t, result.Truncated)
}

func TestURIToPathInvalid(t *testing.T) {
	// Non-file URI should be returned as-is.
	assert.Equal(t, "https://example.com", uriToPath("https://example.com"))
}

func TestURIToPath(t *testing.T) {
	assert.Equal(t, "/home/user/main.go", uriToPath("file:///home/user/main.go"))
	assert.Equal(t, "relative.go", uriToPath("relative.go"))
}

func TestPathToURI(t *testing.T) {
	uri := pathToURI("/home/user/main.go")
	assert.Equal(t, "file:///home/user/main.go", uri)
}
