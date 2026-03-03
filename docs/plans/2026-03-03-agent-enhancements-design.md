# Agent Enhancements Design

**Date:** 2026-03-03
**Status:** Approved

## Overview

Five enhancements inspired by Claude Code's agent architecture, designed as a single cohesive milestone. These build on Rubichan's existing modular architecture (Provider, Tool, Approval, Skill interfaces) to add subagent support, agent definitions, async wake, glob-style permission patterns, and multi-provider prompt caching.

## Enhancement 1: Subagent System & Task Tool

### Concept

A `TaskTool` registered in the tool registry lets the parent agent spawn child agents. Each child is a full `Agent` instance with its own conversation, context manager, and configurable tool subset. Children inherit the parent's `LLMProvider` and `ApprovalChecker`.

### Core Types

```go
// internal/agent/subagent.go

type SubagentConfig struct {
    Name         string   // Identifier (e.g., "explorer")
    SystemPrompt string   // Additional system prompt (appended to base)
    Tools        []string // Whitelist of tool names (nil = all parent tools)
    MaxTurns     int      // Turn limit (default: 10)
    MaxTokens    int      // Output token budget
    Model        string   // Override model (empty = inherit)
    Depth        int      // Current nesting level (0 = top-level)
    MaxDepth     int      // Maximum nesting (default: 3)
}

type SubagentResult struct {
    Name         string
    Output       string
    ToolsUsed    []string
    TurnCount    int
    InputTokens  int
    OutputTokens int
    Error        error
}

type SubagentSpawner interface {
    Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
}
```

### Task Tool

```go
// internal/tools/task.go

// Input schema:
//   prompt: string (required) - Task description
//   agent_type: string (optional) - Named agent definition
//   max_turns: int (optional) - Override max turns
//   background: bool (optional) - Run asynchronously
```

### Spawning Flow

1. TaskTool looks up AgentDef by `agent_type` (or uses "general")
2. Calls `spawner.Spawn(ctx, config, prompt)`
3. Spawner creates a child Agent with filtered tool registry
4. Child runs its turn loop, collects output
5. Returns SubagentResult formatted as ToolResult

### Recursive Spawning

Subagents can spawn their own subagents with depth tracking. The TaskTool is included in subagent registries. Spawning fails if `depth >= maxDepth` (default: 3).

### Tool Filtering

```go
// New method on tools.Registry
func (r *Registry) Filter(names []string) *Registry
```

Creates a new registry containing only the named tools.

## Enhancement 2: Agent Definitions

### AgentDef Type

```go
// internal/agent/agentdef.go

type AgentDef struct {
    Name         string   `toml:"name" yaml:"name"`
    Description  string   `toml:"description" yaml:"description"`
    SystemPrompt string   `toml:"system_prompt" yaml:"system_prompt"`
    Tools        []string `toml:"tools" yaml:"tools"`
    MaxTurns     int      `toml:"max_turns" yaml:"max_turns"`
    MaxDepth     int      `toml:"max_depth" yaml:"max_depth"`
    Model        string   `toml:"model" yaml:"model"`
}
```

### Agent Definition Registry

```go
type AgentDefRegistry struct {
    mu   sync.RWMutex
    defs map[string]*AgentDef
}

func (r *AgentDefRegistry) Register(def *AgentDef) error
func (r *AgentDefRegistry) Get(name string) (*AgentDef, bool)
func (r *AgentDefRegistry) All() []*AgentDef
func (r *AgentDefRegistry) Unregister(name string) error
```

### Registration Sources

1. **Config TOML:**
```toml
[[agent.definitions]]
name = "explorer"
description = "Explore codebase and find relevant files"
system_prompt = "You are a code exploration agent."
tools = ["search", "file"]
max_turns = 5
```

2. **Skill Manifests:**
```yaml
agents:
  - name: k8s-debugger
    description: "Debug Kubernetes issues"
    system_prompt: "You specialize in K8s troubleshooting."
    tools: ["shell", "file"]
    max_turns = 8
```

3. **Built-in (code):** A "general" agent registered at startup (all tools, no extra prompt).

### Skill Backend Extension

Add `Agents() []*agent.AgentDef` to `SkillBackend` interface (same pattern as `Commands()`). Agent defs registered on skill activation, unregistered on deactivation.

## Enhancement 3: Async Wake Mechanism

### Concept

When `background: true` is passed to TaskTool, the subagent runs in a goroutine. The parent continues conversing. When the subagent finishes, a wake event notifies the parent.

### Core Types

```go
// internal/agent/wake.go

type WakeEvent struct {
    AgentName string
    TaskID    string
    Result    *SubagentResult
}

type WakeManager struct {
    mu      sync.Mutex
    pending map[string]*backgroundTask
    wakeCh  chan WakeEvent
}

type backgroundTask struct {
    ID        string
    AgentName string
    Cancel    context.CancelFunc
}

func (wm *WakeManager) Submit(name string, cancel context.CancelFunc) string  // returns task ID
func (wm *WakeManager) Complete(taskID string, result *SubagentResult)
func (wm *WakeManager) Events() <-chan WakeEvent
func (wm *WakeManager) Status() []TaskStatus
```

### Agent Loop Integration

Between turns, the agent checks for wake events:

```go
select {
case wake := <-a.wakeManager.Events():
    a.conversation.AddUser(fmt.Sprintf(
        "[Background task %q completed]\n%s", wake.AgentName, wake.Result.Output))
default:
}
```

### New TurnEvent Type

```go
TurnEvent{Type: "subagent_done", SubagentResult: &result}
```

The TUI renders notifications when background tasks complete.

### Task Tracking Tool

A `list_tasks` tool lets the agent check background task status:

```go
// Input: {} (no args)
// Output: [{id, agent_name, status: "running"|"completed"|"failed"}]
```

## Enhancement 4: Permission Pattern Matching (Glob-style)

### User-Facing Syntax

```toml
[[agent.trust_rules]]
glob = "shell(git *)"
action = "allow"

[[agent.trust_rules]]
glob = "shell(rm -rf *)"
action = "deny"

[[agent.trust_rules]]
glob = "file(read:*.go)"
action = "allow"

[[agent.trust_rules]]
glob = "task(*)"
action = "allow"

# Legacy regex still works:
[[agent.trust_rules]]
tool = "shell"
pattern = "^npm (test|run)"
action = "allow"
```

### Glob Syntax

| Pattern | Meaning |
|---------|---------|
| `*` | Match any sequence of characters |
| `?` | Match any single character |
| `[abc]` | Match character class |
| `shell(git *)` | Tool "shell", input matches glob `git *` |

### Implementation

```go
// internal/agent/approval.go

type GlobTrustRule struct {
    Glob   string `toml:"glob"`
    Action string `toml:"action"`
}

func ParseGlobRule(glob string) (tool string, re *regexp.Regexp, err error)
```

ParseGlobRule splits `ToolName(pattern)` into tool name + glob. Converts glob to regex (`*` -> `.*`, `?` -> `.`). Anchors with `^...$`. Returns compiled regex.

### Config Integration

```go
// internal/config/config.go
type TrustRuleConf struct {
    Tool    string `toml:"tool"`
    Pattern string `toml:"pattern"`
    Glob    string `toml:"glob"`     // New: "tool(pattern)" syntax
    Action  string `toml:"action"`
}
```

At construction, glob rules compile into the same `[]compiledRule` slice as regex rules. The TrustRuleChecker is unchanged internally.

## Enhancement 5: Prompt Caching (All Providers)

### Anthropic: Enhanced Multi-Breakpoint

Additional cache points beyond the existing system prompt breakpoint:

1. **System prompt** (existing) — cacheable, stable across turns
2. **Tool definitions** — mark last tool def with `cache_control: {"type": "ephemeral"}`
3. Conversation prefix is auto-cached by the API for long prompts

### OpenAI: Structured for Auto-Caching

OpenAI auto-caches prompts >1024 tokens. Maximize hits by:

1. Sort tool definitions alphabetically before serialization (deterministic hashing)
2. Keep stable content (system prompt) first — already done by PromptBuilder

No API changes needed — caching is transparent.

### Ollama: Model Keep-Alive

```go
type ollamaChatRequest struct {
    // ... existing fields ...
    KeepAlive string `json:"keep_alive,omitempty"` // e.g., "5m", "10m", "-1"
}
```

Config:
```toml
[agent.cache]
ollama_keep_alive = "10m"
```

### PromptBuilder Enhancement

Support multiple cache breakpoints. Providers that don't support breakpoints ignore them.

## Cross-Cutting Concerns

### Subagent + Approval

Subagents inherit the parent's `CompositeApprovalChecker`. Glob rules like `task(agent_type:explorer)` control which agent types can be spawned. The session-level "always approve" cache is NOT inherited (each subagent gets its own approval scope).

### Subagent + Caching

Subagents benefit from prompt caching automatically — they use the same provider, and their system prompts are cacheable via the same PromptBuilder.

### Agent Defs + Skills

Skills can register agent definitions via `Agents()` on `SkillBackend`. The `AgentDefRegistry` is passed to the skill runtime via `SetAgentDefRegistry()`, mirroring `SetCommandRegistry()`.

## Testing Strategy

- **SubagentSpawner:** Mock interface for unit testing TaskTool
- **AgentDefRegistry:** Unit tests for Register/Get/All/Unregister
- **WakeManager:** Unit tests for Submit/Complete/Events, concurrent safety
- **GlobTrustRule:** Unit tests for ParseGlobRule with various patterns
- **Tool filtering:** Unit tests for Registry.Filter
- **Provider caching:** Unit tests for each provider's cache behavior
- **Integration:** End-to-end test spawning a subagent with a mock provider
