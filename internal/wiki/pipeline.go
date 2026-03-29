package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/security"
)

// Config holds all pipeline configuration.
type Config struct {
	Dir              string
	OutputDir        string
	Format           string // raw-md, hugo, docusaurus
	DiagramFmt       string // mermaid
	Concurrency      int    // parallel LLM calls
	SecurityFindings []security.Finding

	// ProgressFunc, when non-nil, receives progress updates for each pipeline stage.
	// When nil, progress is written to stderr instead.
	ProgressFunc func(stage string, current, total int)
}

// progress reports pipeline progress via ProgressFunc if set, otherwise writes to stderr.
func (c *Config) progress(stage string, current, total int, fallbackMsg string) {
	if c.ProgressFunc != nil {
		c.ProgressFunc(stage, current, total)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", fallbackMsg)
}

// osSourceReader reads files from the filesystem relative to a base directory.
type osSourceReader struct {
	baseDir string
}

func (r *osSourceReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(r.baseDir, path))
}

// Run executes the full wiki pipeline: scan -> chunk -> analyze -> diagrams -> assemble -> render.
// It returns a WikiResult summarising what was generated.
func Run(ctx context.Context, cfg Config, llm LLMCompleter, p *parser.Parser) (*WikiResult, error) {
	start := time.Now()

	// Stage 1: Scan
	cfg.progress("scanning", 0, 0, fmt.Sprintf("wiki: scanning %s...", cfg.Dir))
	files, err := Scan(ctx, cfg.Dir, p)
	if err != nil {
		if isContextCancellation(err) {
			return nil, err
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 2: Scan API patterns
	cfg.progress("scanning-api", 0, len(files), fmt.Sprintf("wiki: scanning API patterns in %d files...", len(files)))
	apiPatterns := ScanAPIPatterns(files, os.ReadFile)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 3: Chunk
	cfg.progress("chunking", 0, len(files), fmt.Sprintf("wiki: chunking %d files...", len(files)))
	reader := &osSourceReader{baseDir: cfg.Dir}
	chunks, err := ChunkFiles(files, reader, DefaultChunkerConfig())
	if err != nil {
		return nil, fmt.Errorf("chunk: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 3: Analyze (base pass)
	cfg.progress("analyzing", 0, len(chunks), fmt.Sprintf("wiki: analyzing %d chunks...", len(chunks)))
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	analyzerCfg := AnalyzerConfig{Concurrency: concurrency}
	analysis, err := AnalyzeBase(ctx, chunks, llm, analyzerCfg)
	if err != nil {
		if isContextCancellation(err) {
			return nil, err
		}
		return nil, fmt.Errorf("analyze: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 3b: Specialized analyzers
	cfg.progress("specialized-analysis", 0, 0, "wiki: running specialized analyzers...")
	specializedAnalyzers := []SpecializedAnalyzer{
		NewSuggestionAnalyzer(llm),
		NewAPIAnalyzer(llm),
		NewSecurityAnalyzer(llm),
		NewDependencyAnalyzer(llm, cfg.Dir),
	}
	analyzerInput := AnalyzerInput{
		Chunks:         chunks,
		Files:          files,
		ModuleAnalyses: analysis.Modules,
		Architecture:   analysis.Architecture,
		APIPatterns:    apiPatterns,
	}
	extraDocs, extraDiagrams, err := RunSpecializedAnalyzers(ctx, specializedAnalyzers, analyzerInput)
	if err != nil {
		if isContextCancellation(err) {
			return nil, err
		}
		return nil, fmt.Errorf("specialized analyzers: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 4: Diagrams
	cfg.progress("diagrams", 0, 0, "wiki: generating diagrams...")
	diagramFmt := cfg.DiagramFmt
	if diagramFmt == "" {
		diagramFmt = "mermaid"
	}
	diagrams, err := GenerateDiagrams(ctx, files, analysis, llm, DiagramConfig{Format: diagramFmt})
	if err != nil {
		if isContextCancellation(err) {
			return nil, err
		}
		return nil, fmt.Errorf("diagrams: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 5: Assemble
	cfg.progress("assembling", 0, 0, "wiki: assembling documents...")
	diagrams = append(diagrams, extraDiagrams...)
	documents, err := Assemble(analysis, diagrams, nil, cfg.SecurityFindings, files)
	if err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}
	documents = append(documents, extraDocs...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stage 6: Render
	cfg.progress("rendering", 0, len(documents), fmt.Sprintf("wiki: rendering %d documents to %s...", len(documents), cfg.OutputDir))
	if err := Render(documents, RendererConfig{Format: cfg.Format, OutputDir: cfg.OutputDir}); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg.progress("done", 0, 0, "wiki: done.")

	format := cfg.Format
	if format == "" {
		format = "raw-md"
	}
	result := &WikiResult{
		OutputDir:  cfg.OutputDir,
		Format:     format,
		Documents:  len(documents),
		Diagrams:   len(diagrams),
		DurationMs: time.Since(start).Milliseconds(),
	}
	return result, nil
}
