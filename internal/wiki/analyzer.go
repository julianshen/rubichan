package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/sourcegraph/conc/pool"
)

// LLMCompleter abstracts LLM completion for testability.
type LLMCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// AnalyzerConfig controls the analyzer behavior.
type AnalyzerConfig struct {
	Concurrency int // max concurrent LLM calls in pass 1
}

// DefaultAnalyzerConfig returns sensible defaults for analysis.
func DefaultAnalyzerConfig() AnalyzerConfig {
	return AnalyzerConfig{
		Concurrency: 5,
	}
}

// ---------- prompt templates ----------

var moduleSummaryTmpl = template.Must(template.New("module").Parse(
	`Analyze the following source code module "{{.Module}}" and provide a structured summary.

Source:
{{.Source}}

Respond in exactly this format:
Summary: <one-paragraph summary of the module>
KeyTypes: <comma-separated list of key types/interfaces>
Patterns: <comma-separated list of design patterns used>
Concerns: <comma-separated list of concerns or issues>`))

var architectureTmpl = template.Must(template.New("architecture").Parse(
	`Given the following module summaries, provide an architecture overview and identify key abstractions.

{{.Summaries}}

Respond in exactly this format:
Architecture: <paragraph describing the overall architecture>
KeyAbstractions: <paragraph describing key abstractions and how modules relate>`))

var suggestionsTmpl = template.Must(template.New("suggestions").Parse(
	`Given the following architecture overview and module summaries, suggest improvements.

Architecture:
{{.Architecture}}

Module Summaries:
{{.Summaries}}

List each suggestion on its own line, one per line.`))

// ---------- analysis ----------

// AnalyzeBase runs passes 1 and 2 only: per-module summarization and architecture
// synthesis. The returned AnalysisResult has Modules, Architecture, and
// KeyAbstractions populated; Suggestions is intentionally left nil.
// Use RunSpecializedAnalyzers to dispatch additional analysis passes.
func AnalyzeBase(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error) {
	if len(chunks) == 0 {
		return &AnalysisResult{}, nil
	}

	// Pass 1: per-module summarization (concurrent).
	modules, err := analyzeModules(ctx, chunks, llm, cfg)
	if err != nil {
		return nil, err
	}

	// Build concatenated summaries for pass 2.
	summariesText := buildSummariesText(modules)

	// Pass 2: architecture synthesis.
	architecture, keyAbstractions, err := synthesizeArchitecture(ctx, summariesText, llm)
	if err != nil {
		return nil, err
	}

	return &AnalysisResult{
		Modules:         modules,
		Architecture:    architecture,
		KeyAbstractions: keyAbstractions,
	}, nil
}

// RunSpecializedAnalyzers dispatches all provided analyzers concurrently and
// merges their Documents and Diagrams. A failing analyzer is non-fatal: its
// error is logged and the remaining results are still returned.
func RunSpecializedAnalyzers(ctx context.Context, analyzers []SpecializedAnalyzer, input AnalyzerInput) ([]Document, []Diagram, error) {
	var (
		mu    sync.Mutex
		docs  []Document
		diags []Diagram
		wg    sync.WaitGroup
	)

	for _, a := range analyzers {
		a := a // capture loop variable
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.Analyze(ctx, input)
			if err != nil {
				log.Printf("WARNING: specialized analyzer %q failed: %v", a.Name(), err)
				return
			}
			mu.Lock()
			docs = append(docs, out.Documents...)
			diags = append(diags, out.Diagrams...)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if ctx.Err() != nil {
		return docs, diags, ctx.Err()
	}

	// Sort for deterministic output across runs.
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	sort.Slice(diags, func(i, j int) bool { return diags[i].Title < diags[j].Title })

	return docs, diags, nil
}

// Analyze runs a three-pass LLM analysis over the given chunks.
// Pass 1: concurrent per-module summarization.
// Pass 2: cross-cutting architecture synthesis.
// Pass 3: improvement suggestions (via SuggestionAnalyzer).
//
// This is a compatibility wrapper. New callers should prefer AnalyzeBase +
// RunSpecializedAnalyzers to control which analyzers run.
func Analyze(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error) {
	result, err := AnalyzeBase(ctx, chunks, llm, cfg)
	if err != nil {
		return nil, err
	}

	input := AnalyzerInput{
		Chunks:         chunks,
		ModuleAnalyses: result.Modules,
		Architecture:   result.Architecture,
	}

	suggester := NewSuggestionAnalyzer(llm)
	docs, _, err := RunSpecializedAnalyzers(ctx, []SpecializedAnalyzer{suggester}, input)
	if err != nil {
		return nil, err
	}

	// Extract suggestion lines from the document produced by SuggestionAnalyzer.
	result.Suggestions = extractSuggestionsFromDocs(docs)
	return result, nil
}

// extractSuggestionsFromDocs reconstructs the []string suggestions from the
// Document that SuggestionAnalyzer emits. Each non-empty, non-heading line is
// treated as one suggestion, preserving backward compatibility with callers
// that expect result.Suggestions to be a string slice.
func extractSuggestionsFromDocs(docs []Document) []string {
	var suggestions []string
	for _, doc := range docs {
		for _, line := range strings.Split(doc.Content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			suggestions = append(suggestions, line)
		}
	}
	return suggestions
}

// analyzeModules runs pass 1: concurrent per-module summarization.
// Results are sorted by module name for deterministic output.
func analyzeModules(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) ([]ModuleAnalysis, error) {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	p := pool.New().WithMaxGoroutines(concurrency)

	var mu sync.Mutex
	var results []ModuleAnalysis

	for _, chunk := range chunks {
		chunk := chunk // capture loop variable
		p.Go(func() {
			analysis, err := analyzeModule(ctx, chunk, llm)
			if err != nil {
				if isContextCancellation(err) {
					return
				}
				log.Printf("WARNING: module %q analysis failed: %v", chunk.Module, err)
				return // non-fatal: skip this module
			}
			mu.Lock()
			results = append(results, analysis)
			mu.Unlock()
		})
	}

	p.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Sort for deterministic output regardless of goroutine scheduling.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Module < results[j].Module
	})

	return results, nil
}

// analyzeModule sends a single module chunk to the LLM and parses the response.
func analyzeModule(ctx context.Context, chunk Chunk, llm LLMCompleter) (ModuleAnalysis, error) {
	var buf bytes.Buffer
	err := moduleSummaryTmpl.Execute(&buf, struct {
		Module string
		Source string
	}{
		Module: chunk.Module,
		Source: string(chunk.Source),
	})
	if err != nil {
		return ModuleAnalysis{}, fmt.Errorf("rendering prompt: %w", err)
	}

	response, err := llm.Complete(ctx, buf.String())
	if err != nil {
		return ModuleAnalysis{}, fmt.Errorf("LLM completion: %w", err)
	}

	return parseModuleResponse(chunk.Module, response), nil
}

// parseModuleResponse parses the LLM response into a ModuleAnalysis.
func parseModuleResponse(module, response string) ModuleAnalysis {
	ma := ModuleAnalysis{Module: module}

	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Summary:"):
			ma.Summary = strings.TrimSpace(strings.TrimPrefix(line, "Summary:"))
		case strings.HasPrefix(line, "KeyTypes:"):
			ma.KeyTypes = strings.TrimSpace(strings.TrimPrefix(line, "KeyTypes:"))
		case strings.HasPrefix(line, "Patterns:"):
			ma.Patterns = strings.TrimSpace(strings.TrimPrefix(line, "Patterns:"))
		case strings.HasPrefix(line, "Concerns:"):
			ma.Concerns = strings.TrimSpace(strings.TrimPrefix(line, "Concerns:"))
		}
	}

	return ma
}

// buildSummariesText concatenates all module summaries into a single text block.
func buildSummariesText(modules []ModuleAnalysis) string {
	var buf strings.Builder
	for _, m := range modules {
		fmt.Fprintf(&buf, "## %s\n", m.Module)
		fmt.Fprintf(&buf, "Summary: %s\n", m.Summary)
		fmt.Fprintf(&buf, "KeyTypes: %s\n", m.KeyTypes)
		fmt.Fprintf(&buf, "Patterns: %s\n", m.Patterns)
		fmt.Fprintf(&buf, "Concerns: %s\n\n", m.Concerns)
	}
	return buf.String()
}

// synthesizeArchitecture runs pass 2: architecture synthesis.
// On non-context LLM failure, it returns fallback text instead of failing.
// Context cancellation errors are propagated to the caller.
func synthesizeArchitecture(ctx context.Context, summaries string, llm LLMCompleter) (architecture, keyAbstractions string, err error) {
	var buf bytes.Buffer
	err = architectureTmpl.Execute(&buf, struct {
		Summaries string
	}{
		Summaries: summaries,
	})
	if err != nil {
		log.Printf("WARNING: architecture prompt rendering failed: %v", err)
		return "Architecture synthesis unavailable.", "", nil
	}

	response, err := llm.Complete(ctx, buf.String())
	if err != nil {
		if isContextCancellation(err) {
			return "", "", err
		}
		log.Printf("WARNING: architecture synthesis failed: %v", err)
		return "Architecture synthesis unavailable.", "", nil
	}

	architecture, keyAbstractions = parseArchitectureResponse(response)
	return architecture, keyAbstractions, nil
}

// parseArchitectureResponse extracts Architecture and KeyAbstractions from LLM response.
func parseArchitectureResponse(response string) (architecture, keyAbstractions string) {
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Architecture:"):
			architecture = strings.TrimSpace(strings.TrimPrefix(line, "Architecture:"))
		case strings.HasPrefix(line, "KeyAbstractions:"):
			keyAbstractions = strings.TrimSpace(strings.TrimPrefix(line, "KeyAbstractions:"))
		}
	}

	// If the response didn't follow the expected format, use the whole response as architecture.
	if architecture == "" {
		architecture = strings.TrimSpace(response)
	}

	return architecture, keyAbstractions
}
