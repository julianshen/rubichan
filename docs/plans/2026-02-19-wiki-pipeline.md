# Wiki Pipeline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the full 6-stage wiki generator pipeline that analyzes a codebase and produces a static documentation site.

**Architecture:** Sequential pipeline where each stage runs to completion before the next begins. Data flows via typed Go structs defined in `internal/wiki/types.go`. Concurrency is internal to the analyzer stage (parallel LLM calls via `errgroup`). CLI entrypoint is `rubichan wiki [path]`.

**Tech Stack:** Go, tree-sitter parser (`internal/parser`), `integrations.LLMCompleter`, `text/template` for prompts, Mermaid for diagrams, Cobra for CLI.

---

### Task 1: Shared Data Types

**Files:**
- Create: `internal/wiki/types.go`
- Test: `internal/wiki/types_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/types_test.go
package wiki

import (
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/assert"
)

func TestScannedFileZeroValue(t *testing.T) {
	var sf ScannedFile
	assert.Empty(t, sf.Path)
	assert.Empty(t, sf.Language)
	assert.Empty(t, sf.Module)
	assert.Nil(t, sf.Functions)
	assert.Nil(t, sf.Imports)
	assert.Zero(t, sf.Size)
}

func TestChunkZeroValue(t *testing.T) {
	var c Chunk
	assert.Empty(t, c.Module)
	assert.Nil(t, c.Files)
	assert.Nil(t, c.Source)
}

func TestAnalysisResultZeroValue(t *testing.T) {
	var ar AnalysisResult
	assert.Nil(t, ar.Modules)
	assert.Empty(t, ar.Architecture)
	assert.Empty(t, ar.KeyAbstractions)
	assert.Nil(t, ar.Suggestions)
}

func TestDiagramTypes(t *testing.T) {
	d := Diagram{
		Title:   "Architecture Overview",
		Type:    "architecture",
		Content: "graph TD\n  A-->B",
	}
	assert.Equal(t, "architecture", d.Type)
	assert.Contains(t, d.Content, "graph TD")
}

func TestDocumentZeroValue(t *testing.T) {
	var doc Document
	assert.Empty(t, doc.Path)
	assert.Empty(t, doc.Title)
	assert.Empty(t, doc.Content)
}

func TestSkillWikiSectionZeroValue(t *testing.T) {
	var s SkillWikiSection
	assert.Empty(t, s.SkillName)
	assert.Empty(t, s.Title)
	assert.Empty(t, s.Content)
	assert.Nil(t, s.Diagrams)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestScannedFile -v`
Expected: FAIL — package does not exist yet

**Step 3: Write minimal implementation**

```go
// internal/wiki/types.go
package wiki

import "github.com/julianshen/rubichan/internal/parser"

// ScannedFile represents a source file discovered by the scanner stage.
type ScannedFile struct {
	Path      string
	Language  string
	Functions []parser.FunctionDef
	Imports   []string
	Size      int64
	Module    string // inferred package/module grouping
}

// Chunk groups related files for LLM analysis.
type Chunk struct {
	Module string
	Files  []ScannedFile
	Source []byte // concatenated structured summary for LLM context
}

// AnalysisResult holds the complete output of the LLM analyzer stage.
type AnalysisResult struct {
	Modules         []ModuleAnalysis
	Architecture    string // cross-cutting synthesis
	KeyAbstractions string
	Suggestions     []string
}

// ModuleAnalysis summarizes a single module from the LLM.
type ModuleAnalysis struct {
	Module   string
	Summary  string
	KeyTypes string
	Patterns string
	Concerns string
}

// Diagram holds a generated Mermaid diagram.
type Diagram struct {
	Title   string
	Type    string // "architecture", "dependency", "data-flow", "sequence"
	Content string // Mermaid source
}

// Document represents a single output page in the wiki.
type Document struct {
	Path    string // relative path in output (e.g., "modules/parser.md")
	Title   string
	Content string // Markdown
}

// SkillWikiSection holds a wiki contribution from a skill.
type SkillWikiSection struct {
	SkillName string
	Title     string
	Content   string
	Diagrams  []Diagram
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS (all 6 tests)

**Step 5: Commit**

```bash
git add internal/wiki/types.go internal/wiki/types_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline shared data types"
```

---

### Task 2: Codebase Scanner (Stage 1)

**Files:**
- Create: `internal/wiki/scanner.go`
- Test: `internal/wiki/scanner_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/scanner_test.go
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

func TestScanEmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Initialize a git repo so git ls-files works.
	initGitRepo(t, dir)

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestScanGoFiles(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create a Go file.
	goDir := filepath.Join(dir, "cmd")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	writeFile(t, filepath.Join(goDir, "main.go"), `package main

func main() {
	println("hello")
}
`)
	gitAdd(t, dir, "cmd/main.go")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 1)

	assert.Equal(t, "cmd/main.go", files[0].Path)
	assert.Equal(t, "go", files[0].Language)
	assert.Equal(t, "cmd", files[0].Module)
	require.Len(t, files[0].Functions, 1)
	assert.Equal(t, "main", files[0].Functions[0].Name)
}

func TestScanSkipsVendorAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create files in vendor/ and node_modules/ — these should be excluded
	// even if tracked by git.
	for _, skip := range []string{"vendor/lib.go", "node_modules/index.js"} {
		p := filepath.Join(dir, skip)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		writeFile(t, p, "package main\n")
		gitAdd(t, dir, skip)
	}

	// Create a normal file that should be included.
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	gitAdd(t, dir, "main.go")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "main.go", files[0].Path)
}

func TestScanUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "README.md"), "# Hello\n")
	gitAdd(t, dir, "README.md")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "README.md", files[0].Path)
	assert.Equal(t, "unknown", files[0].Language)
	assert.Nil(t, files[0].Functions)
}

func TestScanMultipleModules(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Two separate directories = two modules.
	for _, sub := range []string{"pkg/tools", "pkg/parser"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755))
		writeFile(t, filepath.Join(dir, sub, "lib.go"), "package "+filepath.Base(sub)+"\n")
		gitAdd(t, dir, sub+"/lib.go")
	}

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 2)

	modules := map[string]bool{}
	for _, f := range files {
		modules[f.Module] = true
	}
	assert.True(t, modules["pkg/tools"])
	assert.True(t, modules["pkg/parser"])
}

func TestScanFallbackWalkDir(t *testing.T) {
	// Create a directory that is NOT a git repo.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func hello() {}
`)

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "main.go", files[0].Path)
}

// --- test helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
}

func gitAdd(t *testing.T, dir, file string) {
	t.Helper()
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-m", "add "+file, "--allow-empty")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(out))
}
```

Note: Add `"os/exec"` to imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestScan -v`
Expected: FAIL — `Scan` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/scanner.go
package wiki

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/julianshen/rubichan/internal/parser"
)

// skipDirs contains directory names to exclude from scanning.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"build":        true,
	"dist":         true,
	"__pycache__":  true,
}

// langExtensions maps file extensions to language names.
var langExtensions = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".jsx":  "javascript",
	".java": "java",
	".rs":   "rust",
	".rb":   "ruby",
	".c":    "c",
	".h":    "c",
	".cc":   "cpp",
	".cpp":  "cpp",
}

// Scan walks the codebase rooted at dir and extracts file metadata.
// It uses git ls-files if the directory is a git repo, falling back
// to filepath.WalkDir otherwise.
func Scan(ctx context.Context, dir string, p *parser.Parser) ([]ScannedFile, error) {
	paths, err := listFiles(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}

	var files []ScannedFile
	for _, relPath := range paths {
		if shouldSkip(relPath) {
			continue
		}

		absPath := filepath.Join(dir, relPath)
		info, err := os.Stat(absPath)
		if err != nil {
			continue // skip unreadable files
		}

		sf := ScannedFile{
			Path:   relPath,
			Size:   info.Size(),
			Module: inferModule(relPath),
		}

		ext := filepath.Ext(relPath)
		lang, supported := langExtensions[ext]
		if supported {
			sf.Language = lang
			source, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			tree, err := p.Parse(filepath.Base(relPath), source)
			if err == nil {
				defer tree.Close()
				sf.Functions = tree.Functions()
				sf.Imports = tree.Imports()
			}
		} else {
			sf.Language = "unknown"
		}

		files = append(files, sf)
	}

	return files, nil
}

// listFiles returns relative file paths. Uses git ls-files if available,
// otherwise falls back to filepath.WalkDir.
func listFiles(ctx context.Context, dir string) ([]string, error) {
	if isGitRepo(dir) {
		return gitLsFiles(ctx, dir)
	}
	return walkFiles(dir)
}

// isGitRepo checks whether dir is inside a git repository.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// gitLsFiles returns tracked file paths relative to the repo root.
func gitLsFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var paths []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// walkFiles returns file paths relative to dir using filepath.WalkDir.
func walkFiles(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	return paths, err
}

// shouldSkip returns true if the file path should be excluded from scanning.
func shouldSkip(relPath string) bool {
	parts := strings.Split(relPath, string(filepath.Separator))
	for _, part := range parts {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

// inferModule derives a module name from the file's directory path.
func inferModule(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return "root"
	}
	return dir
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/scanner.go internal/wiki/scanner_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline codebase scanner (stage 1)"
```

---

### Task 3: Context-Aware Chunker (Stage 2)

**Files:**
- Create: `internal/wiki/chunker.go`
- Test: `internal/wiki/chunker_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/chunker_test.go
package wiki

import (
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSourceReader returns predefined content for file paths.
type mockSourceReader struct {
	files map[string][]byte
}

func (m *mockSourceReader) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}

func TestChunkGroupsByModule(t *testing.T) {
	files := []ScannedFile{
		{Path: "pkg/tools/file.go", Module: "pkg/tools", Language: "go"},
		{Path: "pkg/tools/shell.go", Module: "pkg/tools", Language: "go"},
		{Path: "pkg/parser/parser.go", Module: "pkg/parser", Language: "go"},
	}

	reader := &mockSourceReader{files: map[string][]byte{
		"pkg/tools/file.go":      []byte("package tools\n"),
		"pkg/tools/shell.go":     []byte("package tools\n"),
		"pkg/parser/parser.go":   []byte("package parser\n"),
	}}

	chunks, err := Chunk(files, reader, ChunkerConfig{
		MaxChunkSize: 100_000,
		MaxFileLines: 500,
	})
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	modules := map[string]int{}
	for _, c := range chunks {
		modules[c.Module] = len(c.Files)
	}
	assert.Equal(t, 2, modules["pkg/tools"])
	assert.Equal(t, 1, modules["pkg/parser"])
}

func TestChunkSplitsLargeModules(t *testing.T) {
	// Create files with enough content to exceed a small MaxChunkSize.
	files := []ScannedFile{
		{Path: "big/a.go", Module: "big", Language: "go", Size: 1000},
		{Path: "big/b.go", Module: "big", Language: "go", Size: 1000},
	}

	bigContent := make([]byte, 600)
	for i := range bigContent {
		bigContent[i] = 'x'
	}

	reader := &mockSourceReader{files: map[string][]byte{
		"big/a.go": bigContent,
		"big/b.go": bigContent,
	}}

	chunks, err := Chunk(files, reader, ChunkerConfig{
		MaxChunkSize: 800, // Force split
		MaxFileLines: 500,
	})
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1, "should split into multiple chunks")
	for _, c := range chunks {
		assert.Equal(t, "big", c.Module)
	}
}

func TestChunkEmptyFiles(t *testing.T) {
	chunks, err := Chunk(nil, nil, DefaultChunkerConfig())
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestChunkSourceContainsFunctionSignatures(t *testing.T) {
	files := []ScannedFile{
		{
			Path:     "pkg/lib.go",
			Module:   "pkg",
			Language: "go",
			Functions: []parser.FunctionDef{
				{Name: "Hello", StartLine: 3, EndLine: 5},
			},
			Imports: []string{"fmt"},
		},
	}

	reader := &mockSourceReader{files: map[string][]byte{
		"pkg/lib.go": []byte("package pkg\n\nfunc Hello() {\n\tfmt.Println(\"hi\")\n}\n"),
	}}

	chunks, err := Chunk(files, reader, DefaultChunkerConfig())
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	src := string(chunks[0].Source)
	assert.Contains(t, src, "Hello")
	assert.Contains(t, src, "fmt")
}

func TestDefaultChunkerConfig(t *testing.T) {
	cfg := DefaultChunkerConfig()
	assert.Equal(t, 100_000, cfg.MaxChunkSize)
	assert.Equal(t, 500, cfg.MaxFileLines)
}
```

Note: Add `"fmt"` to imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestChunk -v`
Expected: FAIL — `Chunk` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/chunker.go
package wiki

import (
	"fmt"
	"strings"
)

// SourceReader abstracts file reading for testability.
type SourceReader interface {
	ReadFile(path string) ([]byte, error)
}

// ChunkerConfig controls chunking behavior.
type ChunkerConfig struct {
	MaxChunkSize int // max characters per chunk (default 100_000)
	MaxFileLines int // include full source below this threshold (default 500)
}

// DefaultChunkerConfig returns default chunking settings.
func DefaultChunkerConfig() ChunkerConfig {
	return ChunkerConfig{
		MaxChunkSize: 100_000,
		MaxFileLines: 500,
	}
}

// Chunk groups ScannedFile entries by Module and builds structured text
// summaries for LLM consumption. Large modules are split into multiple chunks.
func Chunk(files []ScannedFile, reader SourceReader, cfg ChunkerConfig) ([]Chunk, error) {
	if len(files) == 0 {
		return nil, nil
	}

	// Group files by module.
	grouped := map[string][]ScannedFile{}
	var moduleOrder []string
	for _, f := range files {
		if _, seen := grouped[f.Module]; !seen {
			moduleOrder = append(moduleOrder, f.Module)
		}
		grouped[f.Module] = append(grouped[f.Module], f)
	}

	var chunks []Chunk
	for _, mod := range moduleOrder {
		modFiles := grouped[mod]
		modChunks, err := chunkModule(mod, modFiles, reader, cfg)
		if err != nil {
			return nil, fmt.Errorf("chunking module %s: %w", mod, err)
		}
		chunks = append(chunks, modChunks...)
	}

	return chunks, nil
}

// chunkModule builds chunks for a single module, splitting if necessary.
func chunkModule(module string, files []ScannedFile, reader SourceReader, cfg ChunkerConfig) ([]Chunk, error) {
	var chunks []Chunk
	var currentFiles []ScannedFile
	var currentSize int

	for _, f := range files {
		summary := buildFileSummary(f, reader, cfg)
		summarySize := len(summary)

		if currentSize+summarySize > cfg.MaxChunkSize && len(currentFiles) > 0 {
			// Flush current chunk.
			c, err := buildChunk(module, currentFiles, reader, cfg)
			if err != nil {
				return nil, err
			}
			chunks = append(chunks, c)
			currentFiles = nil
			currentSize = 0
		}

		currentFiles = append(currentFiles, f)
		currentSize += summarySize
	}

	// Flush remaining files.
	if len(currentFiles) > 0 {
		c, err := buildChunk(module, currentFiles, reader, cfg)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}

	return chunks, nil
}

// buildChunk constructs a Chunk from a set of files.
func buildChunk(module string, files []ScannedFile, reader SourceReader, cfg ChunkerConfig) (Chunk, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Module: %s\n\n", module))

	for _, f := range files {
		sb.WriteString(buildFileSummary(f, reader, cfg))
	}

	return Chunk{
		Module: module,
		Files:  files,
		Source: []byte(sb.String()),
	}, nil
}

// buildFileSummary creates a structured text summary of a single file.
func buildFileSummary(f ScannedFile, reader SourceReader, cfg ChunkerConfig) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## File: %s (language: %s)\n", f.Path, f.Language))

	// Function signatures.
	if len(f.Functions) > 0 {
		sb.WriteString("### Functions:\n")
		for _, fn := range f.Functions {
			sb.WriteString(fmt.Sprintf("- %s (lines %d-%d)\n", fn.Name, fn.StartLine, fn.EndLine))
		}
	}

	// Imports.
	if len(f.Imports) > 0 {
		sb.WriteString("### Imports:\n")
		for _, imp := range f.Imports {
			sb.WriteString(fmt.Sprintf("- %s\n", imp))
		}
	}

	// Source code — include full source for small files.
	if reader != nil {
		source, err := reader.ReadFile(f.Path)
		if err == nil {
			lineCount := strings.Count(string(source), "\n") + 1
			if lineCount <= cfg.MaxFileLines {
				sb.WriteString("### Source:\n```\n")
				sb.Write(source)
				sb.WriteString("\n```\n")
			}
		}
	}

	sb.WriteString("\n")
	return sb.String()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/chunker.go internal/wiki/chunker_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline context-aware chunker (stage 2)"
```

---

### Task 4: Multi-Pass LLM Analyzer (Stage 3)

**Files:**
- Create: `internal/wiki/analyzer.go`
- Test: `internal/wiki/analyzer_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/analyzer_test.go
package wiki

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMCompleter returns canned responses keyed by prompt substrings.
type mockLLMCompleter struct {
	responses map[string]string // substring match -> response
	calls     []string          // recorded prompts
}

func (m *mockLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	m.calls = append(m.calls, prompt)
	for key, resp := range m.responses {
		if strings.Contains(prompt, key) {
			return resp, nil
		}
	}
	return "default response", nil
}

func TestAnalyzeProducesModuleAnalysis(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"Summarize the following module": `Summary: Handles file operations
KeyTypes: FileTool struct
Patterns: Command pattern
Concerns: Error handling could be improved`,
			"architecture overview":   "The system uses a layered architecture.",
			"improvement suggestions": "1. Add retry logic\n2. Improve logging",
		},
	}

	chunks := []Chunk{
		{Module: "tools", Source: []byte("module tools content")},
	}

	result, err := Analyze(context.Background(), chunks, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "tools", result.Modules[0].Module)
	assert.NotEmpty(t, result.Modules[0].Summary)
	assert.NotEmpty(t, result.Architecture)
	assert.NotEmpty(t, result.Suggestions)
}

func TestAnalyzeConcurrentModules(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{},
	}

	chunks := []Chunk{
		{Module: "a", Source: []byte("mod a")},
		{Module: "b", Source: []byte("mod b")},
		{Module: "c", Source: []byte("mod c")},
	}

	result, err := Analyze(context.Background(), chunks, llm, AnalyzerConfig{Concurrency: 2})
	require.NoError(t, err)
	assert.Len(t, result.Modules, 3)
}

func TestAnalyzeEmptyChunks(t *testing.T) {
	llm := &mockLLMCompleter{responses: map[string]string{}}
	result, err := Analyze(context.Background(), nil, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	assert.Empty(t, result.Modules)
}

func TestAnalyzeModuleFailureContinues(t *testing.T) {
	callCount := 0
	llm := &mockLLMCompleter{
		responses: map[string]string{},
	}
	// Override to fail on first call.
	originalComplete := llm.Complete
	_ = originalComplete // We'll use a different approach.

	// Use a completer that fails for module "bad" but succeeds for "good".
	failLLM := &failingLLMCompleter{failOn: "bad"}

	chunks := []Chunk{
		{Module: "bad", Source: []byte("bad module")},
		{Module: "good", Source: []byte("good module")},
	}

	result, err := Analyze(context.Background(), chunks, failLLM, AnalyzerConfig{Concurrency: 1})
	require.NoError(t, err)
	// Should still get the good module.
	assert.Len(t, result.Modules, 1)
	assert.Equal(t, "good", result.Modules[0].Module)
	_ = callCount
}

func TestDefaultAnalyzerConfig(t *testing.T) {
	cfg := DefaultAnalyzerConfig()
	assert.Equal(t, 5, cfg.Concurrency)
}

// failingLLMCompleter fails for prompts containing a specific module name.
type failingLLMCompleter struct {
	failOn string
}

func (f *failingLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, f.failOn) {
		return "", fmt.Errorf("LLM error for module %s", f.failOn)
	}
	return "Summary: test\nKeyTypes: none\nPatterns: none\nConcerns: none", nil
}
```

Note: Add `"fmt"` to imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestAnalyze -v`
Expected: FAIL — `Analyze` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/analyzer.go
package wiki

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"text/template"

	"golang.org/x/sync/errgroup"
)

// LLMCompleter abstracts LLM calls for testability.
type LLMCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// AnalyzerConfig controls the LLM analysis behavior.
type AnalyzerConfig struct {
	Concurrency int // max parallel LLM calls (default 5)
}

// DefaultAnalyzerConfig returns default analyzer settings.
func DefaultAnalyzerConfig() AnalyzerConfig {
	return AnalyzerConfig{Concurrency: 5}
}

// Prompt templates for each analysis pass.
var modulePromptTmpl = template.Must(template.New("module").Parse(
	`Summarize the following module. Provide your analysis in this exact format:
Summary: <one paragraph summarizing purpose>
KeyTypes: <key types and interfaces>
Patterns: <design patterns used>
Concerns: <potential issues or concerns>

Module: {{.Module}}

{{.Source}}`))

var architecturePromptTmpl = template.Must(template.New("arch").Parse(
	`Given these module summaries, provide an architecture overview describing how the modules relate, the key abstractions, data flow, and cross-cutting concerns.

{{.Summaries}}`))

var suggestionsPromptTmpl = template.Must(template.New("suggest").Parse(
	`Given this architecture and module analysis, provide improvement suggestions — one per line, numbered.

Architecture:
{{.Architecture}}

Module summaries:
{{.Summaries}}`))

// Analyze runs three LLM passes over the chunked codebase.
func Analyze(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error) {
	if len(chunks) == 0 {
		return &AnalysisResult{}, nil
	}

	// Pass 1: Per-module summarization (concurrent).
	modules, err := analyzeModules(ctx, chunks, llm, cfg.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("module analysis: %w", err)
	}

	// Build concatenated summaries for synthesis passes.
	var summaryParts []string
	for _, m := range modules {
		summaryParts = append(summaryParts, fmt.Sprintf("## %s\n%s", m.Module, m.Summary))
	}
	summaries := strings.Join(summaryParts, "\n\n")

	// Pass 2: Cross-cutting synthesis.
	var archBuf strings.Builder
	if err := architecturePromptTmpl.Execute(&archBuf, map[string]string{"Summaries": summaries}); err != nil {
		return nil, fmt.Errorf("building architecture prompt: %w", err)
	}
	architecture, err := llm.Complete(ctx, archBuf.String())
	if err != nil {
		architecture = "Architecture synthesis unavailable."
		log.Printf("wiki: architecture synthesis failed: %v", err)
	}

	// Pass 3: Suggestions.
	var sugBuf strings.Builder
	if err := suggestionsPromptTmpl.Execute(&sugBuf, map[string]string{
		"Architecture": architecture,
		"Summaries":    summaries,
	}); err != nil {
		return nil, fmt.Errorf("building suggestions prompt: %w", err)
	}
	suggestionsText, err := llm.Complete(ctx, sugBuf.String())
	if err != nil {
		suggestionsText = ""
		log.Printf("wiki: suggestions generation failed: %v", err)
	}

	var suggestions []string
	for _, line := range strings.Split(suggestionsText, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			suggestions = append(suggestions, line)
		}
	}

	return &AnalysisResult{
		Modules:         modules,
		Architecture:    architecture,
		KeyAbstractions: architecture, // extracted from same synthesis
		Suggestions:     suggestions,
	}, nil
}

// analyzeModules runs Pass 1 concurrently with a bounded worker pool.
func analyzeModules(ctx context.Context, chunks []Chunk, llm LLMCompleter, concurrency int) ([]ModuleAnalysis, error) {
	if concurrency <= 0 {
		concurrency = 5
	}

	var mu sync.Mutex
	var modules []ModuleAnalysis

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, chunk := range chunks {
		chunk := chunk // capture loop variable
		g.Go(func() error {
			var promptBuf strings.Builder
			if err := modulePromptTmpl.Execute(&promptBuf, map[string]string{
				"Module": chunk.Module,
				"Source": string(chunk.Source),
			}); err != nil {
				return fmt.Errorf("building prompt for %s: %w", chunk.Module, err)
			}

			response, err := llm.Complete(ctx, promptBuf.String())
			if err != nil {
				log.Printf("wiki: analysis failed for module %s: %v", chunk.Module, err)
				return nil // non-fatal: skip this module
			}

			ma := parseModuleResponse(chunk.Module, response)

			mu.Lock()
			modules = append(modules, ma)
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return modules, nil
}

// parseModuleResponse extracts structured fields from the LLM response.
func parseModuleResponse(module, response string) ModuleAnalysis {
	ma := ModuleAnalysis{Module: module, Summary: response}

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Summary:") {
			ma.Summary = strings.TrimSpace(strings.TrimPrefix(line, "Summary:"))
		} else if strings.HasPrefix(line, "KeyTypes:") {
			ma.KeyTypes = strings.TrimSpace(strings.TrimPrefix(line, "KeyTypes:"))
		} else if strings.HasPrefix(line, "Patterns:") {
			ma.Patterns = strings.TrimSpace(strings.TrimPrefix(line, "Patterns:"))
		} else if strings.HasPrefix(line, "Concerns:") {
			ma.Concerns = strings.TrimSpace(strings.TrimPrefix(line, "Concerns:"))
		}
	}

	return ma
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/analyzer.go internal/wiki/analyzer_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline multi-pass LLM analyzer (stage 3)"
```

---

### Task 5: Mermaid Diagram Generator (Stage 4)

**Files:**
- Create: `internal/wiki/diagrams.go`
- Test: `internal/wiki/diagrams_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/diagrams_test.go
package wiki

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateArchitectureDiagram(t *testing.T) {
	files := []ScannedFile{
		{Path: "cmd/main.go", Module: "cmd", Imports: []string{"pkg/tools", "pkg/parser"}},
		{Path: "pkg/tools/file.go", Module: "pkg/tools", Imports: []string{"os"}},
		{Path: "pkg/parser/parse.go", Module: "pkg/parser", Imports: []string{"strings"}},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "cmd", Summary: "CLI entrypoint"},
			{Module: "pkg/tools", Summary: "Tool implementations"},
			{Module: "pkg/parser", Summary: "Parser"},
		},
	}

	diagrams, err := GenerateDiagrams(context.Background(), files, analysis, nil, DefaultDiagramConfig())
	require.NoError(t, err)

	// Should have at least architecture and dependency diagrams.
	var types []string
	for _, d := range diagrams {
		types = append(types, d.Type)
		assert.NotEmpty(t, d.Content)
		assert.NotEmpty(t, d.Title)
	}
	assert.Contains(t, types, "architecture")
	assert.Contains(t, types, "dependency")
}

func TestGenerateDependencyDiagram(t *testing.T) {
	files := []ScannedFile{
		{Path: "a/lib.go", Module: "a", Imports: []string{"b/util"}},
		{Path: "b/util.go", Module: "b", Imports: []string{}},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "a"}, {Module: "b"},
		},
	}

	diagrams, err := GenerateDiagrams(context.Background(), files, analysis, nil, DefaultDiagramConfig())
	require.NoError(t, err)

	var depDiagram *Diagram
	for i, d := range diagrams {
		if d.Type == "dependency" {
			depDiagram = &diagrams[i]
			break
		}
	}
	require.NotNil(t, depDiagram)
	assert.Contains(t, depDiagram.Content, "graph LR")
}

func TestGenerateDiagramsUnsupportedFormat(t *testing.T) {
	_, err := GenerateDiagrams(context.Background(), nil, &AnalysisResult{}, nil, DiagramConfig{Format: "d2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestGenerateDiagramsWithLLMSequence(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"sequence diagram": `sequenceDiagram
    Client->>Server: Request
    Server->>Client: Response`,
		},
	}

	analysis := &AnalysisResult{
		Architecture: "Client-server architecture",
		Modules:      []ModuleAnalysis{{Module: "server"}},
	}

	diagrams, err := GenerateDiagrams(context.Background(), nil, analysis, llm, DefaultDiagramConfig())
	require.NoError(t, err)

	var seqDiagram *Diagram
	for i, d := range diagrams {
		if d.Type == "sequence" {
			seqDiagram = &diagrams[i]
			break
		}
	}
	require.NotNil(t, seqDiagram)
	assert.Contains(t, seqDiagram.Content, "sequenceDiagram")
}

func TestDefaultDiagramConfig(t *testing.T) {
	cfg := DefaultDiagramConfig()
	assert.Equal(t, "mermaid", cfg.Format)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestGenerate -v`
Expected: FAIL — `GenerateDiagrams` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/diagrams.go
package wiki

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// DiagramConfig controls diagram generation.
type DiagramConfig struct {
	Format string // "mermaid" (default); "d2" and "dot" return unsupported error
}

// DefaultDiagramConfig returns default diagram settings.
func DefaultDiagramConfig() DiagramConfig {
	return DiagramConfig{Format: "mermaid"}
}

// GenerateDiagrams produces Mermaid diagrams from file data and analysis results.
// Three diagrams are built programmatically; sequence diagrams use an LLM call.
func GenerateDiagrams(ctx context.Context, files []ScannedFile, analysis *AnalysisResult, llm LLMCompleter, cfg DiagramConfig) ([]Diagram, error) {
	if cfg.Format != "mermaid" {
		return nil, fmt.Errorf("unsupported diagram format %q: only mermaid is supported", cfg.Format)
	}

	var diagrams []Diagram

	// 1. Architecture overview.
	if arch := buildArchitectureDiagram(analysis); arch.Content != "" {
		diagrams = append(diagrams, arch)
	}

	// 2. Dependency graph.
	if dep := buildDependencyDiagram(files, analysis); dep.Content != "" {
		diagrams = append(diagrams, dep)
	}

	// 3. Data flow.
	if flow := buildDataFlowDiagram(files, analysis); flow.Content != "" {
		diagrams = append(diagrams, flow)
	}

	// 4. Sequence diagrams (LLM-generated).
	if llm != nil && analysis.Architecture != "" {
		if seq, err := generateSequenceDiagram(ctx, analysis, llm); err == nil && seq.Content != "" {
			diagrams = append(diagrams, seq)
		} else if err != nil {
			log.Printf("wiki: sequence diagram generation failed: %v", err)
		}
	}

	return diagrams, nil
}

// buildArchitectureDiagram creates a graph TD showing modules.
func buildArchitectureDiagram(analysis *AnalysisResult) Diagram {
	if len(analysis.Modules) == 0 {
		return Diagram{}
	}

	var sb strings.Builder
	sb.WriteString("graph TD\n")
	for _, m := range analysis.Modules {
		id := sanitizeID(m.Module)
		label := m.Module
		if m.Summary != "" {
			// Truncate long summaries for diagram labels.
			summary := m.Summary
			if len(summary) > 40 {
				summary = summary[:40] + "..."
			}
			label = fmt.Sprintf("%s\\n%s", m.Module, summary)
		}
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", id, label))
	}

	return Diagram{
		Title:   "Architecture Overview",
		Type:    "architecture",
		Content: sb.String(),
	}
}

// buildDependencyDiagram creates a graph LR showing import relationships.
func buildDependencyDiagram(files []ScannedFile, analysis *AnalysisResult) Diagram {
	// Build set of known modules.
	knownModules := map[string]bool{}
	for _, m := range analysis.Modules {
		knownModules[m.Module] = true
	}

	// Collect edges: module -> imported module.
	edges := map[string]map[string]bool{}
	for _, f := range files {
		src := f.Module
		for _, imp := range f.Imports {
			// Check if any known module is a prefix of the import.
			for mod := range knownModules {
				if strings.Contains(imp, mod) && mod != src {
					if edges[src] == nil {
						edges[src] = map[string]bool{}
					}
					edges[src][mod] = true
				}
			}
		}
	}

	if len(edges) == 0 {
		// Still produce a diagram with just node declarations.
		var sb strings.Builder
		sb.WriteString("graph LR\n")
		for _, m := range analysis.Modules {
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", sanitizeID(m.Module), m.Module))
		}
		return Diagram{
			Title:   "Module Dependencies",
			Type:    "dependency",
			Content: sb.String(),
		}
	}

	var sb strings.Builder
	sb.WriteString("graph LR\n")
	for src, targets := range edges {
		for tgt := range targets {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", sanitizeID(src), sanitizeID(tgt)))
		}
	}

	return Diagram{
		Title:   "Module Dependencies",
		Type:    "dependency",
		Content: sb.String(),
	}
}

// buildDataFlowDiagram creates a flowchart showing data paths.
func buildDataFlowDiagram(files []ScannedFile, analysis *AnalysisResult) Diagram {
	if len(analysis.Modules) < 2 {
		return Diagram{}
	}

	var sb strings.Builder
	sb.WriteString("flowchart LR\n")
	for i, m := range analysis.Modules {
		id := sanitizeID(m.Module)
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", id, m.Module))
		if i > 0 {
			prev := sanitizeID(analysis.Modules[i-1].Module)
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", prev, id))
		}
	}

	return Diagram{
		Title:   "Data Flow",
		Type:    "data-flow",
		Content: sb.String(),
	}
}

// generateSequenceDiagram uses the LLM to create sequence diagrams.
func generateSequenceDiagram(ctx context.Context, analysis *AnalysisResult, llm LLMCompleter) (Diagram, error) {
	prompt := fmt.Sprintf(`Given this architecture, generate a Mermaid sequence diagram showing the 2-3 most important flows. Output only valid Mermaid syntax starting with "sequenceDiagram".

Architecture:
%s`, analysis.Architecture)

	response, err := llm.Complete(ctx, prompt)
	if err != nil {
		return Diagram{}, fmt.Errorf("LLM sequence diagram: %w", err)
	}

	return Diagram{
		Title:   "Key Sequences",
		Type:    "sequence",
		Content: response,
	}, nil
}

// sanitizeID converts a module path to a valid Mermaid node ID.
func sanitizeID(s string) string {
	r := strings.NewReplacer("/", "_", ".", "_", "-", "_", " ", "_")
	return r.Replace(s)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/diagrams.go internal/wiki/diagrams_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline Mermaid diagram generator (stage 4)"
```

---

### Task 6: Document Assembler (Stage 5)

**Files:**
- Create: `internal/wiki/assembler.go`
- Test: `internal/wiki/assembler_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/assembler_test.go
package wiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssembleCreatesIndexPage(t *testing.T) {
	analysis := &AnalysisResult{
		Architecture: "Layered architecture",
		Modules: []ModuleAnalysis{
			{Module: "core", Summary: "Core logic"},
		},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	var found bool
	for _, d := range docs {
		if d.Path == "_index.md" {
			found = true
			assert.Contains(t, d.Content, "Layered architecture")
		}
	}
	assert.True(t, found, "should have _index.md")
}

func TestAssembleCreatesModulePages(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "pkg/tools", Summary: "Tool implementations"},
			{Module: "pkg/parser", Summary: "Source parser"},
		},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	var modulePaths []string
	for _, d := range docs {
		if len(d.Path) > 8 && d.Path[:8] == "modules/" {
			modulePaths = append(modulePaths, d.Path)
		}
	}
	assert.Len(t, modulePaths, 3) // _index.md + 2 module pages
}

func TestAssembleIncludesDiagrams(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{{Module: "core"}},
	}
	diagrams := []Diagram{
		{Title: "Architecture", Type: "architecture", Content: "graph TD\n  A-->B"},
		{Title: "Dependencies", Type: "dependency", Content: "graph LR\n  A-->B"},
	}

	docs, err := Assemble(analysis, diagrams, nil)
	require.NoError(t, err)

	var archDoc *Document
	for i, d := range docs {
		if d.Path == "architecture/overview.md" {
			archDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, archDoc)
	assert.Contains(t, archDoc.Content, "```mermaid")
}

func TestAssembleIncludesSkillSections(t *testing.T) {
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{{Module: "core"}},
	}
	sections := []SkillWikiSection{
		{SkillName: "k8s", Title: "Kubernetes Architecture", Content: "K8s deployment topology"},
	}

	docs, err := Assemble(analysis, nil, sections)
	require.NoError(t, err)

	var skillDoc *Document
	for i, d := range docs {
		if d.Path == "skill-contributed/kubernetes-architecture.md" {
			skillDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, skillDoc)
	assert.Contains(t, skillDoc.Content, "K8s deployment topology")
}

func TestAssembleCreatesSuggestionsPage(t *testing.T) {
	analysis := &AnalysisResult{
		Modules:     []ModuleAnalysis{{Module: "core"}},
		Suggestions: []string{"Add retry logic", "Improve logging"},
	}

	docs, err := Assemble(analysis, nil, nil)
	require.NoError(t, err)

	var sugDoc *Document
	for i, d := range docs {
		if d.Path == "suggestions/improvements.md" {
			sugDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, sugDoc)
	assert.Contains(t, sugDoc.Content, "Add retry logic")
	assert.Contains(t, sugDoc.Content, "Improve logging")
}

func TestAssembleEmptyAnalysis(t *testing.T) {
	docs, err := Assemble(&AnalysisResult{}, nil, nil)
	require.NoError(t, err)
	// Should still produce at least the index page.
	assert.NotEmpty(t, docs)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestAssemble -v`
Expected: FAIL — `Assemble` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/assembler.go
package wiki

import (
	"fmt"
	"strings"
)

// Assemble builds the document tree from analysis results, diagrams, and skill
// contributions.
func Assemble(analysis *AnalysisResult, diagrams []Diagram, skillSections []SkillWikiSection) ([]Document, error) {
	var docs []Document

	// _index.md — project overview.
	docs = append(docs, buildIndexPage(analysis))

	// Architecture pages.
	docs = append(docs, buildArchitecturePages(analysis, diagrams)...)

	// Module pages.
	docs = append(docs, buildModulePages(analysis)...)

	// Code structure.
	docs = append(docs, buildCodeStructurePage(analysis))

	// Security placeholder.
	docs = append(docs, Document{
		Path:    "security/overview.md",
		Title:   "Security",
		Content: "# Security\n\n*Security analysis pending — security engine integration is not yet available.*\n",
	})

	// Suggestions.
	if len(analysis.Suggestions) > 0 {
		docs = append(docs, buildSuggestionsPage(analysis))
	}

	// Skill-contributed sections.
	for _, section := range skillSections {
		docs = append(docs, buildSkillPage(section))
	}

	return docs, nil
}

func buildIndexPage(analysis *AnalysisResult) Document {
	var sb strings.Builder
	sb.WriteString("# Project Overview\n\n")

	if analysis.Architecture != "" {
		sb.WriteString("## Architecture\n\n")
		sb.WriteString(analysis.Architecture)
		sb.WriteString("\n\n")
	}

	if len(analysis.Modules) > 0 {
		sb.WriteString("## Modules\n\n")
		for _, m := range analysis.Modules {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", m.Module, m.Summary))
		}
		sb.WriteString("\n")
	}

	return Document{
		Path:    "_index.md",
		Title:   "Project Overview",
		Content: sb.String(),
	}
}

func buildArchitecturePages(analysis *AnalysisResult, diagrams []Diagram) []Document {
	var docs []Document

	// Overview page with architecture diagram.
	var overviewSB strings.Builder
	overviewSB.WriteString("# Architecture Overview\n\n")
	if analysis.Architecture != "" {
		overviewSB.WriteString(analysis.Architecture)
		overviewSB.WriteString("\n\n")
	}
	for _, d := range diagrams {
		if d.Type == "architecture" {
			overviewSB.WriteString(fmt.Sprintf("## %s\n\n```mermaid\n%s\n```\n\n", d.Title, d.Content))
		}
	}
	docs = append(docs, Document{
		Path:    "architecture/overview.md",
		Title:   "Architecture Overview",
		Content: overviewSB.String(),
	})

	// Dependencies page.
	var depSB strings.Builder
	depSB.WriteString("# Dependencies\n\n")
	for _, d := range diagrams {
		if d.Type == "dependency" {
			depSB.WriteString(fmt.Sprintf("```mermaid\n%s\n```\n\n", d.Content))
		}
	}
	docs = append(docs, Document{
		Path:    "architecture/dependencies.md",
		Title:   "Dependencies",
		Content: depSB.String(),
	})

	// Data flow page.
	var flowSB strings.Builder
	flowSB.WriteString("# Data Flow\n\n")
	for _, d := range diagrams {
		if d.Type == "data-flow" || d.Type == "sequence" {
			flowSB.WriteString(fmt.Sprintf("## %s\n\n```mermaid\n%s\n```\n\n", d.Title, d.Content))
		}
	}
	docs = append(docs, Document{
		Path:    "architecture/data-flow.md",
		Title:   "Data Flow",
		Content: flowSB.String(),
	})

	return docs
}

func buildModulePages(analysis *AnalysisResult) []Document {
	var docs []Document

	// Module index.
	var indexSB strings.Builder
	indexSB.WriteString("# Modules\n\n")
	for _, m := range analysis.Modules {
		indexSB.WriteString(fmt.Sprintf("- [%s](%s.md)\n", m.Module, sanitizeID(m.Module)))
	}
	docs = append(docs, Document{
		Path:    "modules/_index.md",
		Title:   "Modules",
		Content: indexSB.String(),
	})

	// Individual module pages.
	for _, m := range analysis.Modules {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# %s\n\n", m.Module))
		if m.Summary != "" {
			sb.WriteString(fmt.Sprintf("## Summary\n\n%s\n\n", m.Summary))
		}
		if m.KeyTypes != "" {
			sb.WriteString(fmt.Sprintf("## Key Types\n\n%s\n\n", m.KeyTypes))
		}
		if m.Patterns != "" {
			sb.WriteString(fmt.Sprintf("## Patterns\n\n%s\n\n", m.Patterns))
		}
		if m.Concerns != "" {
			sb.WriteString(fmt.Sprintf("## Concerns\n\n%s\n\n", m.Concerns))
		}

		docs = append(docs, Document{
			Path:    fmt.Sprintf("modules/%s.md", sanitizeID(m.Module)),
			Title:   m.Module,
			Content: sb.String(),
		})
	}

	return docs
}

func buildCodeStructurePage(analysis *AnalysisResult) Document {
	var sb strings.Builder
	sb.WriteString("# Code Structure\n\n")
	if analysis.KeyAbstractions != "" {
		sb.WriteString("## Key Abstractions\n\n")
		sb.WriteString(analysis.KeyAbstractions)
		sb.WriteString("\n\n")
	}
	return Document{
		Path:    "code-structure/overview.md",
		Title:   "Code Structure",
		Content: sb.String(),
	}
}

func buildSuggestionsPage(analysis *AnalysisResult) Document {
	var sb strings.Builder
	sb.WriteString("# Improvement Suggestions\n\n")
	for _, s := range analysis.Suggestions {
		sb.WriteString(fmt.Sprintf("- %s\n", s))
	}
	return Document{
		Path:    "suggestions/improvements.md",
		Title:   "Improvement Suggestions",
		Content: sb.String(),
	}
}

func buildSkillPage(section SkillWikiSection) Document {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", section.Title))
	sb.WriteString(section.Content)
	sb.WriteString("\n")

	for _, d := range section.Diagrams {
		sb.WriteString(fmt.Sprintf("\n## %s\n\n```mermaid\n%s\n```\n", d.Title, d.Content))
	}

	// Build filename from title.
	filename := strings.ToLower(strings.ReplaceAll(section.Title, " ", "-"))
	return Document{
		Path:    fmt.Sprintf("skill-contributed/%s.md", filename),
		Title:   section.Title,
		Content: sb.String(),
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/assembler.go internal/wiki/assembler_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline document assembler (stage 5)"
```

---

### Task 7: Site Renderer (Stage 6)

**Files:**
- Create: `internal/wiki/renderer.go`
- Test: `internal/wiki/renderer_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/renderer_test.go
package wiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderRawMarkdown(t *testing.T) {
	dir := t.TempDir()
	docs := []Document{
		{Path: "_index.md", Title: "Overview", Content: "# Overview\n"},
		{Path: "modules/core.md", Title: "Core", Content: "# Core\n"},
	}

	err := Render(docs, RendererConfig{Format: "raw-md", OutputDir: dir})
	require.NoError(t, err)

	// Verify files exist.
	assertFileContains(t, filepath.Join(dir, "_index.md"), "# Overview")
	assertFileContains(t, filepath.Join(dir, "modules", "core.md"), "# Core")
}

func TestRenderHugo(t *testing.T) {
	dir := t.TempDir()
	docs := []Document{
		{Path: "_index.md", Title: "Overview", Content: "# Overview\n"},
		{Path: "modules/core.md", Title: "Core", Content: "# Core\n"},
	}

	err := Render(docs, RendererConfig{Format: "hugo", OutputDir: dir})
	require.NoError(t, err)

	// Verify Hugo front matter.
	content := readTestFile(t, filepath.Join(dir, "content", "_index.md"))
	assert.Contains(t, content, "---")
	assert.Contains(t, content, "title:")

	// Verify config.toml exists.
	assertFileExists(t, filepath.Join(dir, "config.toml"))
}

func TestRenderDocusaurus(t *testing.T) {
	dir := t.TempDir()
	docs := []Document{
		{Path: "_index.md", Title: "Overview", Content: "# Overview\n"},
		{Path: "modules/core.md", Title: "Core", Content: "# Core\n"},
	}

	err := Render(docs, RendererConfig{Format: "docusaurus", OutputDir: dir})
	require.NoError(t, err)

	// Verify Docusaurus front matter.
	content := readTestFile(t, filepath.Join(dir, "docs", "modules", "core.md"))
	assert.Contains(t, content, "---")
	assert.Contains(t, content, "sidebar_label:")

	// Verify docusaurus.config.js exists.
	assertFileExists(t, filepath.Join(dir, "docusaurus.config.js"))
}

func TestRenderUnsupportedFormat(t *testing.T) {
	err := Render(nil, RendererConfig{Format: "mkdocs"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestRenderEmptyDocuments(t *testing.T) {
	dir := t.TempDir()
	err := Render(nil, RendererConfig{Format: "raw-md", OutputDir: dir})
	require.NoError(t, err)
}

func TestDefaultRendererConfig(t *testing.T) {
	cfg := DefaultRendererConfig()
	assert.Equal(t, "raw-md", cfg.Format)
	assert.Equal(t, "docs/wiki", cfg.OutputDir)
}

// --- test helpers ---

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "file should exist: %s", path)
}

func assertFileContains(t *testing.T, path, substring string) {
	t.Helper()
	content := readTestFile(t, path)
	assert.Contains(t, content, substring)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading %s", path)
	return string(data)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestRender -v`
Expected: FAIL — `Render` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/renderer.go
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RendererConfig controls output format and location.
type RendererConfig struct {
	Format    string // "raw-md" (default), "hugo", "docusaurus"
	OutputDir string // default "docs/wiki"
}

// DefaultRendererConfig returns default renderer settings.
func DefaultRendererConfig() RendererConfig {
	return RendererConfig{
		Format:    "raw-md",
		OutputDir: "docs/wiki",
	}
}

// Render writes documents to disk in the chosen format.
func Render(documents []Document, cfg RendererConfig) error {
	switch cfg.Format {
	case "raw-md":
		return renderRawMarkdown(documents, cfg.OutputDir)
	case "hugo":
		return renderHugo(documents, cfg.OutputDir)
	case "docusaurus":
		return renderDocusaurus(documents, cfg.OutputDir)
	default:
		return fmt.Errorf("unsupported render format %q: use raw-md, hugo, or docusaurus", cfg.Format)
	}
}

// renderRawMarkdown writes plain Markdown files.
func renderRawMarkdown(documents []Document, outputDir string) error {
	for _, doc := range documents {
		if err := writeDoc(filepath.Join(outputDir, doc.Path), doc.Content); err != nil {
			return err
		}
	}
	return nil
}

// renderHugo writes Markdown with Hugo front matter and generates config.toml.
func renderHugo(documents []Document, outputDir string) error {
	contentDir := filepath.Join(outputDir, "content")

	for i, doc := range documents {
		frontMatter := fmt.Sprintf("---\ntitle: %q\nweight: %d\n---\n\n", doc.Title, i+1)
		content := frontMatter + doc.Content
		if err := writeDoc(filepath.Join(contentDir, doc.Path), content); err != nil {
			return err
		}
	}

	// Generate config.toml.
	configContent := `baseURL = "/"
languageCode = "en-us"
title = "Project Wiki"

[params]
  description = "Auto-generated project documentation"

[markup]
  [markup.goldmark]
    [markup.goldmark.renderer]
      unsafe = true
`
	return writeDoc(filepath.Join(outputDir, "config.toml"), configContent)
}

// renderDocusaurus writes Markdown with Docusaurus front matter and config.
func renderDocusaurus(documents []Document, outputDir string) error {
	docsDir := filepath.Join(outputDir, "docs")

	for i, doc := range documents {
		label := doc.Title
		if label == "" {
			label = strings.TrimSuffix(filepath.Base(doc.Path), ".md")
		}
		frontMatter := fmt.Sprintf("---\nsidebar_position: %d\nsidebar_label: %q\n---\n\n", i+1, label)
		content := frontMatter + doc.Content
		if err := writeDoc(filepath.Join(docsDir, doc.Path), content); err != nil {
			return err
		}
	}

	// Generate docusaurus.config.js.
	configContent := `// @ts-check
/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Project Wiki',
  tagline: 'Auto-generated documentation',
  url: 'https://example.com',
  baseUrl: '/',
  onBrokenLinks: 'warn',
  favicon: 'img/favicon.ico',

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.js',
        },
      }),
    ],
  ],

  themes: ['@docusaurus/theme-mermaid'],
  markdown: {
    mermaid: true,
  },
};

module.exports = config;
`
	return writeDoc(filepath.Join(outputDir, "docusaurus.config.js"), configContent)
}

// writeDoc creates parent directories and writes content to a file.
func writeDoc(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/renderer.go internal/wiki/renderer_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline site renderer (stage 6)"
```

---

### Task 8: Pipeline Orchestrator

**Files:**
- Create: `internal/wiki/pipeline.go`
- Test: `internal/wiki/pipeline_test.go`

**Step 1: Write the failing test**

```go
// internal/wiki/pipeline_test.go
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

func TestRunFullPipeline(t *testing.T) {
	// Create a temp directory with a small Go project.
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	writeFile(t, filepath.Join(srcDir, "main.go"), `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	gitAdd(t, srcDir, "main.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{responses: map[string]string{}}
	p := parser.NewParser()

	err := Run(context.Background(), Config{
		Dir:         srcDir,
		OutputDir:   outDir,
		Format:      "raw-md",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}, llm, p)
	require.NoError(t, err)

	// Verify output files exist.
	assertFileExists(t, filepath.Join(outDir, "_index.md"))
}

func TestRunPipelineWithHugoFormat(t *testing.T) {
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	writeFile(t, filepath.Join(srcDir, "lib.go"), "package lib\n")
	gitAdd(t, srcDir, "lib.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{responses: map[string]string{}}
	p := parser.NewParser()

	err := Run(context.Background(), Config{
		Dir:         srcDir,
		OutputDir:   outDir,
		Format:      "hugo",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}, llm, p)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(outDir, "config.toml"))
}

func TestRunPipelineWithDocusaurusFormat(t *testing.T) {
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	writeFile(t, filepath.Join(srcDir, "lib.go"), "package lib\n")
	gitAdd(t, srcDir, "lib.go")

	outDir := t.TempDir()

	llm := &mockLLMCompleter{responses: map[string]string{}}
	p := parser.NewParser()

	err := Run(context.Background(), Config{
		Dir:         srcDir,
		OutputDir:   outDir,
		Format:      "docusaurus",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}, llm, p)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(outDir, "docusaurus.config.js"))
}

func TestRunPipelineEmptyDir(t *testing.T) {
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	outDir := t.TempDir()

	llm := &mockLLMCompleter{responses: map[string]string{}}
	p := parser.NewParser()

	err := Run(context.Background(), Config{
		Dir:         srcDir,
		OutputDir:   outDir,
		Format:      "raw-md",
		DiagramFmt:  "mermaid",
		Concurrency: 1,
	}, llm, p)
	require.NoError(t, err)

	// Should still produce at least the index page.
	assertFileExists(t, filepath.Join(outDir, "_index.md"))
}

func TestRunPipelineCancellation(t *testing.T) {
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	writeFile(t, filepath.Join(srcDir, "main.go"), "package main\n")
	gitAdd(t, srcDir, "main.go")

	outDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	llm := &mockLLMCompleter{responses: map[string]string{}}
	p := parser.NewParser()

	err := Run(ctx, Config{
		Dir: srcDir, OutputDir: outDir, Format: "raw-md",
		DiagramFmt: "mermaid", Concurrency: 1,
	}, llm, p)
	// Context cancellation should cause an error (in scanner or analyzer).
	// But scanner may succeed since it doesn't check ctx in git ls-files on all platforms.
	// Just verify no panic.
	_ = err
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestRunFull -v`
Expected: FAIL — `Run` not defined

**Step 3: Write minimal implementation**

```go
// internal/wiki/pipeline.go
package wiki

import (
	"context"
	"fmt"
	"os"

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
	return os.ReadFile(fmt.Sprintf("%s/%s", r.baseDir, path))
}

// Run executes the full wiki pipeline: scan → chunk → analyze → diagrams → assemble → render.
func Run(ctx context.Context, cfg Config, llm LLMCompleter, p *parser.Parser) error {
	fmt.Fprintf(os.Stderr, "wiki: scanning %s...\n", cfg.Dir)

	// Stage 1: Scan codebase.
	files, err := Scan(ctx, cfg.Dir, p)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wiki: found %d files\n", len(files))

	// Stage 2: Chunk files.
	reader := &osSourceReader{baseDir: cfg.Dir}
	chunks, err := Chunk(files, reader, DefaultChunkerConfig())
	if err != nil {
		return fmt.Errorf("chunk: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wiki: created %d chunks\n", len(chunks))

	// Stage 3: Analyze with LLM.
	analyzerCfg := AnalyzerConfig{Concurrency: cfg.Concurrency}
	if analyzerCfg.Concurrency <= 0 {
		analyzerCfg.Concurrency = 5
	}

	analysis, err := Analyze(ctx, chunks, llm, analyzerCfg)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wiki: analyzed %d modules\n", len(analysis.Modules))

	// Stage 4: Generate diagrams.
	diagrams, err := GenerateDiagrams(ctx, files, analysis, llm, DiagramConfig{Format: cfg.DiagramFmt})
	if err != nil {
		return fmt.Errorf("diagrams: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wiki: generated %d diagrams\n", len(diagrams))

	// Stage 5: Assemble documents.
	documents, err := Assemble(analysis, diagrams, nil)
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wiki: assembled %d documents\n", len(documents))

	// Stage 6: Render output.
	if err := Render(documents, RendererConfig{
		Format:    cfg.Format,
		OutputDir: cfg.OutputDir,
	}); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wiki: output written to %s\n", cfg.OutputDir)

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/wiki/pipeline.go internal/wiki/pipeline_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline orchestrator"
```

---

### Task 9: CLI Entrypoint (`rubichan wiki`)

**Files:**
- Create: `cmd/rubichan/wiki.go`
- Test: `cmd/rubichan/wiki_test.go`

**Step 1: Write the failing test**

```go
// cmd/rubichan/wiki_test.go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiCmdExists(t *testing.T) {
	cmd := wikiCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "wiki", cmd.Use)
}

func TestWikiCmdDefaultFlags(t *testing.T) {
	cmd := wikiCmd()

	format, _ := cmd.Flags().GetString("format")
	assert.Equal(t, "raw-md", format)

	output, _ := cmd.Flags().GetString("output")
	assert.Equal(t, "docs/wiki", output)

	diagrams, _ := cmd.Flags().GetString("diagrams")
	assert.Equal(t, "mermaid", diagrams)

	concurrency, _ := cmd.Flags().GetInt("concurrency")
	assert.Equal(t, 5, concurrency)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestWikiCmd -v`
Expected: FAIL — `wikiCmd` not defined

**Step 3: Write minimal implementation**

```go
// cmd/rubichan/wiki.go
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/wiki"
)

func wikiCmd() *cobra.Command {
	var (
		formatFlag      string
		outputFlag      string
		diagramsFlag    string
		concurrencyFlag int
	)

	cmd := &cobra.Command{
		Use:   "wiki [path]",
		Short: "Generate project documentation wiki",
		Long: `Analyze a codebase and generate a static documentation site with
architecture diagrams, module documentation, and improvement suggestions.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			p, err := provider.NewProvider(cfg)
			if err != nil {
				return fmt.Errorf("creating provider: %w", err)
			}

			llm := integrations.NewLLMCompleter(p, cfg.Provider.Model)
			psr := parser.NewParser()

			return wiki.Run(cmd.Context(), wiki.Config{
				Dir:         dir,
				OutputDir:   outputFlag,
				Format:      formatFlag,
				DiagramFmt:  diagramsFlag,
				Concurrency: concurrencyFlag,
			}, llm, psr)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", "raw-md", "output format: raw-md, hugo, docusaurus")
	cmd.Flags().StringVar(&outputFlag, "output", "docs/wiki", "output directory")
	cmd.Flags().StringVar(&diagramsFlag, "diagrams", "mermaid", "diagram format (only mermaid supported)")
	cmd.Flags().IntVar(&concurrencyFlag, "concurrency", 5, "max parallel LLM calls")

	return cmd
}
```

**Step 4: Wire the command in main.go**

Add `rootCmd.AddCommand(wikiCmd())` in the `main()` function, after `rootCmd.AddCommand(skillCmd())`.

**Step 5: Run test to verify it passes**

Run: `go test ./cmd/rubichan/... -run TestWikiCmd -v`
Expected: PASS

**Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

**Step 7: Commit**

```bash
git add cmd/rubichan/wiki.go cmd/rubichan/wiki_test.go cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Add rubichan wiki CLI subcommand"
```

---

### Task 10: Integration Test & Coverage

**Files:**
- Modify: `internal/wiki/pipeline_test.go` (add integration test)

**Step 1: Write integration test**

Add to `internal/wiki/pipeline_test.go`:

```go
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
		"Summarize": "Summary: Utility functions\nKeyTypes: none\nPatterns: Functional\nConcerns: none",
		"architecture overview": "Layered architecture with cmd and lib modules.",
		"improvement suggestions": "1. Add tests\n2. Add documentation",
		"sequence diagram": "sequenceDiagram\n  Client->>Server: Request",
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
```

**Step 2: Run integration test**

Run: `go test ./internal/wiki/... -run TestRunPipelineIntegration -v`
Expected: PASS

**Step 3: Check coverage**

Run: `go test ./internal/wiki/... -cover`
Expected: Coverage >90%

**Step 4: Commit**

```bash
git add internal/wiki/pipeline_test.go
git commit -m "[BEHAVIORAL] Add wiki pipeline integration test"
```

---

### Task 11: Verify & Clean Up

**Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: All tests pass

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

**Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No output (all files formatted)

**Step 4: Verify wiki coverage**

Run: `go test ./internal/wiki/... -cover`
Expected: >90%

**Step 5: Final commit (if any cleanup was needed)**

```bash
git commit -m "[STRUCTURAL] Clean up wiki pipeline code quality"
```
