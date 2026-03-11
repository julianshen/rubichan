package lsp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SummarizedResult holds a truncated LSP response formatted for LLM consumption.
type SummarizedResult struct {
	Text      string // formatted text for LLM
	Total     int    // total items before truncation
	Shown     int    // items included in output
	Truncated bool   // whether truncation occurred
}

// Summarizer controls token budget for large LSP responses.
type Summarizer struct {
	MaxReferences  int // default threshold for reference lists
	MaxCompletions int // default threshold for completion lists
	MaxSymbols     int // default threshold for workspace symbols
	MaxDiagnostics int // default threshold for diagnostic lists
}

// DefaultSummarizer returns a summarizer with default thresholds.
func DefaultSummarizer() *Summarizer {
	return &Summarizer{
		MaxReferences:  50,
		MaxCompletions: 50,
		MaxSymbols:     50,
		MaxDiagnostics: 20,
	}
}

// SummarizeLocations truncates a location list, grouping by file.
func (s *Summarizer) SummarizeLocations(locs []Location, maxItems int) SummarizedResult {
	if maxItems <= 0 {
		maxItems = s.MaxReferences
	}

	total := len(locs)
	if total <= maxItems {
		return SummarizedResult{
			Text:  formatLocations(locs),
			Total: total,
			Shown: total,
		}
	}

	// Group by file, show count per file, then list first few per file.
	byFile := groupLocationsByFile(locs)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d references across %d files (showing first %d)\n\n", total, len(byFile), maxItems)

	shown := 0
	for _, fg := range byFile {
		if shown >= maxItems {
			break
		}
		fmt.Fprintf(&sb, "%s (%d references):\n", fg.file, len(fg.locs))
		for _, loc := range fg.locs {
			if shown >= maxItems {
				break
			}
			fmt.Fprintf(&sb, "  line %d:%d\n", loc.Range.Start.Line+1, loc.Range.Start.Character+1)
			shown++
		}
	}

	if shown < total {
		fmt.Fprintf(&sb, "\n... %d more references not shown\n", total-shown)
	}

	return SummarizedResult{
		Text:      sb.String(),
		Total:     total,
		Shown:     shown,
		Truncated: true,
	}
}

// SummarizeCompletions truncates a completion list.
func (s *Summarizer) SummarizeCompletions(items []CompletionItem, maxItems int) SummarizedResult {
	if maxItems <= 0 {
		maxItems = s.MaxCompletions
	}

	total := len(items)
	if total <= maxItems {
		return SummarizedResult{
			Text:  formatCompletions(items),
			Total: total,
			Shown: total,
		}
	}

	shown := items[:maxItems]
	var sb strings.Builder
	fmt.Fprintf(&sb, "Showing %d of %d completions\n\n", maxItems, total)
	sb.WriteString(formatCompletions(shown))
	fmt.Fprintf(&sb, "\n... %d more completions not shown\n", total-maxItems)

	return SummarizedResult{
		Text:      sb.String(),
		Total:     total,
		Shown:     maxItems,
		Truncated: true,
	}
}

// SummarizeSymbols truncates a workspace symbol list, grouping by kind.
func (s *Summarizer) SummarizeSymbols(symbols []SymbolInformation, maxItems int) SummarizedResult {
	if maxItems <= 0 {
		maxItems = s.MaxSymbols
	}

	total := len(symbols)
	if total <= maxItems {
		return SummarizedResult{
			Text:  formatSymbols(symbols),
			Total: total,
			Shown: total,
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d symbols found (showing first %d)\n\n", total, maxItems)
	sb.WriteString(formatSymbols(symbols[:maxItems]))
	fmt.Fprintf(&sb, "\n... %d more symbols not shown\n", total-maxItems)

	return SummarizedResult{
		Text:      sb.String(),
		Total:     total,
		Shown:     maxItems,
		Truncated: true,
	}
}

// SummarizeDiagnostics truncates a diagnostic list, always showing all errors.
func (s *Summarizer) SummarizeDiagnostics(diags []Diagnostic, maxItems int) SummarizedResult {
	if maxItems <= 0 {
		maxItems = s.MaxDiagnostics
	}

	total := len(diags)
	if total <= maxItems {
		return SummarizedResult{
			Text:  formatDiagnostics(diags),
			Total: total,
			Shown: total,
		}
	}

	// Separate errors from non-errors. Always show all errors.
	var errors, others []Diagnostic
	for _, d := range diags {
		if d.Severity == SeverityError {
			errors = append(errors, d)
		} else {
			others = append(others, d)
		}
	}

	var sb strings.Builder
	shown := 0

	if len(errors) > 0 {
		fmt.Fprintf(&sb, "Errors (%d):\n", len(errors))
		for _, d := range errors {
			sb.WriteString(formatDiagnostic(d))
			shown++
		}
		sb.WriteString("\n")
	}

	remaining := maxItems - shown
	if remaining > 0 && len(others) > 0 {
		if remaining < len(others) {
			fmt.Fprintf(&sb, "Warnings/Info (%d of %d):\n", remaining, len(others))
			for _, d := range others[:remaining] {
				sb.WriteString(formatDiagnostic(d))
				shown++
			}
		} else {
			fmt.Fprintf(&sb, "Warnings/Info (%d):\n", len(others))
			for _, d := range others {
				sb.WriteString(formatDiagnostic(d))
				shown++
			}
		}
	}

	if shown < total {
		fmt.Fprintf(&sb, "\n... %d more diagnostics not shown\n", total-shown)
	}

	return SummarizedResult{
		Text:      sb.String(),
		Total:     total,
		Shown:     shown,
		Truncated: shown < total,
	}
}

// --- formatting helpers ---

type fileGroup struct {
	file string
	locs []Location
}

func groupLocationsByFile(locs []Location) []fileGroup {
	groups := make(map[string][]Location)
	var order []string
	for _, loc := range locs {
		file := uriToPath(loc.URI)
		if _, exists := groups[file]; !exists {
			order = append(order, file)
		}
		groups[file] = append(groups[file], loc)
	}
	result := make([]fileGroup, len(order))
	for i, file := range order {
		result[i] = fileGroup{file: file, locs: groups[file]}
	}
	return result
}

func formatLocations(locs []Location) string {
	var sb strings.Builder
	for _, loc := range locs {
		fmt.Fprintf(&sb, "%s:%d:%d\n", uriToPath(loc.URI), loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return sb.String()
}

func formatCompletions(items []CompletionItem) string {
	var sb strings.Builder
	for _, item := range items {
		if item.Detail != "" {
			fmt.Fprintf(&sb, "%s  %s\n", item.Label, item.Detail)
		} else {
			fmt.Fprintf(&sb, "%s\n", item.Label)
		}
	}
	return sb.String()
}

func formatSymbols(symbols []SymbolInformation) string {
	// Sort by kind for readability.
	sorted := make([]SymbolInformation, len(symbols))
	copy(sorted, symbols)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		return sorted[i].Name < sorted[j].Name
	})

	var sb strings.Builder
	for _, sym := range sorted {
		file := uriToPath(sym.Location.URI)
		fmt.Fprintf(&sb, "[%s] %s  %s:%d\n", sym.Kind.String(), sym.Name, file, sym.Location.Range.Start.Line+1)
	}
	return sb.String()
}

func formatDiagnostics(diags []Diagnostic) string {
	var sb strings.Builder
	for _, d := range diags {
		sb.WriteString(formatDiagnostic(d))
	}
	return sb.String()
}

func formatDiagnostic(d Diagnostic) string {
	source := ""
	if d.Source != "" {
		source = fmt.Sprintf(" (%s)", d.Source)
	}
	return fmt.Sprintf("  %s line %d:%d: %s%s\n",
		d.Severity.String(),
		d.Range.Start.Line+1,
		d.Range.Start.Character+1,
		d.Message,
		source,
	)
}

// uriToPath converts a file URI to a filesystem path.
func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		path := strings.TrimPrefix(uri, "file://")
		return filepath.Clean(path)
	}
	return uri
}

// pathToURI converts a filesystem path to a file URI.
func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file://" + abs
}
