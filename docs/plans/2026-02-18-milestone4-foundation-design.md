# Milestone 4 Foundation Design

**Goal:** Wire the remaining integration points so the skill system is fully functional, add Ollama provider, MCP client with auto-discovery, and tree-sitter parsing foundation.

**Scope:** Foundation only. Wiki pipeline, security engine, Xcode tools, and apple-dev skill are deferred to Milestone 5.

**Architecture:** Bottom-up — build leaf dependencies first (Ollama, MCP), then wire Task 18 integration stubs, then MCP-as-skill auto-discovery, then tree-sitter.

**Tech Stack:** Go 1.26, `smacker/go-tree-sitter`, JSON-RPC 2.0, NDJSON streaming, SSE.

---

## Phase 1: New Providers & Tools (~4 days)

### 1a. Ollama Provider (1 day)

**Package:** `internal/provider/ollama/`

Follows the existing Anthropic/OpenAI pattern:
- Implements `provider.LLMProvider` interface
- Custom HTTP client per ADR-006 (no vendor SDK)
- `POST /api/chat` with NDJSON streaming (not SSE)
- Each response line is a complete JSON object; `done: true` signals completion
- Tool use via Ollama's function calling format
- Registers via `init()` side effect
- Config: `[provider.ollama] base_url = "http://localhost:11434"`
- No auth required (local server)

### 1b. MCP Client (3 days)

**Package:** `internal/tools/mcp/`

**Transport layer:**
- `Transport` interface abstracting connection types
- `StdioTransport` — spawns child process, JSON-RPC over stdin/stdout
- `SSETransport` — connects to HTTP SSE endpoint, sends requests via POST

**Client:**
- `Client` manages a single MCP server connection
- `Initialize()` — protocol handshake with capabilities negotiation
- `ListTools()` — discovers available tools
- `CallTool(name, args)` — executes a tool, returns result
- `Close()` — clean shutdown
- JSON-RPC 2.0 with request ID correlation

**Tool wrapping:** Each MCP tool becomes a `tools.Tool`:
- `Name()` = `"mcp_<server>_<tool_name>"`
- `Description()` from MCP tool description
- `InputSchema()` from MCP tool `inputSchema`
- `Execute()` calls `Client.CallTool()`

**Configuration:**
```toml
[[mcp.servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

[[mcp.servers]]
name = "web-search"
transport = "sse"
url = "http://localhost:3001/sse"
```

---

## Phase 2: Task 18 Integration Wiring (~3 days)

Replace the 10 "wired in Task 18" stubs across `goplugin.go` and `engine.go` with real implementations.

### LLMCompleter (engine.go line 21)
- `Complete(ctx, prompt) (string, error)`
- Wraps `provider.LLMProvider.Stream()`, collects streamed text into a single string
- Used by Starlark `llm_complete()` and Go plugin `Complete()`

### HTTPFetcher (engine.go line 27)
- `Fetch(ctx, url) (string, error)`
- `net/http.Get` with 15s timeout, 1MB response body limit
- Used by Starlark `fetch()` and Go plugin `Fetch()`

### GitRunner (engine.go line 46)
- `Diff(ctx, args...) (string, error)`
- `Log(ctx, args...) ([]GitCommit, error)`
- `Status(ctx) ([]GitFileStatus, error)`
- Shells out to `git` CLI, parses output into structured types
- Runs in project working directory
- Used by Starlark `git_diff/log/status()` and Go plugin equivalents

### SkillInvoker (engine.go line 54)
- `Invoke(ctx, name, input) (map[string]any, error)`
- Calls `Runtime.InvokeWorkflow()` for cross-skill invocation
- Used by Starlark `invoke_skill()` and Go plugin `InvokeSkill()`

### Wiring in main.go
- Backend factory creates Starlark/Go plugin backends with real implementations injected
- Each backend accepts integrations via functional options or constructor params

---

## Phase 3: MCP-as-Skill Auto-Discovery (~2 days)

MCP servers from config.toml are automatically discovered as skills.

**Discovery flow:**
- `Loader.Discover()` reads `[[mcp.servers]]` from config after scanning skill directories
- For each server, creates a synthetic `SkillManifest`:
  - `Name` = `"mcp-<server-name>"`
  - `Types` = `[SkillTypeTool]`
  - `Source` = `SourceMCP` (new constant)
  - `Implementation.Backend` = `BackendMCP` (new constant)

**Backend:**
- Backend factory recognizes `BackendMCP`
- Creates an MCP-backed `SkillBackend` that:
  - Connects to the MCP server on `Load()`
  - Returns wrapped MCP tools from `Tools()`
  - Disconnects on `Unload()`

**Behavior:**
- MCP skills auto-activate (no trigger evaluation needed)
- MCP tools inherit `shell:exec` permission (external process execution)

---

## Phase 4: Tree-sitter Foundation (~2 days)

**Package:** `internal/parser/`

Foundation for future wiki scanner and SAST — no rules or analysis logic.

**Dependencies:**
- `github.com/smacker/go-tree-sitter` + grammars for top 9 languages:
  Go, Python, JavaScript, TypeScript, Java, Rust, Ruby, C, C++

**API:**
- `Parser` struct with `Parse(filename, source) (*Tree, error)` — auto-detects language from file extension
- `Tree` wraps tree-sitter CST with helpers:
  - `RootNode()` — AST root node
  - `Query(pattern) []Match` — S-expression pattern matching
  - `Functions() []FunctionDef` — extract function/method definitions (name, start line, end line)
  - `Imports() []string` — extract import statements
- Language registry: maps file extensions to grammars, extensible

---

## What's Already Done (from M3)

These M4 spec items were completed during Milestone 3:
- SQLite persistence (`internal/store/`) — 3 tables, full CRUD
- Public Skill SDK (`pkg/skillsdk/`) — complete interface
- Go plugin backend (`internal/skills/goplugin/`) — full with sandboxing
- Skill registry client (`internal/skills/registry.go`) — Search, GetManifest, Download, InstallFromGit
- Context window manager (`internal/agent/context.go`) — token estimation + truncation

## Deferred to Milestone 5

- Wiki pipeline (6-stage: scanner, chunker, LLM analyzer, diagrams, assembler, renderer)
- Security engine (static scanners + LLM analyzers + attack chain correlator)
- Xcode tools + apple-dev built-in skill
- Documentation + skill authoring guide
- Test coverage push to >90%
