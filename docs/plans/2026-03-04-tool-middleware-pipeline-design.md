# Tool Execution Middleware Pipeline — Design Document

> **Date:** 2026-03-04 · **Status:** Approved
> **Motivation:** Learnings from Claude Code's tool system architecture (DeepWiki analysis)

---

## Problem Statement

Rubichan's `executeSingleTool` in `internal/agent/agent.go` is an 85-line monolith that mixes hook dispatch, tool lookup, execution, post-hooks, and result offloading. This makes it impossible to add new validation stages (classification, shell safety, hard deny rules) without growing the method further.

Additionally, several security-relevant gaps exist compared to Claude Code's tool system:

| Gap | Impact |
|---|---|
| No tool classifier — approval operates on raw tool names | Can't write rules like "all file-write tools require approval" |
| `deny` rules return `ApprovalRequired` (user can still approve) | Security rules are advisory, not enforceable |
| No shell command parsing — patterns match raw strings | `shell(rm *)` matches a file named "rm-history.txt" |
| No hierarchical config cascade | Can't have per-project overrides of global rules |

## Architecture

### New Package: `internal/toolexec/`

A composable middleware chain that replaces the inline stages in `executeSingleTool`.

### Core Types

```go
package toolexec

// ToolCall is the input to the pipeline.
type ToolCall struct {
    ID    string
    Name  string
    Input json.RawMessage
}

// Result is the output of the pipeline.
type Result struct {
    Content        string
    DisplayContent string
    IsError        bool
}

// HandlerFunc executes a tool call and returns a result.
type HandlerFunc func(ctx context.Context, tc ToolCall) Result

// Middleware wraps a HandlerFunc, adding behavior before/after.
type Middleware func(next HandlerFunc) HandlerFunc

// Pipeline composes middlewares around a base executor.
type Pipeline struct {
    middlewares []Middleware
    base        HandlerFunc
}
```

### Middleware Stack (outermost to innermost)

```
1. Classifier      — tags call with category (bash, file_read, file_write, etc.)
2. RuleEngine      — evaluates allow/ask/deny rules; hard-deny short-circuits
3. HookDispatch    — runs HookOnBeforeToolCall; cancel short-circuits
4. ShellSafety     — for bash category: full AST parse, validate sub-commands
5. [base]          — registry.Get(name) → tool.Execute(ctx, input)
6. PostHooks       — runs HookOnAfterToolResult, allows content modification
7. OutputManager   — offloads large results to disk via ResultStore
```

## Component Designs

### 1. Tool Classifier

Maps tool names to abstract categories. Built-in tools have hardcoded mappings; skill/MCP tools classified by source.

```go
type Category string

const (
    CategoryBash      Category = "bash"
    CategoryFileRead  Category = "file_read"
    CategoryFileWrite Category = "file_write"
    CategorySearch    Category = "search"
    CategoryGit       Category = "git"
    CategoryNet       Category = "net"
    CategoryMCP       Category = "mcp"
    CategorySkill     Category = "skill"
    CategoryAgent     Category = "agent"
)
```

The classifier supports user-configured overrides via config. Category is stored in context for downstream middlewares.

### 2. Rule Engine with Hard Deny

Three-tier action model replacing the current binary approved/not-approved:

```go
type RuleAction string

const (
    ActionAllow RuleAction = "allow"  // auto-approve, no prompt
    ActionAsk   RuleAction = "ask"    // prompt user
    ActionDeny  RuleAction = "deny"   // hard block, cannot be overridden
)
```

Rules target categories or tool names with glob patterns:

```go
type PermissionRule struct {
    Category Category
    Tool     string
    Pattern  string
    Action   RuleAction
    Source   ConfigSource
}
```

Evaluation order: deny rules checked first (hard block), then ask, then allow, then category defaults.

The `RuleEngine` also implements `ApprovalChecker` interface to integrate with existing `CompositeApprovalChecker` for the allow/ask path.

### 3. Shell Command Safety

Full shell AST parsing via `mvdan.cc/sh/v3/syntax` (pure Go, same parser as `shfmt`).

Capabilities:
- **Command prefix extraction** from env wrappers and bash flags
- **Compound command decomposition** — each sub-command in `&&`, `||`, `;`, `|` chains evaluated independently
- **Subshell/substitution walking** — catches `$(rm -rf /)` inside an allowed command
- **Quote resolution** — `rm '-rf' /` correctly identified as `rm -rf`

Only activates for tools classified as `CategoryBash`.

### 4. Hierarchical Config Cascade

Rules accumulate across configuration layers:

| Priority | Source | File | Checked In? |
|---|---|---|---|
| 1 (lowest) | Built-in defaults | hardcoded | — |
| 2 | User config | `~/.config/aiagent/config.toml` | No |
| 3 | Project config | `.security.yaml` | Yes |
| 4 (highest) | Local config | `.security.local.yaml` | No (gitignored) |

**Deny rules are absolute** — a deny from any source blocks execution regardless of allow rules from higher-priority sources. This prevents local config from undermining project security policy.

Config surface:

TOML (`config.toml`):
```toml
[[tool_rules]]
category = "bash"
pattern = "rm -rf *"
action = "deny"
```

YAML (`.security.yaml`):
```yaml
tool_rules:
  - category: bash
    pattern: "docker push *"
    action: ask
```

### 5. Built-in Defaults

```
file_read, search  → allow (read-only, safe)
bash, file_write, git, net → ask (mutations require approval)
```

## Integration

### Changes to Existing Files

| File | Change | Size |
|---|---|---|
| `internal/agent/agent.go` | `executeSingleTool` delegates to `pipeline.Execute()` (~60 lines removed, ~10 added) | Small |
| `internal/agent/approval.go` | `RuleEngine` implements `ApprovalChecker` for allow/ask path | Small |
| `cmd/rubichan/main.go` | Pipeline construction and wiring | Medium |
| `internal/config/config.go` | Add `ToolRules` field | Small |
| `.security.yaml` schema | Add `tool_rules` section | Small |

### New Files

| File | Purpose |
|---|---|
| `internal/toolexec/pipeline.go` | Pipeline, Middleware, HandlerFunc, Result types |
| `internal/toolexec/classifier.go` | Classifier, Category constants, context helpers |
| `internal/toolexec/rules.go` | RuleEngine, PermissionRule, LoadRules |
| `internal/toolexec/shell.go` | ShellValidator, ParseCommand via mvdan.cc/sh |
| `internal/toolexec/middleware.go` | All middleware constructors |
| `internal/toolexec/executor.go` | RegistryExecutor base handler |
| + `_test.go` for each | |

### What Doesn't Change

- `Tool` interface (Name, Description, InputSchema, Execute)
- `Registry` (register, get, filter, select)
- Skill system (sandbox, permissions, backends)
- Approval partitioning (parallel/sequential in executeTools)
- Conversation management
- Provider layer

## New Dependency

- `mvdan.cc/sh/v3` — Pure Go shell parser (used by shfmt, Docker). Zero transitive deps. Required for shell AST parsing.

## agent.go After Refactor

```go
func (a *Agent) executeSingleTool(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
    result := a.pipeline.Execute(ctx, toolexec.ToolCall{
        ID: tc.ID, Name: tc.Name, Input: tc.Input,
    })
    return toolExecResult{
        toolUseID: tc.ID,
        content:   result.Content,
        isError:   result.IsError,
        event:     makeToolResultEvent(tc.ID, tc.Name, result.Content, result.DisplayContent, result.IsError),
    }
}
```
