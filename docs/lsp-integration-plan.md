# LSP Integration Plan for Rubichan

> **Status:** Proposal · **FR:** FR-1.6 (P2) · **Date:** 2026-03-11

---

## 1. Why LSP?

Rubichan currently relies on **tree-sitter** for AST-level code understanding and **ripgrep** for text search. These are powerful but statically scoped — they parse what's on disk without understanding the project's build system, type resolution, or cross-module semantics.

The [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) (LSP) fills the gap by providing **live, compiler-grade intelligence** from the same language servers that power IDEs. This gives Rubichan access to information that is extremely expensive (or impossible) to reconstruct from raw source files alone.

---

## 2. Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Language scope | **All languages from day one** | Auto-detect from file extensions; any installed language server works. No hardcoded language list — a registry of known server configs with user override via TOML. |
| Server activation | **Only if server binary is present** | `exec.LookPath()` check before spawn. Missing server = graceful fallback to tree-sitter + grep with a one-time info log. Zero mandatory dependencies. |
| Diagnostic delivery | **Push/pull hybrid** | Errors auto-pushed into conversation after edits (self-healing). Warnings available on-demand via `lsp_diagnostics` tool (LLM pulls when relevant). |
| Token budget | **Summarize large responses** | Reference lists, completion lists, and symbol searches are summarized when exceeding a configurable threshold (default: 50 items). Full results available via pagination parameter. |
| Wiki/Security integration | **Yes, deep integration** | LSP feeds call hierarchy into wiki Mermaid diagrams and type resolution into security taint tracking. Both subsystems query the Manager directly. |

---

## 3. Benefits by Subsystem

### 3.1 Agent Core — Smarter Tool Use

| Capability | Without LSP | With LSP |
|---|---|---|
| **Go-to-definition** | Tree-sitter heuristic (same file / grep for symbol) | Exact cross-package resolution, including vendored deps |
| **Find references** | Grep for symbol name (noisy, false positives) | Semantic references only — no coincidental string matches |
| **Type information** | Not available (tree-sitter is syntax-only) | Full type signatures, generic instantiations, interface satisfiers |
| **Diagnostics** | Must run `go build` / compiler and parse output | Real-time errors and warnings streamed as the agent edits |
| **Completions** | LLM generates from context (may hallucinate) | Ground-truth completions from the compiler — validates LLM suggestions |
| **Rename** | Regex find-replace (unsafe across packages) | Compiler-safe rename across entire project |
| **Code actions** | Not available | Auto-imports, extract function, organize imports, quick fixes |

**Key insight:** LSP turns Rubichan from "an LLM that can read files" into "an LLM with a compiler looking over its shoulder." The LLM proposes changes; LSP validates them instantly.

### 3.2 Interactive Mode — Tighter Feedback Loop

- **Self-healing edits:** After the agent writes a file, LSP errors are auto-injected into the conversation. The agent self-corrects in the same turn without user intervention.
- **On-demand warnings:** The LLM calls `lsp_diagnostics` when it wants the full picture (warnings, hints, info) — not just errors.
- **Hover documentation:** When the agent (or user) references an unfamiliar symbol, LSP provides signature + doc comment — reducing unnecessary LLM context.
- **Workspace symbols:** Fast fuzzy search for any symbol in the project by name, useful for "find where X is defined" queries that currently require grep chains.

### 3.3 Headless Mode — Better Code Review

- **Diagnostic-aware review:** Before reviewing a PR, run LSP diagnostics on the diff to catch compile errors, unused imports, unreachable code — things that don't require LLM intelligence.
- **Impact analysis via references:** When a function signature changes, LSP can enumerate every call site to assess blast radius. The LLM gets a precise caller list instead of guessing.
- **Type-aware refactoring validation:** Verify that a refactoring preserves type safety by checking zero LSP errors post-change.

### 3.4 Wiki Generator — Richer Documentation

- **Accurate dependency graphs:** Use LSP's `callHierarchy/incomingCalls` and `callHierarchy/outgoingCalls` to produce Mermaid diagrams that reflect actual call relationships, not just import statements.
- **Interface implementation maps:** "Which types implement `io.Reader` in this project?" — answered precisely via `textDocument/implementation`. Rendered as Mermaid class diagrams in wiki output.
- **Type hierarchy visualizations:** `typeHierarchy/supertypes` and `typeHierarchy/subtypes` produce inheritance trees for wiki module pages.
- **Symbol cross-referencing:** Wiki pages link to precise definitions. Module pages list exported symbols with type signatures from `lsp_hover`.
- **Module coupling metrics:** Count cross-package references via `textDocument/references` to quantify coupling. Surface high-coupling pairs in the wiki architecture overview.

### 3.5 Security Engine — Deeper Analysis

- **Taint tracking through types:** LSP's type resolution traces data flow across function boundaries. Example flow: `http.Request.URL.Query()` → `string` param → `db.Query()` — LSP resolves each hop's type, the SAST engine flags the untrusted-to-sink path.
- **Call hierarchy for attack surfaces:** `callHierarchy/incomingCalls` on a sensitive function (e.g., `sql.DB.Exec`) enumerates every caller chain. Prioritize SAST analysis on these paths.
- **Dead code detection:** Zero references on a function = unreachable code. Remove from SAST scan scope to reduce false positives and LLM budget waste.
- **Interface implementation discovery:** When a taint sink uses an interface type, LSP reveals all concrete implementations — uncovering hidden data flows that grep-based analysis misses.
- **Dependency resolution:** Resolve which version of a transitive dependency is actually in the build graph (not just declared in go.sum/package-lock.json).

---

## 4. Architecture

### 4.1 High-Level Design

```
┌──────────────────────────────────────────────────────────────┐
│                     Agent Core                                │
│  ┌────────────┐                                              │
│  │ Tool Router │───▶ LSP Tools (lsp_diagnostics,             │
│  └────────────┘      lsp_definition, lsp_references,         │
│                      lsp_hover, lsp_rename,                  │
│        ┌─────────────lsp_completions, lsp_code_action,       │
│        │             lsp_symbols, lsp_call_hierarchy)        │
│        │                                                      │
│  on_after_tool_call                                          │
│  (file_write/patch)                                          │
│        │  errors auto-pushed ┌──────────────┐                │
│        └────────────────────▶│ Conversation │                │
│                              └──────────────┘                │
└────────────┬─────────────────────────────────────────────────┘
             │
             ▼
┌──────────────────────────────────────────────────────────────┐
│              internal/tools/lsp/                              │
│                                                               │
│  ┌─────────────┐  ┌────────────┐  ┌──────────────────────┐  │
│  │  LSP Client  │  │  Server    │  │  Capability          │  │
│  │  (JSON-RPC)  │  │  Manager   │  │  Negotiator          │  │
│  └──────┬──────┘  └─────┬──────┘  └──────────────────────┘  │
│         │               │                                     │
│         │  ┌────────────┴──────────────────────────┐         │
│         │  │  Server Registry                      │         │
│         │  │  (auto-detect + user config + lookup)  │         │
│         │  └───────────────────────────────────────┘         │
│         │                                                     │
│         │  ┌────────────────────────────────────────┐        │
│         │  │  Response Summarizer                    │        │
│         │  │  (truncate large reference/symbol lists)│        │
│         │  └────────────────────────────────────────┘        │
│         │                                                     │
│  ┌──────┴─────────────────────────────────────────────────┐  │
│  │  Subsystem Adapters                                     │  │
│  │  ┌──────────────┐  ┌──────────────┐                    │  │
│  │  │ Wiki Adapter  │  │Security Adpt │                    │  │
│  │  │ (call hier →  │  │ (type res →  │                    │  │
│  │  │  mermaid)     │  │  taint track)│                    │  │
│  │  └──────────────┘  └──────────────┘                    │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────┼────────────────────────────────────────────────────┘
          │ stdin/stdout (JSON-RPC 2.0)
          ▼
┌──────────────────────────────────────────────────────────────┐
│  Language Servers (external processes — only if installed)     │
│  ┌──────┐ ┌─────────┐ ┌────────┐ ┌─────────────┐ ┌──────┐  │
│  │gopls │ │ts-server│ │pyright │ │rust-analyzer│ │ ...  │  │
│  └──────┘ └─────────┘ └────────┘ └─────────────┘ └──────┘  │
└──────────────────────────────────────────────────────────────┘
```

### 4.2 Package Structure

```
internal/tools/lsp/
├── client.go              # JSON-RPC 2.0 client over stdin/stdout
├── client_test.go
├── server.go              # Server lifecycle (spawn, init, shutdown, restart)
├── server_test.go
├── manager.go             # Multi-server manager (one per language, lazy start)
├── manager_test.go
├── registry.go            # Known server configs + auto-detection + user override
├── registry_test.go
├── capability.go          # Capability negotiation and feature detection
├── capability_test.go
├── protocol.go            # LSP protocol types (subset we need)
├── protocol_test.go
├── summarizer.go          # Truncate/summarize large LSP responses for token budget
├── summarizer_test.go
├── tool_diagnostics.go    # Tool: get diagnostics for a file/workspace
├── tool_definition.go     # Tool: go-to-definition
├── tool_references.go     # Tool: find references
├── tool_hover.go          # Tool: hover info (type + docs)
├── tool_rename.go         # Tool: rename symbol
├── tool_completions.go    # Tool: completions at position
├── tool_code_action.go    # Tool: request code actions
├── tool_symbols.go        # Tool: workspace symbol search
├── tool_call_hierarchy.go # Tool: incoming/outgoing call hierarchy
├── tool_*_test.go         # Tests for each tool
├── wiki_adapter.go        # Wiki pipeline integration (call hierarchy → Mermaid)
├── wiki_adapter_test.go
├── security_adapter.go    # Security engine integration (type resolution → taint)
└── security_adapter_test.go
```

### 4.3 Core Interfaces

```go
// ServerConfig describes how to launch a language server.
type ServerConfig struct {
    Language    string   // e.g. "go", "typescript", "python"
    Command     string   // e.g. "gopls", "typescript-language-server"
    Args        []string // e.g. ["serve"]
    Env         []string // additional env vars
    Extensions  []string // file extensions that map to this server
    RootURI     string   // workspace root (auto-detected)
}

// Server wraps a running language server process.
type Server interface {
    Initialize(ctx context.Context, rootURI string) (*InitializeResult, error)
    Request(ctx context.Context, method string, params any) (json.RawMessage, error)
    Notify(ctx context.Context, method string, params any) error
    Diagnostics() <-chan PublishDiagnosticsParams // server-pushed diagnostics
    Capabilities() ServerCapabilities             // negotiated capabilities
    Shutdown(ctx context.Context) error
}

// Manager tracks language servers across the workspace.
type Manager interface {
    // ServerFor returns the server for the given language, starting it if
    // needed. Returns ErrServerNotInstalled if the binary is not on PATH.
    ServerFor(ctx context.Context, languageID string) (Server, error)

    // ServerForFile detects language from file extension and delegates to ServerFor.
    ServerForFile(ctx context.Context, filePath string) (Server, error)

    // DiagnosticsFor returns cached diagnostics for a file URI.
    // Errors are always returned; warnings only if includeWarnings is true.
    DiagnosticsFor(uri string, includeWarnings bool) []Diagnostic

    // NotifyFileChanged sends didOpen/didChange to the appropriate server.
    // Called by the file_write/file_patch tool hooks.
    NotifyFileChanged(ctx context.Context, filePath string, content []byte) error

    // Shutdown gracefully stops all running servers.
    Shutdown(ctx context.Context) error
}

// Registry maps file extensions to server configs.
// Combines built-in defaults with user TOML overrides.
type Registry interface {
    // ConfigFor returns the server config for a language ID.
    // Returns ErrNoConfig if no server is configured for this language.
    ConfigFor(languageID string) (ServerConfig, error)

    // LanguageForExt maps a file extension to a language ID.
    LanguageForExt(ext string) (string, bool)

    // Available returns language IDs whose server binary is on PATH.
    Available() []string
}

// Summarizer controls token budget for LSP responses.
type Summarizer interface {
    // SummarizeReferences truncates a reference list if it exceeds maxItems.
    // Returns a summary header + top N items sorted by relevance.
    SummarizeReferences(refs []Location, maxItems int) SummarizedResult

    // SummarizeCompletions truncates completion items.
    SummarizeCompletions(items []CompletionItem, maxItems int) SummarizedResult

    // SummarizeSymbols truncates workspace symbol results.
    SummarizeSymbols(symbols []SymbolInformation, maxItems int) SummarizedResult
}

type SummarizedResult struct {
    Items      string // formatted text for LLM consumption
    Total      int    // total items before truncation
    Shown      int    // items included in output
    Truncated  bool   // whether truncation occurred
}

// ErrServerNotInstalled is returned when a language server binary is not found.
var ErrServerNotInstalled = errors.New("language server not installed")
```

### 4.4 Server Registry — All Languages Support

The registry ships with built-in configs for common language servers, auto-detected via `exec.LookPath()`:

```go
var defaultServers = []ServerConfig{
    {Language: "go",         Command: "gopls",                       Args: []string{"serve"},   Extensions: []string{".go"}},
    {Language: "typescript", Command: "typescript-language-server",   Args: []string{"--stdio"}, Extensions: []string{".ts", ".tsx", ".js", ".jsx"}},
    {Language: "python",     Command: "pyright-langserver",          Args: []string{"--stdio"}, Extensions: []string{".py"}},
    {Language: "rust",       Command: "rust-analyzer",               Args: nil,                 Extensions: []string{".rs"}},
    {Language: "java",       Command: "jdtls",                       Args: nil,                 Extensions: []string{".java"}},
    {Language: "c",          Command: "clangd",                      Args: nil,                 Extensions: []string{".c", ".h"}},
    {Language: "cpp",        Command: "clangd",                      Args: nil,                 Extensions: []string{".cpp", ".hpp", ".cc", ".cxx"}},
    {Language: "ruby",       Command: "solargraph",                  Args: []string{"stdio"},   Extensions: []string{".rb"}},
    {Language: "php",        Command: "phpactor",                    Args: []string{"language-server"}, Extensions: []string{".php"}},
    {Language: "swift",      Command: "sourcekit-lsp",               Args: nil,                 Extensions: []string{".swift"}},
    {Language: "kotlin",     Command: "kotlin-language-server",      Args: nil,                 Extensions: []string{".kt", ".kts"}},
    {Language: "zig",        Command: "zls",                         Args: nil,                 Extensions: []string{".zig"}},
    {Language: "lua",        Command: "lua-language-server",         Args: nil,                 Extensions: []string{".lua"}},
    {Language: "elixir",     Command: "elixir-ls",                   Args: nil,                 Extensions: []string{".ex", ".exs"}},
    {Language: "haskell",    Command: "haskell-language-server-wrapper", Args: []string{"--lsp"}, Extensions: []string{".hs"}},
    {Language: "csharp",     Command: "OmniSharp",                   Args: []string{"--languageserver"}, Extensions: []string{".cs"}},
    {Language: "dart",       Command: "dart",                        Args: []string{"language-server"}, Extensions: []string{".dart"}},
    {Language: "ocaml",      Command: "ocamllsp",                    Args: nil,                 Extensions: []string{".ml", ".mli"}},
}
```

Users can override or add servers via TOML config. The registry merges defaults with user config, and `Available()` filters to only those where the binary is actually installed.

### 4.5 Tool Definitions (exposed to LLM)

Each LSP capability becomes a separate tool registered in the Tool Layer:

| Tool Name | Input | Output | LSP Method |
|---|---|---|---|
| `lsp_diagnostics` | `{file: string}` | Errors + warnings with locations | `textDocument/publishDiagnostics` (cached) |
| `lsp_definition` | `{file, line, column}` | Definition location(s) | `textDocument/definition` |
| `lsp_references` | `{file, line, column, max_results?}` | Reference locations (summarized) | `textDocument/references` |
| `lsp_hover` | `{file, line, column}` | Type signature + documentation | `textDocument/hover` |
| `lsp_rename` | `{file, line, column, newName}` | Workspace edit (set of file changes) | `textDocument/rename` |
| `lsp_completions` | `{file, line, column, max_results?}` | Completion items (summarized) | `textDocument/completion` |
| `lsp_code_action` | `{file, line, column}` | Available quick fixes / refactors | `textDocument/codeAction` |
| `lsp_symbols` | `{query: string, max_results?}` | Matching symbols (summarized) | `workspace/symbol` |
| `lsp_call_hierarchy` | `{file, line, column, direction}` | Incoming or outgoing call tree | `callHierarchy/*` |

Tools that return large lists accept an optional `max_results` parameter (default: 50). When truncated, the response includes a summary header: `"Showing 50 of 342 references. Use max_results to see more."`.

### 4.6 Push/Pull Hybrid Diagnostic Model

```
                    ┌─────────────────────────────────────────────┐
                    │              Diagnostic Flow                 │
                    └─────────────────────────────────────────────┘

  PUSH (automatic):
  ┌────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
  │ file_write │────▶│ didChange    │────▶│ LSP server   │────▶│ Manager      │
  │ file_patch │     │ notification │     │ processes    │     │ caches diags │
  └────────────┘     └──────────────┘     └──────────────┘     └──────┬───────┘
                                                                       │
                                                            errors only│
                                                                       ▼
                                                               ┌──────────────┐
                                                               │ Auto-inject  │
                                                               │ into convo:  │
                                                               │ "LSP found   │
                                                               │  2 errors in │
                                                               │  foo.go"     │
                                                               └──────────────┘

  PULL (on-demand):
  ┌────────────┐     ┌──────────────┐     ┌──────────────────────────────────┐
  │ LLM calls  │────▶│ lsp_diag-    │────▶│ Returns full diagnostics:       │
  │ tool       │     │ nostics tool │     │ errors + warnings + hints + info │
  └────────────┘     └──────────────┘     └──────────────────────────────────┘
```

**Push rules:**
- Only **errors** (severity 1) are auto-injected into the conversation after file edits.
- Debounce: wait 500ms after the last `didChange` before reading diagnostics (servers need processing time).
- Max 10 errors per push to avoid flooding the conversation. If more, append: `"... and 15 more errors. Use lsp_diagnostics for full list."`.
- Push is suppressed if the agent's next action is another file write to the same file (batched edits).

**Pull rules:**
- `lsp_diagnostics` tool returns all severity levels (errors, warnings, information, hints).
- Grouped by severity, sorted by line number.
- Summarized if exceeding threshold.

### 4.7 Response Summarization Strategy

When LSP responses exceed the configured threshold, the summarizer applies these strategies:

| Response Type | Threshold | Summarization |
|---|---|---|
| **References** | >50 items | Group by file, show count per file, list first 3 references per file. Header: `"342 references across 28 files"` |
| **Completions** | >50 items | Sort by relevance score, show top 50. Filter by prefix match if available. Header: `"50 of 203 completions shown (filtered by prefix)"` |
| **Symbols** | >50 items | Group by kind (function, type, variable), show top items per kind. Header: `"189 symbols matching 'Handle'; showing top 50"` |
| **Call hierarchy** | >3 levels deep | Truncate at depth 3, show `"... 12 more callers (use depth parameter)"` |
| **Diagnostics** | >20 items | Group by severity, show all errors, truncate warnings. Header: `"3 errors, 47 warnings (showing first 20)"` |

The `max_results` parameter on tools overrides the default threshold for a single call.

### 4.8 Server Lifecycle

1. **Presence check:** On first LSP tool call for a file, `Registry.LanguageForExt()` maps extension to language ID. `exec.LookPath(config.Command)` checks if the server binary exists. If not → return `ErrServerNotInstalled` → tool returns `"LSP not available for {language}: {command} not found on PATH"`.
2. **Lazy initialization:** Server process spawned only on first use. `Manager.ServerFor()` handles spawn + `initialize` handshake. Capability negotiation determines which tools are actually available for this server.
3. **`didOpen` / `didChange` tracking:** `Manager.NotifyFileChanged()` is called by the agent's file tool hooks. Sends `didOpen` on first access, `didChange` on subsequent modifications.
4. **Diagnostic streaming:** Servers push diagnostics asynchronously via `textDocument/publishDiagnostics`. The Manager caches the latest diagnostics per file URI.
5. **Graceful shutdown:** On agent exit, `Manager.Shutdown()` sends `shutdown` + `exit` to all running servers.
6. **Crash recovery:** If a server process dies, the Manager restarts it on the next request (exponential backoff: 1s, 2s, 4s). After 3 consecutive failures, disable that server for the session with a warning.
7. **Idle timeout:** Servers with no requests for 10 minutes are shut down to free memory. Re-spawned on next use.

### 4.9 JSON-RPC 2.0 Transport

The LSP client uses a **custom JSON-RPC 2.0 implementation** over stdin/stdout, consistent with the project's ADR-006 philosophy (no vendor SDKs). This is ~200-300 LOC:

- Content-Length framing (LSP's `Content-Length: N\r\n\r\n` header format)
- Request/response correlation via atomic integer IDs
- Server-initiated notifications (e.g., `textDocument/publishDiagnostics`) dispatched to registered handlers
- Concurrent request support with `sync.Map` for pending response channels
- Per-request timeout (default 30s, configurable)

---

## 5. Subsystem Integration Details

### 5.1 Wiki Generator Integration

The wiki pipeline gains a new optional pass when LSP servers are available:

```
Wiki Pipeline (existing):
  1. File discovery → 2. Tree-sitter AST → 3. LLM per-module → 4. Cross-cutting → 5. Render

With LSP (new step 2.5):
  2.5. LSP enrichment:
       a. callHierarchy/outgoingCalls on exported functions → build call graph
       b. textDocument/implementation on interfaces → build impl map
       c. typeHierarchy/subtypes on base types → build type tree
       d. Render as Mermaid diagrams injected into module pages
```

**Wiki adapter API:**

```go
// WikiAdapter provides LSP-enriched data for the wiki pipeline.
type WikiAdapter struct {
    manager Manager
}

// CallGraph returns a Mermaid flowchart of call relationships for a package.
func (w *WikiAdapter) CallGraph(ctx context.Context, packageDir string) (string, error)

// InterfaceMap returns a Mermaid class diagram of interface implementations.
func (w *WikiAdapter) InterfaceMap(ctx context.Context, packageDir string) (string, error)

// TypeHierarchy returns a Mermaid class diagram of type inheritance.
func (w *WikiAdapter) TypeHierarchy(ctx context.Context, typeName string, file string, line int) (string, error)

// ModuleCoupling returns cross-reference counts between packages.
func (w *WikiAdapter) ModuleCoupling(ctx context.Context, packages []string) ([]CouplingEdge, error)
```

### 5.2 Security Engine Integration

The security engine gains LSP-enhanced analysis when servers are available:

```
Security Pipeline (existing):
  1. Static scanners (regex, entropy, AST) → 2. Prioritize by risk score → 3. LLM analyzers

With LSP (enhanced steps 1 and 2):
  1b. LSP-enhanced scanning:
      - callHierarchy on known sinks (sql.Exec, os.Exec, http.Redirect)
        → identify all callers → prioritize for taint analysis
      - textDocument/references on sensitive functions
        → zero refs = dead code → skip from scan
      - textDocument/implementation on interface params
        → discover concrete types for taint propagation

  2b. LSP-enhanced risk scoring:
      - Functions reachable from HTTP handlers get +10 risk
      - Functions with zero references get -20 risk (dead code)
      - Functions handling interface types get +5 (hidden data flows)
```

**Security adapter API:**

```go
// SecurityAdapter provides LSP-enriched data for the security engine.
type SecurityAdapter struct {
    manager Manager
}

// CallersOf returns all call sites of a function (for taint source/sink analysis).
func (s *SecurityAdapter) CallersOf(ctx context.Context, file string, line, col int) ([]CallSite, error)

// ImplementorsOf returns concrete types implementing an interface (for hidden data flows).
func (s *SecurityAdapter) ImplementorsOf(ctx context.Context, file string, line, col int) ([]Location, error)

// IsDeadCode returns true if a function has zero references (excluding its declaration).
func (s *SecurityAdapter) IsDeadCode(ctx context.Context, file string, line, col int) (bool, error)

// ResolveType returns the fully qualified type name at a position (for taint tracking).
func (s *SecurityAdapter) ResolveType(ctx context.Context, file string, line, col int) (string, error)
```

---

## 6. Configuration

```toml
# ~/.config/aiagent/config.toml

[lsp]
enabled = true                # global toggle
auto_diagnostics = true       # push errors after edits
auto_diagnostics_delay = 500  # ms to wait after didChange before reading diagnostics
idle_timeout = 600            # seconds before shutting down idle servers
max_restart_attempts = 3      # crash recovery limit per session

[lsp.summarizer]
max_references = 50           # truncate reference lists beyond this
max_completions = 50          # truncate completion lists beyond this
max_symbols = 50              # truncate symbol search results beyond this
max_call_depth = 3            # truncate call hierarchy beyond this depth
max_diagnostics_push = 10     # max errors to auto-inject per edit

# Override or add server configs (merged with built-in defaults)
[lsp.servers.go]
command = "gopls"
args = ["serve"]

[lsp.servers.typescript]
command = "typescript-language-server"
args = ["--stdio"]

[lsp.servers.python]
command = "pyright-langserver"
args = ["--stdio"]

[lsp.servers.rust]
command = "rust-analyzer"
args = []

# Example: add a custom server not in defaults
[lsp.servers.gleam]
command = "gleam"
args = ["lsp"]
extensions = [".gleam"]
```

Language servers are **optional runtime dependencies** (consistent with NFR-9: zero mandatory external dependencies). When a server is not installed, LSP tools for that language gracefully return `"LSP not available for {language}: {command} not found on PATH"` and the agent falls back to tree-sitter + grep.

---

## 7. Phased Implementation Roadmap

### Phase 1: Foundation (3-4 days)
- [ ] JSON-RPC 2.0 client with Content-Length framing
- [ ] Server process lifecycle (spawn, initialize, shutdown, crash recovery)
- [ ] Server registry with built-in defaults + TOML override
- [ ] `exec.LookPath()` auto-detection of available servers
- [ ] Manager with lazy server initialization
- [ ] Capability negotiation
- [ ] Unit tests with mock server

### Phase 2: Core Tools (3-4 days)
- [ ] `lsp_diagnostics` tool (with diagnostic caching)
- [ ] `lsp_definition` tool
- [ ] `lsp_references` tool (with summarization)
- [ ] `lsp_hover` tool
- [ ] `lsp_symbols` tool (with summarization)
- [ ] Response summarizer for large result sets
- [ ] Integration tests with `gopls`

### Phase 3: Edit Integration + Hybrid Diagnostics (2-3 days)
- [ ] `didOpen` / `didChange` notifications via `Manager.NotifyFileChanged()`
- [ ] Post-edit auto-diagnostics hook (push errors only, debounced)
- [ ] Batch edit suppression (don't push between rapid consecutive writes)
- [ ] `lsp_rename` tool with workspace edit application
- [ ] `lsp_code_action` tool
- [ ] `lsp_completions` tool (with summarization)

### Phase 4: Advanced Features (2-3 days)
- [ ] `lsp_call_hierarchy` tool (incoming + outgoing)
- [ ] Multi-server support (concurrent language servers for polyglot projects)
- [ ] Idle timeout and server lifecycle management
- [ ] Exponential backoff crash recovery

### Phase 5: Wiki Integration (2-3 days)
- [ ] Wiki adapter: call graph → Mermaid flowchart
- [ ] Wiki adapter: interface implementations → Mermaid class diagram
- [ ] Wiki adapter: type hierarchy → Mermaid class diagram
- [ ] Wiki adapter: module coupling metrics
- [ ] Integration into wiki pipeline as optional enrichment pass

### Phase 6: Security Integration (2-3 days)
- [ ] Security adapter: callers of known sinks
- [ ] Security adapter: dead code detection
- [ ] Security adapter: interface implementation discovery for taint propagation
- [ ] Security adapter: type resolution for taint tracking
- [ ] Integration into security pipeline risk scoring

**Total estimate: ~15-20 days**

---

## 8. Risks and Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Language server not installed | LSP tools unavailable for that language | High | `exec.LookPath()` check; graceful fallback to tree-sitter + grep; one-time info log |
| Server startup latency (gopls 2-5s on large projects) | Slow first LSP tool call | Medium | Lazy init; async warm-up option; cache initialization result |
| Server crashes or hangs | Tool calls fail | Medium | 30s per-request timeout; exponential backoff restart (1s/2s/4s); circuit breaker after 3 failures |
| Memory usage from multiple servers | High memory on polyglot projects | Medium | Idle timeout (10min default) shuts down unused servers; only start on first use |
| Large LSP responses blow token budget | Context window exhaustion | High | Summarizer truncates at configurable thresholds; `max_results` parameter on tools |
| LSP protocol version mismatches | Capability negotiation failures | Low | Support LSP 3.17 baseline; check capabilities before calling methods; skip unavailable features |
| Stale diagnostics after rapid edits | False error reports in push mode | Medium | 500ms debounce after last didChange; batch edit suppression; wait for server response |
| Wiki/security integration adds pipeline latency | Slower wiki/security runs | Medium | LSP enrichment is optional; skip if no server available; timeout per-query |

---

## 9. What LSP Does NOT Replace

- **Tree-sitter:** Still needed for SAST pattern matching (S-expression queries), wiki AST extraction, and environments where no language server is available. Tree-sitter is embedded and always works; LSP is optional.
- **Ripgrep:** Still needed for raw text search, regex patterns, and searching non-code files. LSP only understands code symbols.
- **LLM analysis:** LSP provides facts (types, references, errors); the LLM provides reasoning (is this a bug? what's the best fix? is this a security issue?).

LSP is a **complement**, not a replacement, to the existing analysis stack. Every LSP feature degrades gracefully when the server is absent — the agent continues to work, just with less precision.
