# Plan: `pkg/agentsdk/` — Public Agent SDK

## Goal

Export the Rubichan agent core as a reusable Go library so external applications (web UIs, NATS bridges, chatbots like OpenClaw) can import and use it without depending on `internal/` packages.

## Architecture

```
pkg/agentsdk/           ← Public API (types + interfaces + agent core)
                           NO imports from internal/

internal/provider/      ← Type aliases to pkg/agentsdk/ + provider implementations
internal/tools/         ← Type aliases to pkg/agentsdk/ + built-in tools
internal/agent/         ← Type aliases to pkg/agentsdk/ + CLI-specific helpers
internal/tui/           ← Consumes pkg/agentsdk/ types (via aliases)
cmd/rubichan/           ← Wires internal implementations into pkg/agentsdk/ interfaces
```

External consumer usage:
```go
import "github.com/julianshen/rubichan/pkg/agentsdk"

agent := agentsdk.NewAgent(myProvider,
    agentsdk.WithTools(myRegistry),
    agentsdk.WithModel("claude-sonnet-4-20250514"),
    agentsdk.WithApproval(myApprovalHandler),
    agentsdk.WithSystemPrompt("You are a helpful assistant."),
)

events, _ := agent.Turn(ctx, "Hello")
for ev := range events {
    switch ev.Type {
    case "text_delta":  websocket.Send(ev.Text)
    case "tool_call":   log.Info("calling", ev.ToolCall.Name)
    case "done":        break
    }
}
```

## Dependency Direction (No Cycles)

```
pkg/agentsdk/  →  (nothing internal)
internal/*     →  pkg/agentsdk/ (via type aliases)
cmd/rubichan/  →  both pkg/agentsdk/ and internal/ (wiring)
external-app   →  pkg/agentsdk/ only
```

## Public API Surface

### `pkg/agentsdk/types.go` — Wire Types
- `Message`, `ContentBlock`, `ToolDef`, `ToolUseBlock`
- `StreamEvent`, `CompletionRequest`

### `pkg/agentsdk/provider.go` — LLM Interface
- `LLMProvider` interface: `Stream(ctx, CompletionRequest) (<-chan StreamEvent, error)`

### `pkg/agentsdk/tool.go` — Tool Interface
- `Tool` interface: `Name()`, `Description()`, `InputSchema()`, `Execute()`
- `ToolResult`, `EventStage`, `ToolEvent`, `StreamingTool`

### `pkg/agentsdk/registry.go` — Tool Registry
- `ToolRegistry` interface: `Register()`, `Get()`, `All()`
- `NewRegistry()` — standalone implementation (no internal import)

### `pkg/agentsdk/events.go` — Agent Events
- `TurnEvent`, `ToolCallEvent`, `ToolResultEvent`, `ToolProgressEvent`
- `SubagentResult`

### `pkg/agentsdk/approval.go` — Approval System
- `ApprovalFunc`, `ApprovalChecker`, `ApprovalResult`
- `AutoApproveChecker`, `AlwaysAutoApprove`

### `pkg/agentsdk/config.go` — Agent Configuration
- `AgentConfig` (replaces `*config.Config` dependency):
  ```go
  type AgentConfig struct {
      Model                  string
      MaxTurns               int     // default 50
      ContextBudget          int     // default 100000
      MaxOutputTokens        int     // default 4096
      CompactTrigger         float64 // default 0.95
      HardBlock              float64 // default 0.98
      ResultOffloadThreshold int     // default 4096
      ToolDeferralThreshold  float64 // default 0.10
      SystemPrompt           string  // optional override
  }
  ```

### `pkg/agentsdk/logger.go` — Structured Logging
- `Logger` interface: `Warn(msg, args...)`, `Error(msg, args...)`
- `DefaultLogger()` wrapping `log.Printf`

### `pkg/agentsdk/persistence.go` — Storage Interface
- `PersistenceStore` interface (session, messages, snapshots, blobs)

### `pkg/agentsdk/compaction.go` — Context Management
- `CompactionStrategy` interface, `ContextBudget`, `CompactResult`

### `pkg/agentsdk/summarizer.go` — Summarization
- `Summarizer` interface

### `pkg/agentsdk/memory.go` — Cross-Session Memory
- `MemoryStore` interface, `MemoryEntry`

### `pkg/agentsdk/subagent.go` — Child Agents
- `SubagentConfig`, `SubagentResult`, `SubagentSpawner` interface

### `pkg/agentsdk/agent.go` — Agent Core
- `Agent` struct with `Turn(ctx, msg) (<-chan TurnEvent, error)`
- `NewAgent(provider, opts...) *Agent` constructor
- `Option` functional options
- Core loop: `runLoop()`, `executeTools()`

## Migration: Type Alias Strategy

To avoid breaking existing internal code, use Go type aliases:

**Before** (`internal/provider/types.go`):
```go
type Message struct { Role string; Content []ContentBlock }
```

**After** (`internal/provider/types.go`):
```go
import "github.com/julianshen/rubichan/pkg/agentsdk"
type Message = agentsdk.Message
type ContentBlock = agentsdk.ContentBlock
```

All existing code using `provider.Message` compiles unchanged because aliases are transparent.

## PR Sequence

### Phase 1: Extract Public Types (Structural — No Behavior Change)

- [x] **PR 1: [STRUCTURAL] Define message and provider types in `pkg/agentsdk/`**
  - New: `pkg/agentsdk/types.go`, `pkg/agentsdk/provider.go`
  - Modify: `internal/provider/types.go` → type aliases
  - Keep helper functions (`NewUserMessage`, etc.) in `internal/provider/`
  - Tests: all existing tests pass; new type property tests

- [x] **PR 2: [STRUCTURAL] Define tool types in `pkg/agentsdk/`**
  - New: `pkg/agentsdk/tool.go`, `pkg/agentsdk/registry.go`
  - Modify: `internal/tools/` → type aliases
  - Tests: all existing tool tests pass

- [x] **PR 3: [STRUCTURAL] Define agent event and approval types in `pkg/agentsdk/`**
  - New: `pkg/agentsdk/events.go`, `pkg/agentsdk/approval.go`, `pkg/agentsdk/compaction.go`, `pkg/agentsdk/summarizer.go`, `pkg/agentsdk/memory.go`, `pkg/agentsdk/subagent.go`
  - Modify: `internal/agent/` → type aliases
  - Tests: all existing agent tests pass

### Phase 2: Agent Core and Constructor (Behavioral)

- [x] **PR 4: [BEHAVIORAL] Define `AgentConfig`, `Logger`, `PersistenceStore` in `pkg/agentsdk/`**
  - New: `pkg/agentsdk/config.go`, `pkg/agentsdk/logger.go`, `pkg/agentsdk/persistence.go`
  - Tests: defaults, logger contract

- [x] **PR 5: [BEHAVIORAL] Move agent core loop to `pkg/agentsdk/` + add `NewAgent()` constructor**
  - Move `Agent`, `Turn()`, `runLoop()`, `executeTools()` to `pkg/agentsdk/agent.go`
  - Use interfaces for optional deps (`PersistenceStore`, skill hooks, diff tracker)
  - Keep CLI-specific helpers in `internal/agent/` (persona, ResultStore, agentCompactor)
  - Tests: construct agent via `agentsdk.NewAgent()` with mock provider, run turns
  - Note: PR 7 (standalone Registry) folded into this PR

- [x] **PR 6: [BEHAVIORAL] Replace `log.Printf` with `Logger` in agent core**
  - Thread `Logger` through `Agent` constructor
  - Replace ~10 `log.Printf("warning: ...")` calls
  - Tests: verify log output goes through logger

- [x] **PR 7: [BEHAVIORAL] Add standalone `NewRegistry()` in `pkg/agentsdk/`**
  - Folded into PR 5

### Phase 3: Update Internal Consumers (Structural)

- [x] **PR 8: [STRUCTURAL] Update `cmd/rubichan/` to use `pkg/agentsdk/` imports**
  - Skipped — type aliases in `internal/` make this unnecessary; types are already unified
- [x] **PR 9: [STRUCTURAL] Update `internal/tui/` and `internal/runner/` to use `pkg/agentsdk/` types**
  - Skipped — type aliases in `internal/` make this unnecessary; types are already unified

### Phase 4: Documentation (Behavioral)

- [x] **PR 10: [BEHAVIORAL] Add `doc.go`, examples, and usage guide**
  - `pkg/agentsdk/doc.go`
  - `pkg/agentsdk/example_test.go`
  - Example: web UI consumer, NATS bridge consumer
  - Coverage: 95.8%

## What Stays Internal (Not Exposed in v1)

| Package | Reason |
|---------|--------|
| `skills.Runtime` | Deeply coupled to Starlark/Go-plugin/process backends; CLI-specific |
| `DiffTracker` | TUI-specific file change visualization |
| `WakeManager` | Advanced background subagent management |
| `Scratchpad` | Internal agent note-taking |
| `PromptBuilder` | Internal prompt assembly |
| `ResultStore` | Internal result offloading (exposed via `PersistenceStore` interface) |
| `persona` | Rubichan-specific personality; external consumers set own system prompt |

## Key Design Decisions

1. **Types live in `pkg/agentsdk/`, not `internal/`** — canonical source of truth for all public API types
2. **Type aliases in `internal/`** — zero-cost, transparent, no code changes needed in existing files
3. **`AgentConfig` replaces `*config.Config`** — no TOML paths, no API keys, just agent-relevant params
4. **`Logger` interface replaces `log.Printf`** — structured, injectable, testable
5. **Skills NOT exposed in v1** — too CLI-specific; custom tools cover the use case
6. **Persona NOT in SDK** — external consumers provide their own system prompt

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Circular dependency `pkg/agentsdk ↔ internal/agent` | Types in `pkg/agentsdk/` only; `internal/` aliases one-way; core loop moves to `pkg/agentsdk/` in PR 5 |
| Moving core loop breaks internal consumers | Type aliases make the transition transparent; TUI/headless see same types |
| Large PR 5 (moving core loop) | Can split into sub-PRs: move `Conversation` first, then `ContextManager`, then `Agent` |
| Test coverage drop during migration | Each PR maintains >90% coverage; tests move with the code |
