# Wiki Plan B: New Analyzers + API Scanner

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add APIAnalyzer, SecurityAnalyzer, and DependencyAnalyzer as specialized analyzers, plus an API pattern scanner, producing 9 new document types.

**Architecture:** Each analyzer implements the existing `SpecializedAnalyzer` interface and is registered in `pipeline.go`'s specialized analyzer list. The API scanner extends `scanner.go` with regex-based pattern detection. All analyzers receive `AnalyzerInput` with base analysis results and produce `AnalyzerOutput` with documents and diagrams.

**Tech Stack:** Go, existing `internal/wiki/` package, LLM prompts via `text/template`, regex for API pattern detection.

**Spec:** `docs/superpowers/specs/2026-03-29-headless-wiki-enhancement-design.md` (Sections 3, 4.1, 4.2, 4.3)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/wiki/types.go` | Add `APIPattern` struct to AnalyzerInput |
| `internal/wiki/scanner_api.go` | New: regex-based API pattern detection across languages |
| `internal/wiki/scanner_api_test.go` | Tests for pattern detection |
| `internal/wiki/analyzer_api.go` | New: APIAnalyzer producing api/*.md documents |
| `internal/wiki/analyzer_api_test.go` | Tests |
| `internal/wiki/analyzer_security.go` | New: SecurityAnalyzer with 3 sub-prompts |
| `internal/wiki/analyzer_security_test.go` | Tests |
| `internal/wiki/analyzer_dependency.go` | New: DependencyAnalyzer with enhanced dep docs |
| `internal/wiki/analyzer_dependency_test.go` | Tests |
| `internal/wiki/pipeline.go` | Modify: register new analyzers, pass APIPatterns |

---

### Task 1: Add APIPattern Type and Extend AnalyzerInput

**Files:**
- Modify: `internal/wiki/types.go`

Add `APIPattern` struct and an `APIPatterns` field to `AnalyzerInput`:

```go
type APIPattern struct {
    Kind     string // "http", "grpc", "cli", "graphql", "websocket", "export"
    Method   string // "GET", "POST" etc. (HTTP only)
    Path     string // route path, command name, or service name
    Handler  string // function/method name
    File     string // source file path
    Line     int    // line number
    Language string
}
```

Add to `AnalyzerInput`:
```go
APIPatterns []APIPattern
```

Write tests verifying the new struct fields. Commit with `[BEHAVIORAL]`.

---

### Task 2: API Pattern Scanner

**Files:**
- Create: `internal/wiki/scanner_api.go`
- Create: `internal/wiki/scanner_api_test.go`

Implement `ScanAPIPatterns(files []ScannedFile, readFile func(string) ([]byte, error)) []APIPattern`.

Uses a table of regex patterns per language to detect HTTP routes, CLI commands, gRPC services, and exports. Also detects `.proto` and `.graphql` files by extension.

Each pattern entry: `{Language, Kind, Regex, MethodGroup, PathGroup, HandlerGroup}`.

The scanner reads file content via the `readFile` function, scans line-by-line, and emits `APIPattern` matches. Non-matching files are skipped silently.

Key patterns (subset):
- Go HTTP: `http\.HandleFunc|\.Get\(|\.Post\(|\.HandleFunc\(`
- Go CLI: `cobra\.Command\{`
- Python HTTP: `@app\.route|@router\.(get|post)`
- JS/TS HTTP: `app\.(get|post|put|delete)\(`
- Java HTTP: `@(Get|Post|Put|Delete|Request)Mapping`

Write thorough tests with sample source snippets for each language. Commit with `[BEHAVIORAL]`.

---

### Task 3: APIAnalyzer

**Files:**
- Create: `internal/wiki/analyzer_api.go`
- Create: `internal/wiki/analyzer_api_test.go`

Implement `APIAnalyzer` with `Name() = "api"`.

In `Analyze()`:
1. Group `input.APIPatterns` by Kind
2. For each non-empty group, generate a prompt asking the LLM to produce structured documentation
3. Produce documents:
   - `api/_index.md` — overview of detected API surfaces
   - `api/http-endpoints.md` — if HTTP patterns found
   - `api/grpc-services.md` — if proto files found
   - `api/cli-commands.md` — if CLI patterns found
   - `api/public-interfaces.md` — if export patterns found
4. Generate one Mermaid sequence diagram for key API flows

Use `text/template` for LLM prompts. Each prompt receives the raw pattern matches and asks for structured markdown output.

Follow `SuggestionAnalyzer` patterns: non-fatal on LLM errors, context cancellation propagated.

Write tests with mock LLM. Commit with `[BEHAVIORAL]`.

---

### Task 4: SecurityAnalyzer

**Files:**
- Create: `internal/wiki/analyzer_security.go`
- Create: `internal/wiki/analyzer_security_test.go`

Implement `SecurityAnalyzer` with `Name() = "security"`.

Three sub-prompts, each producing a document:

1. **Auth & Trust Boundaries** → `security/auth-and-access.md`
   - Prompt: given module summaries and architecture, identify auth mechanisms, authorization patterns, trust boundaries
   - Produces Mermaid sequence diagram for auth flow

2. **Threat Model (STRIDE)** → `security/threat-model.md`
   - Prompt: STRIDE analysis of the architecture
   - Produces Mermaid attack tree diagram

3. **Data Flow & Compliance** → `security/data-flow.md`
   - Prompt: identify sensitive data, encryption, logging hygiene
   - Produces Mermaid data flow diagram

Each sub-prompt runs sequentially (needs full picture). Non-fatal: if one fails, others still produce output.

Write tests with mock LLM returning structured responses. Commit with `[BEHAVIORAL]`.

---

### Task 5: DependencyAnalyzer

**Files:**
- Create: `internal/wiki/analyzer_dependency.go`
- Create: `internal/wiki/analyzer_dependency_test.go`

Implement `DependencyAnalyzer` with `Name() = "dependencies"`.

In `Analyze()`:
1. Scan for dependency files: `go.mod`, `package.json`, `Cargo.toml`, `requirements.txt`, `pyproject.toml`
2. Extract external dependency list with versions (regex parsing, not full semantic understanding)
3. LLM prompt: given modules, dependencies, and architecture, produce:
   - `architecture/design-decisions.md` — inferred ADR-style decisions
   - Enhanced dependency analysis with risk notes

Non-fatal on LLM errors. Write tests. Commit with `[BEHAVIORAL]`.

---

### Task 6: Wire New Analyzers Into Pipeline

**Files:**
- Modify: `internal/wiki/pipeline.go`

In `Run()`, after the scanner stage:
1. Call `ScanAPIPatterns(files, readFile)` to get API patterns
2. Add `APIPatterns` to the `AnalyzerInput`
3. Register new analyzers in the specialized list:

```go
specializedAnalyzers := []SpecializedAnalyzer{
    NewSuggestionAnalyzer(llm),
    NewAPIAnalyzer(llm),
    NewSecurityAnalyzer(llm),
    NewDependencyAnalyzer(llm, cfg.Dir),
}
```

Add progress reporting for API scanning stage. Write pipeline integration test. Commit with `[BEHAVIORAL]`.

---

### Task 7: Final Verification

Run full test suite, build, verify new doc types are produced. Commit any fixes with `[STRUCTURAL]`.
