# LSP Integration Plan for Rubichan

> **Status:** Proposal · **FR:** FR-1.6 (P2) · **Date:** 2026-03-11

---

## 1. Why LSP?

Rubichan currently relies on **tree-sitter** for AST-level code understanding and **ripgrep** for text search. These are powerful but statically scoped — they parse what's on disk without understanding the project's build system, type resolution, or cross-module semantics.

The [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) (LSP) fills the gap by providing **live, compiler-grade intelligence** from the same language servers that power IDEs. This gives Rubichan access to information that is extremely expensive (or impossible) to reconstruct from raw source files alone.

---

## 2. Benefits by Subsystem

### 2.1 Agent Core — Smarter Tool Use

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

### 2.2 Interactive Mode — Tighter Feedback Loop

- **Inline diagnostics after edits:** After the agent writes a file, LSP can immediately report compiler errors without a full build cycle. The agent can self-correct in the same turn.
- **Hover documentation:** When the agent (or user) references an unfamiliar symbol, LSP provides signature + doc comment — reducing unnecessary LLM context.
- **Workspace symbols:** Fast fuzzy search for any symbol in the project by name, useful for "find where X is defined" queries that currently require grep chains.

### 2.3 Headless Mode — Better Code Review

- **Diagnostic-aware review:** Before reviewing a PR, run LSP diagnostics on the diff to catch compile errors, unused imports, unreachable code — things that don't require LLM intelligence.
- **Impact analysis via references:** When a function signature changes, LSP can enumerate every call site to assess blast radius. The LLM gets a precise caller list instead of guessing.
- **Type-aware refactoring validation:** Verify that a refactoring preserves type safety by checking zero LSP errors post-change.

### 2.4 Wiki Generator — Richer Documentation

- **Accurate dependency graphs:** Use LSP's call hierarchy and type hierarchy to produce Mermaid diagrams that reflect actual usage, not just import statements.
- **Interface implementation maps:** "Which types implement `io.Reader` in this project?" — answered precisely via `textDocument/implementation`.
- **Symbol cross-referencing:** Wiki pages can link to precise definitions rather than file-level references.

### 2.5 Security Engine — Deeper Analysis

- **Taint tracking through types:** LSP's type resolution helps trace data flow across function boundaries (e.g., "this `string` came from `http.Request.URL.Query()`").
- **Dead code detection:** LSP references can identify truly unreachable code — reducing false positives in SAST scans.
- **Dependency resolution:** Resolve which version of a transitive dependency is actually used (not just declared).

---

## 3. Architecture

### 3.1 High-Level Design

```
┌──────────────────────────────────────────────────────────┐
│                     Agent Core                            │
│  ┌────────────┐                                          │
│  │ Tool Router │───▶ LSP Tool (lsp_diagnostics,          │
│  └────────────┘      lsp_definition, lsp_references,     │
│                      lsp_hover, lsp_rename,              │
│                      lsp_completions, lsp_code_action)   │
└────────────┬─────────────────────────────────────────────┘
             │
             ▼
┌──────────────────────────────────────────────────────────┐
│              internal/tools/lsp/                          │
│  ┌─────────────┐  ┌────────────┐  ┌──────────────────┐  │
│  │  LSP Client  │  │  Server    │  │  Capability      │  │
│  │  (JSON-RPC)  │  │  Manager   │  │  Negotiator      │  │
│  └──────┬──────┘  └─────┬──────┘  └──────────────────┘  │
│         │               │                                 │
│         │  ┌────────────┴────────────┐                   │
│         │  │  Server Lifecycle       │                   │
│         │  │  (spawn, init, restart) │                   │
│         │  └─────────────────────────┘                   │
└─────────┼────────────────────────────────────────────────┘
          │ stdin/stdout (JSON-RPC 2.0)
          ▼
┌──────────────────────────────────────────────────────────┐
│  Language Servers (external processes)                     │
│  ┌──────┐  ┌─────────┐  ┌──────────┐  ┌─────────────┐  │
│  │gopls │  │ts-server│  │pyright   │  │rust-analyzer│  │
│  └──────┘  └─────────┘  └──────────┘  └─────────────┘  │
└──────────────────────────────────────────────────────────┘
```

### 3.2 Package Structure

```
internal/tools/lsp/
├── client.go           # JSON-RPC 2.0 client over stdin/stdout
├── client_test.go
├── server.go           # Server lifecycle management (spawn, init, shutdown)
├── server_test.go
├── manager.go          # Multi-server manager (one per language)
├── manager_test.go
├── capability.go       # Capability negotiation and feature detection
├── capability_test.go
├── protocol.go         # LSP protocol types (subset we need)
├── protocol_test.go
├── tool_diagnostics.go # Tool: get diagnostics for a file/workspace
├── tool_definition.go  # Tool: go-to-definition
├── tool_references.go  # Tool: find references
├── tool_hover.go       # Tool: hover info (type + docs)
├── tool_rename.go      # Tool: rename symbol
├── tool_completions.go # Tool: completions at position
├── tool_code_action.go # Tool: request code actions
└── tool_*_test.go      # Tests for each tool
```

### 3.3 Core Interfaces

```go
// ServerConfig describes how to launch a language server.
type ServerConfig struct {
    Language string   // e.g. "go", "typescript", "python"
    Command  string   // e.g. "gopls", "typescript-language-server"
    Args     []string // e.g. ["serve"]
    Env      []string // additional env vars
    RootURI  string   // workspace root
}

// Server wraps a running language server process.
type Server interface {
    Initialize(ctx context.Context, rootURI string) (*InitializeResult, error)
    Request(ctx context.Context, method string, params any) (json.RawMessage, error)
    Notify(ctx context.Context, method string, params any) error
    Diagnostics() <-chan PublishDiagnosticsParams // server-pushed diagnostics
    Shutdown(ctx context.Context) error
}

// Manager tracks language servers across the workspace.
type Manager interface {
    ServerFor(ctx context.Context, languageID string) (Server, error) // lazy start
    DiagnosticsFor(uri string) []Diagnostic
    Shutdown(ctx context.Context) error
}
```

### 3.4 Tool Definitions (exposed to LLM)

Each LSP capability becomes a separate tool registered in the Tool Layer:

| Tool Name | Input | Output | LSP Method |
|---|---|---|---|
| `lsp_diagnostics` | `{file: string}` | List of errors/warnings with locations | `textDocument/publishDiagnostics` (cached) |
| `lsp_definition` | `{file, line, column}` | Definition location(s) | `textDocument/definition` |
| `lsp_references` | `{file, line, column}` | List of reference locations | `textDocument/references` |
| `lsp_hover` | `{file, line, column}` | Type signature + documentation | `textDocument/hover` |
| `lsp_rename` | `{file, line, column, newName}` | Workspace edit (set of file changes) | `textDocument/rename` |
| `lsp_completions` | `{file, line, column}` | Completion items | `textDocument/completion` |
| `lsp_code_action` | `{file, line, column}` | Available quick fixes / refactors | `textDocument/codeAction` |
| `lsp_symbols` | `{query: string}` | Matching symbols across workspace | `workspace/symbol` |

### 3.5 Server Lifecycle

1. **Lazy initialization:** Language servers are spawned only when the first LSP tool is called for that language. The `Manager` detects the language from the file extension and checks `ServerConfig`.
2. **`didOpen` / `didChange` tracking:** The client sends `textDocument/didOpen` when a file is first referenced and `textDocument/didChange` when the agent modifies a file. This keeps the server in sync.
3. **Diagnostic streaming:** Servers push diagnostics asynchronously. The `Manager` caches the latest diagnostics per file and exposes them via `DiagnosticsFor()`.
4. **Graceful shutdown:** On agent exit, `Manager.Shutdown()` sends `shutdown` + `exit` to all running servers.
5. **Crash recovery:** If a server process dies, the `Manager` restarts it on the next request (up to 3 retries, then disables with a warning).

### 3.6 JSON-RPC 2.0 Transport

The LSP client uses a **custom JSON-RPC 2.0 implementation** over stdin/stdout, consistent with the project's ADR-006 philosophy (no vendor SDKs). This is ~200-300 LOC:

- Content-Length framing (LSP's header format)
- Request/response correlation via integer IDs
- Server-initiated notifications (e.g., `textDocument/publishDiagnostics`)
- Concurrent request support with `sync.Map` for pending responses

---

## 4. Improvement Opportunities

### 4.1 Post-Edit Validation Loop

The highest-value integration: after every `file_write` or `file_patch` tool execution, automatically run `lsp_diagnostics` on the modified file. If errors are found, inject them into the conversation so the LLM can self-correct without the user needing to say "it doesn't compile."

```
Agent writes file → didChange → LSP diagnostics → errors? → auto-inject into conversation
```

This can be implemented as an `on_after_tool_call` hook in the agent loop, requiring zero LLM involvement for the check itself.

### 4.2 LLM Grounding for Completions

When the LLM suggests a function call or import, cross-check against `lsp_completions` to verify the symbol actually exists. This eliminates hallucinated API calls — a common failure mode of coding agents.

### 4.3 Intelligent Context Building

Instead of dumping entire files into the context window, use LSP to fetch only the relevant symbols:
- `lsp_definition` to get the implementation of a called function
- `lsp_hover` to get type signatures without reading the whole file
- `lsp_references` to understand usage patterns

This reduces token consumption while providing more precise information.

### 4.4 Security Engine Enhancement

Feed LSP type resolution into the SAST scanner to improve taint tracking:
- Resolve function return types to determine if data flows from untrusted sources
- Use call hierarchy to trace data propagation paths
- Identify interface implementations to discover hidden data flows

### 4.5 Wiki Generator Enhancement

Use LSP's workspace/symbol and call hierarchy to generate:
- Accurate architecture diagrams (Mermaid) based on actual call relationships
- Interface implementation maps
- Type hierarchy visualizations
- Module coupling metrics based on cross-reference counts

---

## 5. Configuration

```toml
# ~/.config/aiagent/config.toml

[lsp]
enabled = true          # global toggle
auto_diagnostics = true # auto-check after edits

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
```

Language servers are **optional runtime dependencies** (consistent with NFR-9: zero mandatory external dependencies). When a server is not installed, LSP tools for that language gracefully return "LSP not available for {language}" and the agent falls back to tree-sitter + grep.

---

## 6. Phased Implementation Roadmap

### Phase 1: Foundation (3-4 days)
- [ ] JSON-RPC 2.0 client with Content-Length framing
- [ ] Server process lifecycle (spawn, initialize, shutdown)
- [ ] Manager with lazy server initialization
- [ ] Capability negotiation
- [ ] Unit tests with mock server

### Phase 2: Core Tools (3-4 days)
- [ ] `lsp_diagnostics` tool (with diagnostic caching)
- [ ] `lsp_definition` tool
- [ ] `lsp_references` tool
- [ ] `lsp_hover` tool
- [ ] Integration tests with `gopls`

### Phase 3: Edit Integration (2-3 days)
- [ ] `didOpen` / `didChange` notifications on file tool use
- [ ] Post-edit auto-diagnostics hook
- [ ] `lsp_rename` tool with workspace edit application
- [ ] `lsp_code_action` tool

### Phase 4: Advanced Features (2-3 days)
- [ ] `lsp_completions` tool
- [ ] `lsp_symbols` (workspace symbol search)
- [ ] Multi-server support (concurrent language servers)
- [ ] Crash recovery and restart logic

### Phase 5: Subsystem Integration (2-3 days)
- [ ] Security engine: LSP-enhanced taint tracking
- [ ] Wiki generator: LSP-based dependency graphs
- [ ] Headless mode: LSP diagnostics in code review pipeline
- [ ] Configuration via TOML config

**Total estimate: ~12-17 days** (aligns with Milestone 2-3 timeframe in spec)

---

## 7. Risks and Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Language server not installed | LSP tools unavailable for that language | High | Graceful fallback to tree-sitter + grep; clear user messaging |
| Server startup latency (gopls can take seconds on large projects) | Slow first LSP tool call | Medium | Lazy init + async warm-up; cache initialization |
| Server crashes or hangs | Tool calls fail | Medium | Timeout per request (30s); auto-restart with backoff; circuit breaker after 3 failures |
| Memory usage from multiple servers | High memory on polyglot projects | Low | Only start servers for languages actually referenced; idle timeout to shut down unused servers |
| LSP protocol version mismatches | Capability negotiation failures | Low | Support LSP 3.17 baseline; check capabilities before calling methods |
| Stale diagnostics after rapid edits | False error reports | Medium | Debounce `didChange` notifications; wait for diagnostic response before reporting |

---

## 8. What LSP Does NOT Replace

- **Tree-sitter:** Still needed for SAST pattern matching (S-expression queries), wiki AST extraction, and environments where no language server is available. Tree-sitter is embedded and always works; LSP is optional.
- **Ripgrep:** Still needed for raw text search, regex patterns, and searching non-code files. LSP only understands code symbols.
- **LLM analysis:** LSP provides facts (types, references, errors); the LLM provides reasoning (is this a bug? what's the best fix? is this a security issue?).

LSP is a **complement**, not a replacement, to the existing analysis stack.
