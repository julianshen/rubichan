package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/julianshen/rubichan/internal/parser"
)

// Config holds all pipeline configuration.
type Config struct {
	Dir         string
	OutputDir   string
	Format      string // raw-md, hugo, docusaurus
	DiagramFmt  string // mermaid
	Concurrency int    // parallel LLM calls
}

// osSourceReader reads files from the filesystem relative to a base directory.
type osSourceReader struct {
	baseDir string
}

func (r *osSourceReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(r.baseDir, path))
}

// Run executes the full wiki pipeline: scan -> chunk -> analyze -> diagrams -> assemble -> render.
func Run(ctx context.Context, cfg Config, llm LLMCompleter, p *parser.Parser) error {
	// Stage 1: Scan
	fmt.Fprintf(os.Stderr, "wiki: scanning %s...\n", cfg.Dir)
	files, err := Scan(ctx, cfg.Dir, p)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	// Stage 2: Chunk
	fmt.Fprintf(os.Stderr, "wiki: chunking %d files...\n", len(files))
	reader := &osSourceReader{baseDir: cfg.Dir}
	chunks, err := ChunkFiles(files, reader, DefaultChunkerConfig())
	if err != nil {
		return fmt.Errorf("chunk: %w", err)
	}

	// Stage 3: Analyze
	fmt.Fprintf(os.Stderr, "wiki: analyzing %d chunks...\n", len(chunks))
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	analyzerCfg := AnalyzerConfig{Concurrency: concurrency}
	analysis, err := Analyze(ctx, chunks, llm, analyzerCfg)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	// Stage 4: Diagrams
	fmt.Fprintf(os.Stderr, "wiki: generating diagrams...\n")
	diagrams, err := GenerateDiagrams(ctx, files, analysis, llm, DiagramConfig{Format: cfg.DiagramFmt})
	if err != nil {
		return fmt.Errorf("diagrams: %w", err)
	}

	// Stage 5: Assemble
	fmt.Fprintf(os.Stderr, "wiki: assembling documents...\n")
	documents, err := Assemble(analysis, diagrams, nil)
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}

	// Stage 6: Render
	fmt.Fprintf(os.Stderr, "wiki: rendering %d documents to %s...\n", len(documents), cfg.OutputDir)
	if err := Render(documents, RendererConfig{Format: cfg.Format, OutputDir: cfg.OutputDir}); err != nil {
		return fmt.Errorf("render: %w", err)
	}

	fmt.Fprintf(os.Stderr, "wiki: done.\n")
	return nil
}
