package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"text/template"

	"golang.org/x/sync/errgroup"
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

// Analyze runs a three-pass LLM analysis over the given chunks.
// Pass 1: concurrent per-module summarization.
// Pass 2: cross-cutting architecture synthesis.
// Pass 3: improvement suggestions.
func Analyze(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error) {
	if len(chunks) == 0 {
		return &AnalysisResult{}, nil
	}

	// Pass 1: per-module summarization (concurrent).
	modules, err := analyzeModules(ctx, chunks, llm, cfg)
	if err != nil {
		return nil, fmt.Errorf("pass 1 (module analysis): %w", err)
	}

	// Build concatenated summaries for passes 2 and 3.
	summariesText := buildSummariesText(modules)

	// Pass 2: architecture synthesis.
	architecture, keyAbstractions := synthesizeArchitecture(ctx, summariesText, llm)

	// Pass 3: suggestions.
	suggestions := generateSuggestions(ctx, architecture, summariesText, llm)

	return &AnalysisResult{
		Modules:         modules,
		Architecture:    architecture,
		KeyAbstractions: keyAbstractions,
		Suggestions:     suggestions,
	}, nil
}

// analyzeModules runs pass 1: concurrent per-module summarization.
func analyzeModules(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) ([]ModuleAnalysis, error) {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	var mu sync.Mutex
	var results []ModuleAnalysis

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, chunk := range chunks {
		chunk := chunk // capture loop variable
		g.Go(func() error {
			analysis, err := analyzeModule(ctx, chunk, llm)
			if err != nil {
				log.Printf("WARNING: module %q analysis failed: %v", chunk.Module, err)
				return nil // non-fatal: skip this module
			}
			mu.Lock()
			results = append(results, analysis)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

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
// On LLM failure, returns fallback text instead of failing.
func synthesizeArchitecture(ctx context.Context, summaries string, llm LLMCompleter) (architecture, keyAbstractions string) {
	var buf bytes.Buffer
	err := architectureTmpl.Execute(&buf, struct {
		Summaries string
	}{
		Summaries: summaries,
	})
	if err != nil {
		log.Printf("WARNING: architecture prompt rendering failed: %v", err)
		return "Architecture synthesis unavailable.", ""
	}

	response, err := llm.Complete(ctx, buf.String())
	if err != nil {
		log.Printf("WARNING: architecture synthesis failed: %v", err)
		return "Architecture synthesis unavailable.", ""
	}

	return parseArchitectureResponse(response)
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

// generateSuggestions runs pass 3: improvement suggestions.
// On LLM failure, returns nil instead of failing the pipeline.
func generateSuggestions(ctx context.Context, architecture, summaries string, llm LLMCompleter) []string {
	var buf bytes.Buffer
	err := suggestionsTmpl.Execute(&buf, struct {
		Architecture string
		Summaries    string
	}{
		Architecture: architecture,
		Summaries:    summaries,
	})
	if err != nil {
		log.Printf("WARNING: suggestions prompt rendering failed: %v", err)
		return nil
	}

	response, err := llm.Complete(ctx, buf.String())
	if err != nil {
		log.Printf("WARNING: suggestions generation failed: %v", err)
		return nil
	}

	return parseSuggestions(response)
}

// parseSuggestions splits the LLM response into individual suggestion lines.
func parseSuggestions(response string) []string {
	var suggestions []string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			suggestions = append(suggestions, line)
		}
	}
	return suggestions
}
