package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

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

func TestRunPipelineIntegration(t *testing.T) {
	// Full integration test: create a multi-file project, run pipeline,
	// verify the complete output structure.
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)

	// Create a small multi-module project.
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "cmd"), 0o755))
	writeFile(t, filepath.Join(srcDir, "cmd", "main.go"), `package main

import "fmt"

func main() {
    fmt.Println("hello")
}

func greet(name string) string {
    return "Hello, " + name
}
`)
	gitAdd(t, srcDir, "cmd/main.go")

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "pkg", "lib"), 0o755))
	writeFile(t, filepath.Join(srcDir, "pkg", "lib", "util.go"), `package lib

import "strings"

func ToUpper(s string) string {
    return strings.ToUpper(s)
}
`)
	gitAdd(t, srcDir, "pkg/lib/util.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{responses: map[string]string{
		"Analyze the following source code": "Summary: Utility functions\nKeyTypes: none\nPatterns: Functional\nConcerns: none",
		"identify key abstractions":         "Architecture: Layered architecture with cmd and lib modules.\nKeyAbstractions: Main entry point and utility library",
		"suggest improvements":              "1. Add tests\n2. Add documentation",
		"sequenceDiagram":                   "sequenceDiagram\n  Client->>Server: Request",
	}}

	p := parser.NewParser()

	err := Run(context.Background(), Config{
		Dir:         srcDir,
		OutputDir:   outDir,
		Format:      "raw-md",
		DiagramFmt:  "mermaid",
		Concurrency: 2,
	}, llm, p)
	require.NoError(t, err)

	// Verify output structure.
	assertFileExists(t, filepath.Join(outDir, "_index.md"))
	assertFileExists(t, filepath.Join(outDir, "architecture", "overview.md"))
	assertFileExists(t, filepath.Join(outDir, "architecture", "dependencies.md"))
	assertFileExists(t, filepath.Join(outDir, "architecture", "data-flow.md"))
	assertFileExists(t, filepath.Join(outDir, "modules", "_index.md"))
	assertFileExists(t, filepath.Join(outDir, "code-structure", "overview.md"))
	assertFileExists(t, filepath.Join(outDir, "security", "overview.md"))
	assertFileExists(t, filepath.Join(outDir, "suggestions", "improvements.md"))

	// Verify content quality.
	indexContent := readTestFile(t, filepath.Join(outDir, "_index.md"))
	assert.Contains(t, indexContent, "Project Overview")

	sugContent := readTestFile(t, filepath.Join(outDir, "suggestions", "improvements.md"))
	assert.Contains(t, sugContent, "Add tests")
}
