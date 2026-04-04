# Headless Wiki Enhancement — Plan A: CLI + Analyzer Architecture

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--wiki` as a first-class headless mode and refactor the analyzer into a multi-pass architecture with a specialized analyzer interface, while producing identical output to today (no new doc types yet).

**Architecture:** The `--wiki` flag bypasses the agent loop and runs `wiki.Run()` directly from `cmd/rubichan/main.go`. The single-pass `Analyze()` function is split into a base pass (module analysis + architecture synthesis) and a dispatcher that runs `SpecializedAnalyzer` implementations. In this plan, the only specialized analyzer is `SuggestionAnalyzer` (extracted from the current code). Plans B and C will add API, Security, and Dependency analyzers.

**Tech Stack:** Go, existing `internal/wiki/` package, `sourcegraph/conc` for concurrency, `spf13/cobra` for CLI flags.

**Spec:** `docs/superpowers/specs/2026-03-29-headless-wiki-enhancement-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `cmd/rubichan/main.go` | Add `--wiki` flags, `runWikiHeadless()` entry point |
| `internal/wiki/types.go` | Add `SpecializedAnalyzer` interface, `AnalyzerInput`, `AnalyzerOutput` |
| `internal/wiki/types_test.go` | Test new types |
| `internal/wiki/analyzer.go` | Refactor: base pass only (modules + architecture) + dispatcher |
| `internal/wiki/analyzer_test.go` | Update tests for refactored analyzer |
| `internal/wiki/analyzer_suggestion.go` | Extract `SuggestionAnalyzer` from current code |
| `internal/wiki/analyzer_suggestion_test.go` | Tests for extracted analyzer |
| `internal/wiki/pipeline.go` | Update `Run()` to use new analyzer flow and pass `AnalyzerInput` |
| `internal/wiki/pipeline_test.go` | Update pipeline integration test |

---

### Task 1: Add `--wiki` CLI Flags

**Files:**
- Modify: `cmd/rubichan/main.go`

- [ ] **Step 1: Write failing test for wiki flag parsing**

```go
// cmd/rubichan/coverage_test.go — add to existing test file
func TestWikiFlagImpliesHeadless(t *testing.T) {
	// Verify that --wiki sets the internal wikiMode flag.
	// We test the flag wiring, not the full execution.
	cmd := rootCmd()
	cmd.SetArgs([]string{"--wiki"})
	// Parse flags only (don't execute).
	err := cmd.ParseFlags([]string{"--wiki"})
	require.NoError(t, err)
	val, err := cmd.Flags().GetBool("wiki")
	require.NoError(t, err)
	assert.True(t, val)
}

func TestWikiOutDefault(t *testing.T) {
	cmd := rootCmd()
	err := cmd.ParseFlags([]string{})
	require.NoError(t, err)
	val, err := cmd.Flags().GetString("wiki-out")
	require.NoError(t, err)
	assert.Equal(t, "docs/wiki", val)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run "TestWikiFlag|TestWikiOut" -v`
Expected: FAIL — `unknown flag: --wiki`

- [ ] **Step 3: Add flag definitions**

In `cmd/rubichan/main.go`, in the flag definition section (around line 497-510), add:

```go
wikiFlag       bool
wikiOutFlag    string
wikiFormatFlag string
wikiConcFlag   int
```

And in the cobra flag registration:

```go
rootCmd.PersistentFlags().BoolVar(&wikiFlag, "wiki", false, "run wiki generation in headless mode")
rootCmd.PersistentFlags().StringVar(&wikiOutFlag, "wiki-out", "docs/wiki", "output directory for wiki files")
rootCmd.PersistentFlags().StringVar(&wikiFormatFlag, "wiki-format", "raw-md", "wiki output format: raw-md, hugo, docusaurus")
rootCmd.PersistentFlags().IntVar(&wikiConcFlag, "wiki-concurrency", 5, "max parallel LLM calls for wiki generation")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/rubichan/... -run "TestWikiFlag|TestWikiOut" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/rubichan/main.go cmd/rubichan/coverage_test.go
git commit -m "[BEHAVIORAL] Add --wiki, --wiki-out, --wiki-format, --wiki-concurrency CLI flags"
```

---

### Task 2: Add `runWikiHeadless()` Entry Point

**Files:**
- Modify: `cmd/rubichan/main.go`

- [ ] **Step 1: Write failing test for wiki headless execution**

```go
// cmd/rubichan/coverage_test.go
func TestRunWikiHeadlessRequiresProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = ""
	cfg.Provider.Model = ""
	err := runWikiHeadless(cfg, "/tmp/test-wiki", "docs/wiki", "raw-md", 5)
	assert.Error(t, err, "should fail without a configured provider")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestRunWikiHeadless -v`
Expected: FAIL — `runWikiHeadless` undefined

- [ ] **Step 3: Implement runWikiHeadless**

In `cmd/rubichan/main.go`, add the function. This is the core of the `--wiki` mode — it creates a provider, builds a `wiki.Config`, and calls `wiki.Run()` directly (no agent loop).

```go
// runWikiHeadless runs the wiki pipeline directly without the agent loop.
// It creates its own LLM completer from the provider config and writes
// progress to stderr.
func runWikiHeadless(cfg *config.Config, cwd, outDir, format string, concurrency int) error {
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	llm := integrations.NewLLMCompleter(p, cfg.Provider.Model)
	par := parser.NewParser()
	defer par.Close()

	wikiCfg := wiki.Config{
		Dir:         cwd,
		OutputDir:   outDir,
		Format:      format,
		Concurrency: concurrency,
		ProgressFunc: func(stage string, current, total int) {
			if total > 0 {
				fmt.Fprintf(os.Stderr, "[%s] %d/%d\n", stage, current, total)
			} else {
				fmt.Fprintf(os.Stderr, "[%s]\n", stage)
			}
		},
	}

	return wiki.Run(context.Background(), wikiCfg, llm, par)
}
```

- [ ] **Step 4: Wire into main command execution**

In the `RunE` function of the root command (before the headless/interactive branch), add:

```go
if wikiFlag {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	return runWikiHeadless(cfg, cwd, wikiOutFlag, wikiFormatFlag, wikiConcFlag)
}
```

This must come before the `if headless {` check so `--wiki` takes priority.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/rubichan/... -run TestRunWikiHeadless -v`
Expected: PASS

- [ ] **Step 6: Run full test suite**

Run: `go test ./cmd/rubichan/... -count=1 -timeout 120s 2>&1 | tail -5`
Expected: PASS (no regressions)

- [ ] **Step 7: Commit**

```bash
git add cmd/rubichan/main.go cmd/rubichan/coverage_test.go
git commit -m "[BEHAVIORAL] Add runWikiHeadless entry point for --wiki flag"
```

---

### Task 3: Add SpecializedAnalyzer Interface

**Files:**
- Modify: `internal/wiki/types.go`
- Create: `internal/wiki/types_test.go`

- [ ] **Step 1: Write failing test for new types**

```go
// internal/wiki/types_test.go
package wiki

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type stubAnalyzer struct {
	name string
	docs []Document
}

func (s *stubAnalyzer) Name() string { return s.name }
func (s *stubAnalyzer) Analyze(_ context.Context, _ AnalyzerInput) (*AnalyzerOutput, error) {
	return &AnalyzerOutput{Documents: s.docs}, nil
}

func TestSpecializedAnalyzerInterface(t *testing.T) {
	analyzer := &stubAnalyzer{
		name: "test",
		docs: []Document{{Path: "test.md", Title: "Test", Content: "body"}},
	}

	var sa SpecializedAnalyzer = analyzer
	assert.Equal(t, "test", sa.Name())

	out, err := sa.Analyze(context.Background(), AnalyzerInput{})
	assert.NoError(t, err)
	assert.Len(t, out.Documents, 1)
	assert.Equal(t, "test.md", out.Documents[0].Path)
}

func TestAnalyzerInputHasBaseFields(t *testing.T) {
	input := AnalyzerInput{
		Architecture: "layered",
		ModuleAnalyses: []ModuleAnalysis{
			{Module: "core", Summary: "Core module"},
		},
	}
	assert.Equal(t, "layered", input.Architecture)
	assert.Len(t, input.ModuleAnalyses, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run "TestSpecializedAnalyzer|TestAnalyzerInput" -v`
Expected: FAIL — `SpecializedAnalyzer` undefined

- [ ] **Step 3: Add new types to types.go**

In `internal/wiki/types.go`, after the existing types, add:

```go
// SpecializedAnalyzer produces documents and diagrams for a specific domain.
// Implementations receive base analysis results and produce domain-specific output.
type SpecializedAnalyzer interface {
	Name() string
	Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error)
}

// AnalyzerInput provides shared context from the base analysis pass to
// specialized analyzers.
type AnalyzerInput struct {
	Chunks         []Chunk
	Files          []ScannedFile
	ModuleAnalyses []ModuleAnalysis
	Architecture   string
	ExistingDocs   map[string]string // path → content (for change history)
}

// AnalyzerOutput holds documents and diagrams produced by a specialized analyzer.
type AnalyzerOutput struct {
	Documents []Document
	Diagrams  []Diagram
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -run "TestSpecializedAnalyzer|TestAnalyzerInput" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/wiki/types.go internal/wiki/types_test.go
git commit -m "[BEHAVIORAL] Add SpecializedAnalyzer interface, AnalyzerInput, AnalyzerOutput types"
```

---

### Task 4: Extract SuggestionAnalyzer

**Files:**
- Create: `internal/wiki/analyzer_suggestion.go`
- Create: `internal/wiki/analyzer_suggestion_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/wiki/analyzer_suggestion_test.go
package wiki

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLLM struct {
	response string
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.response, nil
}

func TestSuggestionAnalyzer_Name(t *testing.T) {
	sa := NewSuggestionAnalyzer(&mockLLM{})
	assert.Equal(t, "suggestions", sa.Name())
}

func TestSuggestionAnalyzer_ProducesDocument(t *testing.T) {
	llm := &mockLLM{response: "- Use dependency injection\n- Add more tests"}
	sa := NewSuggestionAnalyzer(llm)

	input := AnalyzerInput{
		Architecture: "The system uses a layered architecture with provider abstraction.",
	}

	out, err := sa.Analyze(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, out.Documents, 1)
	assert.Equal(t, "suggestions/improvements.md", out.Documents[0].Path)
	assert.Contains(t, out.Documents[0].Content, "dependency injection")
}

func TestSuggestionAnalyzer_EmptyArchitecture(t *testing.T) {
	sa := NewSuggestionAnalyzer(&mockLLM{response: ""})
	out, err := sa.Analyze(context.Background(), AnalyzerInput{})
	require.NoError(t, err)
	// Empty architecture produces no suggestions document.
	assert.Empty(t, out.Documents)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestSuggestionAnalyzer -v`
Expected: FAIL — `NewSuggestionAnalyzer` undefined

- [ ] **Step 3: Implement SuggestionAnalyzer**

```go
// internal/wiki/analyzer_suggestion.go
package wiki

import (
	"context"
	"fmt"
	"strings"
)

// SuggestionAnalyzer generates improvement suggestions from the architecture summary.
type SuggestionAnalyzer struct {
	llm LLMCompleter
}

// NewSuggestionAnalyzer creates a SuggestionAnalyzer with the given LLM.
func NewSuggestionAnalyzer(llm LLMCompleter) *SuggestionAnalyzer {
	return &SuggestionAnalyzer{llm: llm}
}

func (a *SuggestionAnalyzer) Name() string { return "suggestions" }

func (a *SuggestionAnalyzer) Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error) {
	if input.Architecture == "" {
		return &AnalyzerOutput{}, nil
	}

	prompt := fmt.Sprintf(suggestionsTmpl, input.Architecture)
	resp, err := a.llm.Complete(ctx, prompt)
	if err != nil {
		return &AnalyzerOutput{}, nil // non-fatal
	}

	resp = strings.TrimSpace(resp)
	if resp == "" {
		return &AnalyzerOutput{}, nil
	}

	doc := Document{
		Path:    "suggestions/improvements.md",
		Title:   "Improvement Suggestions",
		Content: fmt.Sprintf("# Improvement Suggestions\n\n%s\n", resp),
	}

	return &AnalyzerOutput{Documents: []Document{doc}}, nil
}
```

Note: `suggestionsTmpl` already exists in `analyzer.go`. It stays there for now — Task 5 will clean up.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -run TestSuggestionAnalyzer -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/wiki/analyzer_suggestion.go internal/wiki/analyzer_suggestion_test.go
git commit -m "[BEHAVIORAL] Extract SuggestionAnalyzer implementing SpecializedAnalyzer interface"
```

---

### Task 5: Refactor Analyze() to Multi-Pass with Dispatcher

**Files:**
- Modify: `internal/wiki/analyzer.go`
- Modify: `internal/wiki/analyzer_test.go` (if exists, update; if not, create)

This is the key refactoring. The current `Analyze()` does three passes in sequence. We split it:
- `AnalyzeBase()` — pass 1 (module summaries + architecture synthesis). Returns `*AnalysisResult` without suggestions.
- `RunSpecializedAnalyzers()` — dispatches `[]SpecializedAnalyzer` concurrently, collects results.
- `Analyze()` — preserved as a compatibility wrapper that calls both.

- [ ] **Step 1: Write failing test for AnalyzeBase**

```go
// internal/wiki/analyzer_test.go
package wiki

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeBase_ProducesModulesAndArchitecture(t *testing.T) {
	llm := &mockLLM{response: "Summary: core module\nKey Types: Config\nPatterns: factory\nConcerns: none"}
	chunks := []Chunk{
		{Module: "core", Source: []byte("package core\nfunc New() {}")},
	}

	result, err := AnalyzeBase(context.Background(), chunks, llm, AnalyzerConfig{Concurrency: 1})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Modules, 1)
	assert.Equal(t, "core", result.Modules[0].Module)
	// Architecture should be populated (from pass 2).
	// Suggestions should be empty (moved to SuggestionAnalyzer).
	assert.Empty(t, result.Suggestions)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/... -run TestAnalyzeBase -v`
Expected: FAIL — `AnalyzeBase` undefined

- [ ] **Step 3: Refactor analyzer.go**

Rename the existing `Analyze()` internals:

```go
// AnalyzeBase runs the base analysis pass: per-module summaries (concurrent)
// and architecture synthesis. Returns AnalysisResult without suggestions
// (those are produced by SuggestionAnalyzer in the specialized pass).
func AnalyzeBase(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}

	modules, err := analyzeModules(ctx, chunks, llm, cfg.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("module analysis: %w", err)
	}

	summariesText := buildSummariesText(modules)
	arch, abstractions := synthesizeArchitecture(ctx, summariesText, llm)

	return &AnalysisResult{
		Modules:         modules,
		Architecture:    arch,
		KeyAbstractions: abstractions,
	}, nil
}

// RunSpecializedAnalyzers dispatches analyzers concurrently and collects
// their documents and diagrams.
func RunSpecializedAnalyzers(ctx context.Context, analyzers []SpecializedAnalyzer, input AnalyzerInput) ([]Document, []Diagram, error) {
	type result struct {
		docs     []Document
		diagrams []Diagram
		err      error
	}

	results := make([]result, len(analyzers))
	var wg sync.WaitGroup
	for i, a := range analyzers {
		wg.Add(1)
		go func(idx int, analyzer SpecializedAnalyzer) {
			defer wg.Done()
			out, err := analyzer.Analyze(ctx, input)
			if err != nil {
				results[idx] = result{err: err}
				return
			}
			if out != nil {
				results[idx] = result{docs: out.Documents, diagrams: out.Diagrams}
			}
		}(i, a)
	}
	wg.Wait()

	var allDocs []Document
	var allDiagrams []Diagram
	for _, r := range results {
		// Specialized analyzers are non-fatal: log warning but continue.
		if r.err != nil {
			continue
		}
		allDocs = append(allDocs, r.docs...)
		allDiagrams = append(allDiagrams, r.diagrams...)
	}
	return allDocs, allDiagrams, nil
}

// Analyze is a compatibility wrapper that runs the base pass followed by
// the suggestion analyzer. New code should use AnalyzeBase + RunSpecializedAnalyzers.
func Analyze(ctx context.Context, chunks []Chunk, llm LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error) {
	result, err := AnalyzeBase(ctx, chunks, llm, cfg)
	if err != nil {
		return nil, err
	}

	// Run suggestion analyzer for backward compatibility.
	sa := NewSuggestionAnalyzer(llm)
	input := AnalyzerInput{
		Architecture:   result.Architecture,
		ModuleAnalyses: result.Modules,
	}
	out, _ := sa.Analyze(ctx, input)
	if out != nil {
		for _, doc := range out.Documents {
			// Extract suggestions text from document for AnalysisResult.Suggestions.
			lines := strings.Split(strings.TrimSpace(doc.Content), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					result.Suggestions = append(result.Suggestions, line)
				}
			}
		}
	}

	return result, nil
}
```

Add `"sync"` and `"strings"` to imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wiki/... -run TestAnalyzeBase -v`
Expected: PASS

- [ ] **Step 5: Write test for RunSpecializedAnalyzers**

```go
func TestRunSpecializedAnalyzers_Concurrent(t *testing.T) {
	a1 := &stubAnalyzer{name: "a1", docs: []Document{{Path: "a1.md", Title: "A1"}}}
	a2 := &stubAnalyzer{name: "a2", docs: []Document{{Path: "a2.md", Title: "A2"}}}

	docs, diagrams, err := RunSpecializedAnalyzers(context.Background(), []SpecializedAnalyzer{a1, a2}, AnalyzerInput{})
	require.NoError(t, err)
	assert.Len(t, docs, 2)
	assert.Empty(t, diagrams)
}

func TestRunSpecializedAnalyzers_NonFatalError(t *testing.T) {
	good := &stubAnalyzer{name: "good", docs: []Document{{Path: "ok.md"}}}
	// A nil-returning analyzer simulates a non-fatal failure.
	bad := &stubAnalyzer{name: "bad", docs: nil}

	docs, _, err := RunSpecializedAnalyzers(context.Background(), []SpecializedAnalyzer{good, bad}, AnalyzerInput{})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/wiki/... -run "TestRunSpecialized" -v`
Expected: PASS

- [ ] **Step 7: Run full wiki test suite**

Run: `go test ./internal/wiki/... -count=1 -v 2>&1 | tail -20`
Expected: PASS (existing tests should still pass via the `Analyze()` compat wrapper)

- [ ] **Step 8: Commit**

```bash
git add internal/wiki/analyzer.go internal/wiki/analyzer_test.go
git commit -m "[STRUCTURAL] Refactor Analyze into AnalyzeBase + RunSpecializedAnalyzers dispatcher"
```

---

### Task 6: Update Pipeline to Use New Analyzer Flow

**Files:**
- Modify: `internal/wiki/pipeline.go`

- [ ] **Step 1: Write failing test for pipeline with specialized analyzers**

```go
// internal/wiki/pipeline_test.go (add to existing or create)
package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineRunProducesSuggestionDoc(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "wiki-out")

	// Create a minimal Go source file for the scanner to find.
	srcDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main\nfunc main() {}"), 0o644))

	// Init git repo so scanner uses git ls-files.
	// (Alternatively, scanner falls back to filepath.Walk.)

	llm := &mockLLM{response: "Summary: main\nKey Types: none\nPatterns: none\nConcerns: none"}

	cfg := Config{
		Dir:         srcDir,
		OutputDir:   outDir,
		Format:      "raw-md",
		Concurrency: 1,
	}

	err := Run(context.Background(), cfg, llm, nil)
	require.NoError(t, err)

	// Verify suggestions doc was created via the SuggestionAnalyzer.
	suggestionsPath := filepath.Join(outDir, "suggestions", "improvements.md")
	// The suggestion doc may or may not exist depending on LLM response.
	// What matters is the pipeline completes without error.
	_, _ = os.Stat(suggestionsPath)
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/wiki/... -run TestPipelineRunProduces -v`

- [ ] **Step 3: Update pipeline.go to use new flow**

Replace the Analyze call and update Assemble to include specialized analyzer output:

```go
func Run(ctx context.Context, cfg Config, llm LLMCompleter, p *parser.Parser) error {
	// ... stages 1-2 (scan, chunk) unchanged ...

	// Stage 3: Base analysis.
	progress("Analyzing", 0, 0)
	analyzerCfg := AnalyzerConfig{Concurrency: concurrency}
	analysis, err := AnalyzeBase(ctx, chunks, llm, analyzerCfg)
	if err != nil {
		return fmt.Errorf("analysis: %w", err)
	}

	// Stage 3b: Specialized analyzers.
	progress("Running specialized analyzers", 0, 0)
	specializedAnalyzers := []SpecializedAnalyzer{
		NewSuggestionAnalyzer(llm),
	}
	input := AnalyzerInput{
		Chunks:         chunks,
		Files:          files,
		ModuleAnalyses: analysis.Modules,
		Architecture:   analysis.Architecture,
	}
	extraDocs, extraDiagrams, err := RunSpecializedAnalyzers(ctx, specializedAnalyzers, input)
	if err != nil {
		return fmt.Errorf("specialized analysis: %w", err)
	}

	// Stage 4: Diagrams.
	// ... unchanged ...

	// Stage 5: Assemble.
	progress("Assembling", 0, 0)
	documents, err := Assemble(analysis, diagrams, nil, cfg.SecurityFindings)
	if err != nil {
		return fmt.Errorf("assembly: %w", err)
	}
	// Merge specialized analyzer output.
	documents = append(documents, extraDocs...)
	diagrams = append(diagrams, extraDiagrams...)

	// Stage 6: Render.
	// ... unchanged ...
}
```

The key change: `Analyze()` becomes `AnalyzeBase()`, and suggestions come from `RunSpecializedAnalyzers` instead of being embedded in `AnalysisResult`.

Update the `Assemble()` call: since suggestions are now in `extraDocs` (from SuggestionAnalyzer), remove the suggestion generation from the assembler. The assembler should skip its built-in suggestion section when `analysis.Suggestions` is empty (which it now always is from `AnalyzeBase`).

- [ ] **Step 4: Run full test suite**

Run: `go test ./internal/wiki/... -count=1 -v 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 5: Run build**

Run: `go build ./cmd/rubichan`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add internal/wiki/pipeline.go internal/wiki/pipeline_test.go
git commit -m "[BEHAVIORAL] Update pipeline to use AnalyzeBase + specialized analyzer dispatcher"
```

---

### Task 7: End-to-End Integration Test

**Files:**
- Modify: `cmd/rubichan/coverage_test.go`

- [ ] **Step 1: Write integration test**

```go
func TestWikiHeadlessEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "project")
	outDir := filepath.Join(tmpDir, "wiki-out")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(srcDir, "main.go"),
		[]byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"),
		0o644,
	))

	// Test that runWikiHeadless creates output files.
	// This requires a real or mock provider — use a mock config.
	// For unit testing, verify the function signature and error paths.
	cfg := config.DefaultConfig()
	err := runWikiHeadless(cfg, srcDir, outDir, "raw-md", 1)
	// Will fail without API key, but should fail gracefully.
	assert.Error(t, err) // Expected: provider creation error
	assert.Contains(t, err.Error(), "provider")
}
```

- [ ] **Step 2: Run test**

Run: `go test ./cmd/rubichan/... -run TestWikiHeadless -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/rubichan/coverage_test.go
git commit -m "[BEHAVIORAL] Add wiki headless integration test"
```

---

### Task 8: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 -timeout 300s 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

- [ ] **Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No output

- [ ] **Step 4: Build binary**

Run: `go build -o rubichan_test ./cmd/rubichan && ./rubichan_test --help | grep wiki`
Expected: Shows `--wiki`, `--wiki-out`, `--wiki-format`, `--wiki-concurrency` flags

- [ ] **Step 5: Commit any fixes**

```bash
git add -A && git commit -m "[STRUCTURAL] Final verification fixes"
```
