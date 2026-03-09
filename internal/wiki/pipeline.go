package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
func Run(ctx context.Context, cfg Config, llm LLMCompleter, p *parser.Parser) error {
	// Stage 1: Scan
	cfg.progress("scanning", 0, 0, fmt.Sprintf("wiki: scanning %s...", cfg.Dir))
	files, err := Scan(ctx, cfg.Dir, p)
	if err != nil {
		if isContextCancellation(err) {
			return err
		}
		return fmt.Errorf("scan: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Stage 2: Chunk
	cfg.progress("chunking", 0, len(files), fmt.Sprintf("wiki: chunking %d files...", len(files)))
	reader := &osSourceReader{baseDir: cfg.Dir}
	chunks, err := ChunkFiles(files, reader, DefaultChunkerConfig())
	if err != nil {
		return fmt.Errorf("chunk: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Stage 3: Analyze
	cfg.progress("analyzing", 0, len(chunks), fmt.Sprintf("wiki: analyzing %d chunks...", len(chunks)))
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	analyzerCfg := AnalyzerConfig{Concurrency: concurrency}
	analysis, err := Analyze(ctx, chunks, llm, analyzerCfg)
	if err != nil {
		if isContextCancellation(err) {
			return err
		}
		return fmt.Errorf("analyze: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
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
			return err
		}
		return fmt.Errorf("diagrams: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Stage 5: Assemble
	cfg.progress("assembling", 0, 0, "wiki: assembling documents...")
	documents, err := Assemble(analysis, diagrams, nil, cfg.SecurityFindings)
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Stage 6: Render
	cfg.progress("rendering", 0, len(documents), fmt.Sprintf("wiki: rendering %d documents to %s...", len(documents), cfg.OutputDir))
	if err := Render(documents, RendererConfig{Format: cfg.Format, OutputDir: cfg.OutputDir}); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cfg.progress("done", 0, 0, "wiki: done.")
	return nil
}
