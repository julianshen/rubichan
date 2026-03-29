# Headless Wiki Enhancement — Design Spec

## Goal

Extend rubichan's wiki generator to work as a first-class headless mode (`--wiki`), produce richer documentation (API docs, security design, enhanced architecture), and preserve change history across regenerations.

## Architecture Overview

The existing 6-stage wiki pipeline (Scan → Chunk → Analyze → Diagrams → Assemble → Render) is preserved. The key change is replacing the single-pass LLM analyzer with a multi-pass architecture: a base analysis pass followed by specialized domain analyzers running concurrently. A new change-history step runs before final rendering.

```
Scan → Chunk → Base Analysis (pass 1)
                    ↓
         ┌─────────┼──────────┬──────────┐
         ↓         ↓          ↓          ↓
    APIAnalyzer  SecurityAnalyzer  DependencyAnalyzer  SuggestionAnalyzer
         ↓         ↓          ↓          ↓
         └─────────┼──────────┴──────────┘
                    ↓
              Assemble → ChangeHistory → Render
```

## 1. CLI & Headless Integration

### New Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--wiki` | bool | false | Run wiki generation (implies `--headless`, `--approve-cwd`) |
| `--wiki-out` | string | `docs/wiki` | Output directory for wiki files |
| `--wiki-format` | string | `raw-md` | Output format: `raw-md`, `hugo`, `docusaurus` |
| `--wiki-concurrency` | int | 5 | Max parallel LLM analysis calls |

### Invocation

```bash
# Default — generates to docs/wiki/ in raw-md format
rubichan --wiki

# Custom output
rubichan --wiki --wiki-out ./output/docs --wiki-format hugo

# Long output path
rubichan --wiki --wiki-out ./output/docs

# With model selection and timeout
rubichan --wiki --model "qwen/qwen3.5-397b-a17b" --timeout 10m

# JSON summary to stdout (files still written to disk)
rubichan --wiki --output json
```

### Behavior

- `--wiki` implies `--headless` and `--approve-cwd` (wiki only reads files, no mutations).
- The pipeline runs directly — no agent loop, no tool dispatch. This is faster and more reliable than routing through the LLM agent.
- Progress is emitted to stderr: `[1/6] Scanning files... [2/6] Chunking...` etc.
- Exit code 0 on success, 1 on failure.
- `--output json` writes a JSON summary to stdout (see Section 6). `--output markdown` (default) writes a brief completion message.
- `--wiki-out` is the canonical path flag. No generic `--output-dir` alias — wiki-specific flag avoids ambiguity with headless `--output` (format flag).

### Implementation Location

- `cmd/rubichan/main.go`: Add `--wiki` flag handling before the headless/interactive branch. When `--wiki` is set, call `runWikiHeadless()` which configures `wiki.Config` from flags and calls `wiki.Run()` directly.
- Reuse existing `wireWiki` for LLM completer setup.

## 2. Multi-Pass Analyzer Architecture

### Interface

```go
// SpecializedAnalyzer produces documents and diagrams for a specific domain.
type SpecializedAnalyzer interface {
    Name() string
    Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error)
}

type AnalyzerInput struct {
    Chunks         []Chunk
    Files          []ScannedFile
    ModuleAnalyses []ModuleAnalysis  // from base pass
    Architecture   string            // from base synthesis
    APIPatterns    []APIPattern      // from enhanced scanner
    ExistingDocs   map[string]string // path → content (for change history)
}

type AnalyzerOutput struct {
    Documents []Document
    Diagrams  []Diagram
}
```

### Execution Order

| Pass | Analyzer | Concurrency | Dependencies | Output Documents |
|------|----------|-------------|-------------|-----------------|
| 1 (base) | `ModuleAnalyzer` | Per-module parallel | Chunks | ModuleAnalysis[], Architecture, KeyAbstractions |
| 2a | `APIAnalyzer` | Per-module parallel | Chunks + ScannedFiles + APIPatterns | `api/*.md` |
| 2b | `SecurityAnalyzer` | Sequential (needs full picture) | All modules + Architecture | `security/*.md` |
| 2c | `DependencyAnalyzer` | Single call | Files + ModuleAnalysis | `architecture/dependencies.md`, `architecture/design-decisions.md` |
| 2d | `SuggestionAnalyzer` | Single call | Architecture | `suggestions/improvements.md` |

Pass 1 runs first (shared foundation). Passes 2a–2d run concurrently via `sourcegraph/conc` errgroup. Each analyzer receives the base analysis results and its domain-specific scanner data.

### File Organization

- `internal/wiki/analyzer.go` — Retains base module analysis (pass 1) + dispatcher for pass 2.
- `internal/wiki/analyzer_api.go` — `APIAnalyzer` implementation.
- `internal/wiki/analyzer_security.go` — `SecurityAnalyzer` implementation.
- `internal/wiki/analyzer_dependency.go` — `DependencyAnalyzer` implementation.
- `internal/wiki/analyzer_suggestion.go` — `SuggestionAnalyzer` (extracted from current analyzer.go).

## 3. Scanner Enhancement — API Pattern Detection

### New Output Type

```go
type APIPattern struct {
    Kind     string // "http", "grpc", "cli", "graphql", "websocket", "export"
    Method   string // "GET", "POST", etc. (HTTP only)
    Path     string // route path, command name, or service name
    Handler  string // function/method handling this
    File     string // source file path
    Line     int    // line number
    Language string // detected language
}
```

### Detection Strategy

The scanner performs a fast regex pass over source files to find API registration patterns. It does not need full AST understanding — raw pattern matches are passed to the LLM analyzer for structured interpretation.

| Language | HTTP Patterns | CLI Patterns | Export Patterns |
|----------|-------------|-------------|----------------|
| Go | `http.HandleFunc`, `r.Get/Post`, `e.GET`, `mux.Handle` | `cobra.Command{Use:`, `flag.String` | Capitalized functions in `pkg/` |
| Python | `@app.route`, `@router.get`, `urlpatterns` | `@click.command`, `add_parser` | `__all__`, non-underscore in `__init__.py` |
| JS/TS | `app.get/post`, `router.route`, `@Get()/@Post()` | `yargs.command`, `program.command` | `export function`, `export class` |
| Java | `@GetMapping`, `@PostMapping`, `@RequestMapping` | `@Command`, `@Parameters` | `public` in `*Controller`, `*Service` |
| Rust | `#[get("/")]`, `#[post("/")]`, `.route()` | `#[command]`, `clap::Command` | `pub fn`, `pub struct` in `lib.rs` |
| Ruby | `get '/'`, `post '/'`, `resources :` | `desc`, `method_option` (Thor) | `module_function`, public methods |

**Proto/GraphQL files** are detected by extension (`.proto`, `.graphql`, `.gql`) and passed to the API analyzer as-is.

### Implementation

- `internal/wiki/scanner.go`: Add `ScanAPIPatterns(files []ScannedFile) []APIPattern` function.
- Pattern definitions live in a `var apiPatterns` table keyed by language, making it easy to extend.
- `ScannedFile` gains an `APIPatterns []APIPattern` field populated during the scan stage.

## 4. New Document Types

### 4.1 API Documentation (`api/`)

Generated by `APIAnalyzer`. Documents are only produced when the corresponding API surface is detected.

| Document | Condition | Content |
|----------|-----------|---------|
| `api/_index.md` | Always (if any API detected) | Overview: what API surfaces exist, counts, entry points |
| `api/http-endpoints.md` | HTTP patterns found | Method, path, handler, parameters, request/response description |
| `api/grpc-services.md` | `.proto` files found | Service names, RPC methods, message types |
| `api/cli-commands.md` | CLI patterns found | Command tree, flags, descriptions |
| `api/public-interfaces.md` | Export patterns found | Exported functions/types with signatures, doc comments |
| `api/sequence-diagrams.md` | 2+ API patterns | LLM-generated Mermaid sequence diagrams for key flows |

**LLM prompt strategy:** One prompt per API category (not per endpoint). The prompt receives the raw scanner matches for that category plus surrounding source context, and produces structured markdown documentation.

### 4.2 Security Design (`security/`)

Generated by `SecurityAnalyzer` using three focused sub-prompts.

**Sub-prompt 1 — Auth & Trust Boundaries → `security/auth-and-access.md`:**
- Authentication mechanisms (JWT, OAuth, session, API keys)
- Authorization patterns (RBAC, middleware guards, permission checks)
- Trust boundaries (external input → processing → storage)
- Mermaid sequence diagram: auth flow

**Sub-prompt 2 — Threat Model (STRIDE) → `security/threat-model.md`:**
- Spoofing: identity verification gaps
- Tampering: input validation, data integrity
- Repudiation: audit logging
- Information Disclosure: data exposure, error message leaking
- Denial of Service: rate limiting, resource bounds
- Elevation of Privilege: privilege escalation paths
- Mermaid diagram: attack tree

**Sub-prompt 3 — Data Flow & Compliance → `security/data-flow.md`:**
- Sensitive data inventory (credentials, PII, tokens)
- Data flow: where sensitive data enters, transforms, stores, exits
- Encryption at rest and in transit
- Logging hygiene (what's logged, what's redacted)
- Mermaid diagram: data flow

The existing `security/overview.md` becomes a summary linking to the three detail docs, plus the existing security scanner findings.

### 4.3 Enhanced Existing Documents

**Architecture:**
- `architecture/overview.md` — Enhanced: layer-by-layer breakdown, component responsibilities, technology choices.
- `architecture/design-decisions.md` — NEW: ADR-style summaries of inferred architectural decisions (e.g., "Why X pattern is used for Y").
- `architecture/dependencies.md` — Enhanced: external dependency inventory with versions (from `go.mod`, `package.json`, `Cargo.toml`, `requirements.txt`), internal import graph with cycle detection, dependency risk notes.
- `architecture/data-flow.md` — Unchanged (already generates data flow + sequence diagrams).

**Modules:**
- `modules/<module>.md` — Enhanced: add public interface signatures (exported functions/types), usage examples from test files (if detected), test coverage indicators (presence of `*_test.go` / `test_*.py` / `*.test.ts`).

## 5. Change History Mechanism

### Flow

```
1. Before rendering: read existing docs from output dir into map[path]content
2. Run full pipeline (scan → analyze → assemble) producing new docs
3. For each document:
   a. No existing doc → write new file, no changelog
   b. Existing doc, content differs:
      - Extract existing "## Change History" section
      - LLM call: "summarize what changed" (old excerpt vs new excerpt)
      - Append dated entry to changelog
      - Write file with changelog at bottom
   c. Existing doc, content identical → skip write
```

### Changelog Format

Appended at the bottom of each document:

```markdown
---

## Change History

- **2026-03-29** — Initial generation
- **2026-04-05** — Added WebSocket transport to architecture overview; updated dependency graph with new ws package; security section now covers WebSocket authentication
- **2026-04-12** — Removed deprecated REST endpoints from API docs; added gRPC service definitions
```

### Implementation Details

- Marker: `## Change History` — everything after this line is preserved across regenerations.
- The new content for diffing excludes the changelog section (compare body only).
- Change summary LLM prompt receives first 500 lines of old content and first 500 lines of new content (not full docs — cost control).
- Prompts run concurrently for all changed docs.
- Max 50 changelog entries per doc; oldest trimmed when exceeded.
- Date format: UTC `YYYY-MM-DD`.
- A doc with zero content changes but a different changelog (e.g., from a prior partial run) is not considered "changed."

### File

- `internal/wiki/changelog.go` — `ApplyChangelog(existing, new map[string]Document, llm LLMCompleter) ([]Document, error)`

## 6. Output Structure

### Directory Layout

```
docs/wiki/
├── _index.md
├── architecture/
│   ├── overview.md              (enhanced)
│   ├── design-decisions.md      (new)
│   ├── dependencies.md          (enhanced)
│   └── data-flow.md
├── api/                         (new section)
│   ├── _index.md
│   ├── http-endpoints.md        (if detected)
│   ├── grpc-services.md         (if detected)
│   ├── cli-commands.md          (if detected)
│   ├── public-interfaces.md
│   └── sequence-diagrams.md
├── modules/
│   ├── _index.md
│   └── <module>.md              (enhanced)
├── code-structure/
│   └── overview.md
├── security/
│   ├── overview.md              (now summary + links)
│   ├── auth-and-access.md       (new)
│   ├── threat-model.md          (new)
│   └── data-flow.md             (new)
├── suggestions/
│   └── improvements.md
└── skill-contributed/
    └── <skill-title>.md
```

### JSON Summary Output

When `--output json` is set, this JSON is written to stdout (wiki files still written to disk):

```json
{
  "output_dir": "docs/wiki",
  "format": "raw-md",
  "documents": 18,
  "new_documents": 6,
  "updated_documents": 8,
  "unchanged_documents": 4,
  "diagrams": 12,
  "duration_ms": 45200,
  "api_surfaces": ["http", "cli", "public-interfaces"],
  "security_depth": ["auth", "stride", "compliance"]
}
```

## 7. Pipeline Config Changes

```go
type Config struct {
    Dir              string
    OutputDir        string
    Format           string                  // "raw-md", "hugo", "docusaurus"
    DiagramFmt       string                  // "mermaid"
    Concurrency      int                     // parallel LLM calls
    SecurityFindings []security.Finding
    ProgressFunc     func(stage string, current, total int)

    // New fields:
    EnableAPI        bool   // run API analyzer (default: true)
    EnableSecurity   bool   // run security analyzer (default: true)
    TrackChanges     bool   // enable change history (default: true)
}
```

`EnableAPI` and `EnableSecurity` default to true. They can be set to false to speed up generation when only basic architecture docs are needed. `TrackChanges` controls whether existing docs are read and changelogs maintained.

## 8. Error Handling

- Each specialized analyzer is non-fatal: if `APIAnalyzer` fails, the pipeline continues without API docs and logs a warning.
- Change history LLM failures produce a generic changelog entry: `"- **DATE** — Content updated"` instead of a specific summary.
- Scanner API pattern detection failures are silently skipped per-file (logged at debug level).
- The overall pipeline fails only if the base analysis (pass 1) fails or the renderer cannot write files.

## 9. Not In Scope (YAGNI)

- Incremental scanning (always full regeneration)
- Custom analyzer plugins (skills contribute sections via existing `HookOnBeforeWikiSection`)
- HTML rendering of Mermaid diagrams (stays as markdown code fences)
- Interactive diff viewer for change history
- Per-file wiki generation (always generates full wiki)
