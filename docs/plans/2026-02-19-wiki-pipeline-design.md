# Wiki Pipeline Design

## Goal

Build the full 6-stage wiki generator pipeline that analyzes an entire codebase and produces a static documentation site with architecture diagrams, module documentation, and improvement suggestions.

## Scope

**In scope:**
- All 6 pipeline stages: scanner, chunker, LLM analyzer, diagram generator, assembler, renderer
- Three output formats: raw Markdown (default), Hugo, Docusaurus
- Mermaid diagram generation (architecture, dependency, data-flow, sequence)
- Skill wiki section contributions via manifest `wiki:` config
- CLI entrypoint (`rubichan wiki`)
- Multi-pass LLM analysis (per-module, cross-cutting synthesis, suggestions)
- Concurrent LLM calls within the analyzer stage

**Out of scope (deferred):**
- Incremental regeneration (FR-3.7, P2)
- Test execution integration (FR-3.8, P1)
- LLM budget/prioritization (analyze all files)
- D2 and Graphviz DOT diagram formats
- Security engine integration (placeholder pages only)
- Xcode project analysis

## Architecture

Sequential pipeline. Each stage runs to completion before the next begins. Data flows via typed Go structs. Concurrency is internal to the analyzer stage (parallel LLM calls via `errgroup`).

```
Scanner(dir) → []ScannedFile
    ↓
Chunker(files) → []Chunk
    ↓
Analyzer(chunks, llm) → *AnalysisResult
    ↓
DiagramGen(files, analysis, llm) → []Diagram
    ↓
Assembler(analysis, diagrams, skillSections) → []Document
    ↓
Renderer(documents, format, outputDir) → error
```

## Package Structure

```
internal/wiki/
  pipeline.go     -- Pipeline orchestrator (Run function)
  scanner.go      -- Stage 1: walks codebase, parses with tree-sitter
  chunker.go      -- Stage 2: groups files into LLM-sized chunks
  analyzer.go     -- Stage 3: multi-pass LLM analysis
  diagrams.go     -- Stage 4: Mermaid diagram generation
  assembler.go    -- Stage 5: builds document structure
  renderer.go     -- Stage 6: writes output files
  types.go        -- Shared data types flowing between stages

cmd/rubichan/
  wiki.go         -- cobra subcommand wiring
```

## Data Types

```go
// Stage 1 output
type ScannedFile struct {
    Path      string
    Language  string
    Functions []parser.FunctionDef
    Imports   []string
    Size      int64
    Module    string  // inferred package/module grouping
}

// Stage 2 output
type Chunk struct {
    Module string
    Files  []ScannedFile
    Source []byte  // concatenated structured summary for LLM context
}

// Stage 3 output
type AnalysisResult struct {
    Modules         []ModuleAnalysis
    Architecture    string  // cross-cutting synthesis
    KeyAbstractions string
    Suggestions     []string
}

type ModuleAnalysis struct {
    Module   string
    Summary  string
    KeyTypes string
    Patterns string
    Concerns string
}

// Stage 4 output
type Diagram struct {
    Title   string
    Type    string  // "architecture", "dependency", "data-flow", "sequence"
    Content string  // Mermaid source
}

// Stage 5 output
type Document struct {
    Path    string  // relative path in output (e.g., "modules/parser.md")
    Title   string
    Content string  // Markdown
}

// Skill wiki contributions
type SkillWikiSection struct {
    SkillName string
    Title     string
    Content   string
    Diagrams  []Diagram
}
```

## Stage 1: Codebase Scanner

- Uses `git ls-files` to get tracked file list (respects .gitignore automatically)
- Falls back to `filepath.WalkDir` with hardcoded skip patterns if not in a git repo
- Skips: binary files, vendor/, node_modules/, .git/, build artifacts
- For each file with a supported extension, calls `parser.Parse()` to extract functions and imports
- For unsupported extensions, records the file (path, size, language="unknown") without AST extraction
- Infers module grouping from directory structure:
  - Go: directory = package
  - Python: directory with `__init__.py` or just directory
  - JS/TS: directory (or nearest `package.json`)
  - Default: parent directory name

```go
func Scan(ctx context.Context, dir string, p *parser.Parser) ([]ScannedFile, error)
```

Sequential file-by-file processing. Tree-sitter parsing is ~1ms per file, so 500 files completes in under 1 second.

## Stage 2: Context-Aware Chunker

- Groups `ScannedFile` entries by `Module` field
- For each module, builds a structured text summary:
  1. File paths and languages
  2. Function/method signatures (name + line range)
  3. Import lists
  4. Full source for small files (<500 lines); for large files, function signatures + first 10 lines of each function body
- Enforces max chunk size (default 100k characters, ~25k tokens)
- If a module exceeds the limit, splits into multiple chunks
- Each chunk includes a preamble describing the module context

```go
type ChunkerConfig struct {
    MaxChunkSize int  // max characters per chunk (default 100_000)
    MaxFileLines int  // include full source below this threshold (default 500)
}

func Chunk(files []ScannedFile, sourceReader SourceReader, cfg ChunkerConfig) ([]Chunk, error)
```

`SourceReader` is an interface (`ReadFile(path string) ([]byte, error)`) for testability.

## Stage 3: Multi-Pass LLM Analyzer

Three analysis passes using `LLMCompleter`:

**Pass 1 — Per-module summarization:**
- For each Chunk, sends a prompt asking the LLM to summarize: purpose, key types/interfaces, design patterns, public API, concerns
- Runs concurrently via `errgroup` with configurable concurrency (default 5)
- Produces a `ModuleAnalysis` per chunk

**Pass 2 — Cross-cutting synthesis:**
- Takes all ModuleAnalysis summaries as input (concatenated)
- Single LLM call for: architecture overview, key abstractions, data flow, dependency relationships, cross-cutting concerns
- Produces `Architecture` and `KeyAbstractions` fields

**Pass 3 — Suggestions:**
- Takes architecture synthesis + module summaries
- Single LLM call for improvement opportunities, potential issues, refactoring suggestions
- Produces the `Suggestions` list

```go
type AnalyzerConfig struct {
    Concurrency int  // max parallel LLM calls (default 5)
}

func Analyze(ctx context.Context, chunks []Chunk, llm *integrations.LLMCompleter, cfg AnalyzerConfig) (*AnalysisResult, error)
```

**Error resilience:** If an individual module analysis fails (LLM error, timeout), log a warning and continue. A wiki with 45/50 modules is better than no wiki.

**Prompts:** Go `text/template` constants in `analyzer.go`. No external prompt files.

## Stage 4: Diagram Generator

Generates Mermaid diagrams from structured data. Three diagram types are built programmatically, one uses an LLM call.

**Programmatic diagrams:**
1. **Architecture overview** — `graph TD` showing modules and their relationships (from cross-module imports)
2. **Dependency graph** — `graph LR` showing import relationships between modules
3. **Data flow** — `flowchart LR` showing key data paths (from function call patterns and imports)

**LLM-generated diagram:**
4. **Sequence diagrams** — Single LLM call with the architecture synthesis as context, asking for Mermaid sequence diagrams of the 2-3 most important flows

```go
type DiagramConfig struct {
    Format string  // "mermaid" (default); "d2" and "dot" return unsupported error
}

func GenerateDiagrams(ctx context.Context, files []ScannedFile, analysis *AnalysisResult, llm *integrations.LLMCompleter, cfg DiagramConfig) ([]Diagram, error)
```

Only Mermaid is implemented. D2/DOT return an error for now.

## Stage 5: Document Assembler

Builds the document tree from analysis results, diagrams, and skill contributions.

**Output structure:**
```
_index.md                           -- project overview
architecture/
  overview.md                       -- architecture diagram + description
  dependencies.md                   -- dependency graph
  data-flow.md                      -- data flow + sequence diagrams
modules/
  _index.md                         -- module listing
  <name>.md                         -- one page per ModuleAnalysis
code-structure/
  overview.md                       -- key abstractions, design patterns
security/
  overview.md                       -- placeholder (pending security engine)
suggestions/
  improvements.md                   -- suggestions from Pass 3
skill-contributed/
  <title>.md                        -- from active skills with wiki: config
```

**Skill contributions:**
- Queries skill runtime for active skills with `Wiki` config
- Fires `HookOnBeforeWikiSection` before each skill section
- Reads skill template files, renders with analysis context
- Places in `skill-contributed/`

```go
func Assemble(analysis *AnalysisResult, diagrams []Diagram, skillSections []SkillWikiSection) ([]Document, error)
```

## Stage 6: Site Renderer

Writes documents to disk in the chosen format.

**Raw Markdown (default):**
- Creates directory tree under output dir
- Writes each Document as a `.md` file
- No external tools required

**Hugo:**
- Adds Hugo front matter (`weight`, `menu` fields)
- Generates `config.toml` with theme config
- Wraps Mermaid in Hugo shortcodes if needed
- User must have Hugo installed to build

**Docusaurus:**
- Generates `docusaurus.config.js` with site metadata
- Writes `.md` files with Docusaurus front matter (`sidebar_position`, `sidebar_label`)
- Mermaid works natively with `@docusaurus/theme-mermaid`
- User must have Node.js + Docusaurus installed to build

```go
type RendererConfig struct {
    Format    string  // "raw-md" (default), "hugo", "docusaurus"
    OutputDir string  // default "docs/wiki"
}

func Render(documents []Document, cfg RendererConfig) error
```

## CLI Entrypoint

```bash
rubichan wiki [path]                          # raw-md to docs/wiki/
rubichan wiki --format=hugo --output=./site   # Hugo format
rubichan wiki --format=docusaurus             # Docusaurus format
rubichan wiki --diagrams=mermaid              # (only mermaid for now)
rubichan wiki --skills=hipaa-compliance       # activate specific skills
```

**Pipeline orchestrator:**

```go
type Config struct {
    Dir          string
    OutputDir    string
    Format       string          // raw-md, hugo, docusaurus
    DiagramFmt   string          // mermaid
    Concurrency  int             // parallel LLM calls
    SkillRuntime *skills.Runtime // optional, for skill wiki sections
}

func Run(ctx context.Context, cfg Config, llm *integrations.LLMCompleter, p *parser.Parser) error
```

`Run` calls each stage sequentially and reports progress to stderr.

## Error Handling

- Scanner errors on unreadable files: skip file, log warning
- Chunker: pure computation, errors are bugs (return error)
- Analyzer: per-module LLM failures are warnings, pipeline continues
- Diagram generator: LLM failure for sequence diagrams is a warning, programmatic diagrams always succeed
- Assembler: pure computation, errors are bugs
- Renderer: I/O errors (permissions, disk full) are fatal

## Testing Strategy

- Each stage is independently testable with typed inputs/outputs
- Scanner: test with temp directory containing known files
- Chunker: test with constructed `[]ScannedFile`, verify chunk sizes and grouping
- Analyzer: mock `LLMCompleter` returning canned responses
- Diagrams: verify Mermaid syntax of generated output
- Assembler: verify document paths and content structure
- Renderer: verify file tree written to temp directory
- Pipeline integration test: mock LLM, run full pipeline, verify output
- Target >90% coverage per CLAUDE.md
