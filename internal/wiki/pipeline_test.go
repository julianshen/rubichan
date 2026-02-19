package wiki

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestRunFullPipeline(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	src := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}
`
	writeFile(t, filepath.Join(dir, "main.go"), src)
	gitAdd(t, dir, "main.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"root": "Summary: Main module\nKeyTypes: none\nPatterns: none\nConcerns: none",
		},
	}

	cfg := Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      "raw-md",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}

	p := parser.NewParser()
	err := Run(context.Background(), cfg, llm, p)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(outDir, "_index.md"))
}

func TestRunPipelineWithHugoFormat(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	src := `package main

func Main() {}
`
	writeFile(t, filepath.Join(dir, "main.go"), src)
	gitAdd(t, dir, "main.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"root": "Summary: Main module\nKeyTypes: none\nPatterns: none\nConcerns: none",
		},
	}

	cfg := Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      "hugo",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}

	p := parser.NewParser()
	err := Run(context.Background(), cfg, llm, p)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(outDir, "config.toml"))
}

func TestRunPipelineWithDocusaurusFormat(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	src := `package main

func Main() {}
`
	writeFile(t, filepath.Join(dir, "main.go"), src)
	gitAdd(t, dir, "main.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"root": "Summary: Main module\nKeyTypes: none\nPatterns: none\nConcerns: none",
		},
	}

	cfg := Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      "docusaurus",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}

	p := parser.NewParser()
	err := Run(context.Background(), cfg, llm, p)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(outDir, "docusaurus.config.js"))
}

func TestRunPipelineEmptyDir(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	outDir := t.TempDir()

	llm := &mockLLMCompleter{
		responses: map[string]string{},
	}

	cfg := Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      "raw-md",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}

	p := parser.NewParser()
	err := Run(context.Background(), cfg, llm, p)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(outDir, "_index.md"))
}

func TestRunPipelineCancellation(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	outDir := t.TempDir()

	llm := &mockLLMCompleter{
		responses: map[string]string{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      "raw-md",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}

	p := parser.NewParser()

	// Should not panic — error is acceptable but no panic
	require.NotPanics(t, func() {
		_ = Run(ctx, cfg, llm, p)
	})
}
