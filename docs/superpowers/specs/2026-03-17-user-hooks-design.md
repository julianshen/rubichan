# User Hooks — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-17 · **Status:** Approved
> **Milestone:** 6, Phase 5
> **Parent:** [Spec Amendments](2026-03-16-spec-amendments-design.md), Amendment 4
> **FRs:** FR-7.12, FR-7.13, FR-7.14, FR-7.15

---

## Overview

User hooks are shell commands that fire on agent events, configured in `config.toml` or `AGENT.md` frontmatter. They integrate into the existing skill hook dispatch system via `LifecycleManager` at a dedicated priority level, avoiding a parallel hook system. Pre-event hooks can block operations; post-event hooks are fire-and-forget.

## Existing Infrastructure

The codebase already provides:
- **`LifecycleManager`** (`internal/skills/hooks.go`) — priority-based hook dispatch with cancellable/modifying semantics
- **`HookPhase`** constants — 11 phases including `HookOnBeforeToolCall`, `HookOnAfterToolResult`, `HookOnConversationStart`
- **Priority system** — `PriorityBuiltin=0`, `PriorityUser=10`, `PriorityProject=20`
- **`SkillHookAdapter`** (`internal/toolexec/hookadapter.go`) — bridges toolexec to skill hooks
- **`HookMiddleware`/`PostHookMiddleware`** — pipeline middleware dispatching hooks before/after tool execution

User hooks register handlers into this existing system. No new middleware or dispatcher needed.

## Components

### 1. UserHookRunner (`internal/hooks/runner.go`)

Core type that converts user-configured hook rules into `HookHandler` functions and registers them into the `LifecycleManager`.

```go
package hooks

type UserHookRunner struct {
    hooks   []UserHookConfig
    workDir string
}

type UserHookConfig struct {
    Event       string        // "pre_tool", "post_edit", "pre_shell", etc.
    Pattern     string        // file glob pattern (optional)
    Command     string        // shell command template with {var} placeholders
    Description string
    Timeout     time.Duration // default 30s
    Source      string        // "config" or "agent.md"
}
```

**`RegisterInto(lm *skills.LifecycleManager)`** — For each `UserHookConfig`:

1. Map the event string to a `HookPhase` constant.
2. Create a `HookHandler` closure that:
   a. Extracts template variables from `HookEvent.Data` (`tool_name`, `input`, etc.)
   b. Checks if the `Pattern` glob matches the affected file (for edit events)
   c. Substitutes template variables into the command string
   d. Executes the command via `exec.CommandContext` with timeout
   e. For pre-events: returns `Cancel: true` on non-zero exit code
   f. For post-events: logs output, ignores exit code
3. Register the handler at `PriorityUserHook = 5` (fires before skill hooks at priority 10).

**Event → Phase mapping:**

| User Event | HookPhase | Filter | Can Block |
|-----------|-----------|--------|-----------|
| `pre_tool` | `HookOnBeforeToolCall` | None | Yes (exit 1) |
| `post_tool` | `HookOnAfterToolResult` | None | No |
| `pre_edit` | `HookOnBeforeToolCall` | tool=file, op=write/patch | Yes (exit 1) |
| `post_edit` | `HookOnAfterToolResult` | tool=file, op=write/patch | No |
| `pre_shell` | `HookOnBeforeToolCall` | tool=shell | Yes (exit 1) |
| `session_start` | `HookOnConversationStart` | None | No |

**Template variables** (substituted in command string before execution):

| Variable | Value | Source |
|----------|-------|--------|
| `{tool}` | Tool name | `event.Data["tool_name"]` |
| `{file}` | File path from tool input | Parsed from `event.Data["input"]` |
| `{command}` | Shell command from tool input | Parsed from `event.Data["input"]` |
| `{turn}` | Current turn number | `event.Data["turn"]` |
| `{session_id}` | Session ID | `event.Data["session_id"]` |

Missing variables are replaced with empty string (no error).

**Input parsing for `{file}` and `{command}`:** The `event.Data["input"]` field contains the raw JSON input string from the tool call. The runner parses this JSON to extract tool-specific fields:
- For `tool=file`: parse `{"operation": ..., "path": ...}` → `{file}` = path value
- For `tool=shell`: parse `{"command": ...}` → `{command}` = command value
- If JSON parsing fails or the field is missing, the variable is empty string (no error)

**Shell execution:**
- Uses `exec.CommandContext(ctx, "sh", "-c", expandedCommand)` with configured timeout
- Working directory set to `runner.workDir`
- Stdout/stderr captured for logging
- Exit code determines block/allow for pre-events

### 2. Priority Constant (`internal/skills/types.go`)

Add new priority level:

```go
const (
    PriorityBuiltin  = 0   // existing
    PriorityUserHook = 5   // NEW — user-configured shell hooks
    PriorityUser     = 10  // existing — skill-provided hooks
    PriorityProject  = 20  // existing
)
```

User hooks fire before skill hooks (lower number = higher priority) for pre-events, and after skill hooks for post-events (LifecycleManager processes all in priority order; for post-events the skill hook at priority 10 runs first, then user hook at priority 5 runs — wait, that's wrong. Lower priority number runs first always).

**Correction:** Since lower priority = runs first, user hooks at priority 5 would run BEFORE skill hooks at priority 10 for ALL events (both pre and post). This matches the spec amendment's requirement: "User hooks fire before skill hooks for pre_* events." For post_* events the amendment says "after skill hooks" — but with priority 5 < 10, user hooks would run first.

To match the spec exactly, we need **different priorities for pre vs post**, or we accept that user hooks always run first (simpler and still gives users ultimate control). The simpler approach is acceptable — if a user hook blocks, skill hooks are skipped (correct), and for post events, user hooks see the result first (harmless).

**Decision:** User hooks always run first (priority 5). This is simpler and gives users ultimate control over their environment.

**Note on modifying phases:** `HookOnAfterToolResult` is a modifying phase — handlers chain data from one to the next. With user hooks at priority 5, a user hook that returns `Modified` data will alter the event before skill hooks at priority 10 see it. This is acceptable: users configured these hooks intentionally, and seeing modified output before skills is consistent with "users have ultimate control." Document this in code comments.

### 3. Config Loading (`internal/config/config.go`)

Add to Config struct:

```go
type HooksConfig struct {
    TrustProjectHooks bool             `toml:"trust_project_hooks"`
    Rules             []HookRuleConfig `toml:"rules"`
}

type HookRuleConfig struct {
    Event       string `toml:"event"`
    Pattern     string `toml:"pattern"`
    Command     string `toml:"command"`
    Description string `toml:"description"`
    Timeout     string `toml:"timeout"` // e.g., "30s", "60s"
}
```

Config TOML example:

```toml
[hooks]
trust_project_hooks = false

[[hooks.rules]]
event = "post_edit"
pattern = "*.go"
command = "gofmt -w {file}"

[[hooks.rules]]
event = "pre_shell"
command = "echo {command} | grep -qv 'rm -rf' || exit 1"
timeout = "5s"
```

### 4. AGENT.md Frontmatter (`internal/config/agentmd.go`)

Extend `LoadAgentMD` to parse YAML frontmatter for hooks:

```go
// LoadAgentMDWithHooks returns the markdown body and any hooks from frontmatter.
func LoadAgentMDWithHooks(projectRoot string) (body string, hooks []HookRuleConfig, err error)
```

Frontmatter format:

```yaml
---
hooks:
  - event: post_edit
    pattern: "*.go"
    command: "gofmt -w {file}"
---

# Project Instructions
...
```

Uses `---` delimiters. Parsing uses simple line-based extraction (split on `---` markers, parse inner YAML via `gopkg.in/yaml.v3`).

**Coexistence with `LoadAgentMD`:** The existing `LoadAgentMD` is updated to strip frontmatter from the returned body (if present), so callers get clean markdown. `LoadAgentMDWithHooks` is a new function that returns both the stripped body and the parsed hooks. Both functions coexist — `LoadAgentMD` is backward-compatible (existing callers get cleaner output since frontmatter is stripped).

### 5. Trust Gate (`internal/hooks/trust.go`)

Project hooks (from AGENT.md) require user approval. Trust stored in SQLite.

```go
// CheckTrust returns true if the project hooks have been approved by the user.
func CheckTrust(store *store.Store, projectPath string, hooks []UserHookConfig) (bool, error)

// ApproveTrust records approval for the given project hooks.
func ApproveTrust(store *store.Store, projectPath string, hooks []UserHookConfig) error
```

**SQLite table** (added to store schema):

```sql
CREATE TABLE IF NOT EXISTS hook_approvals (
    project_path TEXT NOT NULL,
    hook_hash    TEXT NOT NULL,
    approved_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (project_path, hook_hash)
)
```

**Hook hash:** SHA-256 of the concatenated `event+command` strings for all project hooks. If any hook changes, the hash changes and re-approval is required.

**Trust flow:**
1. Load project hooks from AGENT.md frontmatter
2. Compute hash, check `hook_approvals` table
3. If approved: include in runner
4. If not approved: skip project hooks, log warning ("Project hooks not trusted, run with --trust-project-hooks or approve interactively")

Config hooks (from `config.toml`) are always trusted — they're self-authored.

### 6. Agent Wiring (`internal/agent/agent.go`)

New option:

```go
func WithUserHooks(runner *hooks.UserHookRunner) AgentOption
```

In `New()`, after skill runtime setup, if `userHookRunner` is set:

```go
if a.userHookRunner != nil && a.skillRuntime != nil {
    a.userHookRunner.RegisterInto(a.skillRuntime)
}
```

**Note:** `Runtime.lifecycle` is unexported. Rather than exposing the full `LifecycleManager`, add a delegation method to `Runtime`:

```go
// RegisterHook adds a hook handler to the lifecycle manager at the given priority.
func (rt *Runtime) RegisterHook(phase HookPhase, name string, priority int, handler HookHandler) {
    rt.lifecycle.Register(phase, name, priority, handler)
}
```

`UserHookRunner.RegisterInto(*skills.Runtime)` calls this method for each hook config. This maintains encapsulation — callers can register hooks but cannot access the full lifecycle manager.

### 7. Main.go Wiring (`cmd/rubichan/main.go`)

In both interactive and headless setup:

1. Load config hooks: `cfg.Hooks.Rules` → `[]hooks.UserHookConfig`
2. Load AGENT.md hooks: `config.LoadAgentMDWithHooks(cwd)` → frontmatter hooks
3. Check trust for project hooks (skip if not approved)
4. Create `hooks.NewUserHookRunner(allHooks, cwd)`
5. Pass to agent: `agent.WithUserHooks(runner)`

## Scope Exclusions

- **No pre_edit modify via stdout** — block only (exit 1). Simpler and avoids ambiguity about what stdout content means.
- **No `/hooks` TUI command** — hooks are file-configured, not interactive
- **No hook result display** — hooks are background automation
- **No `post_response` event** — requires hooking into the LLM response path, not the tool path. Deferred.
- **No `session_end` event** — requires hooking agent shutdown. Deferred.

## File Summary

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/hooks/runner.go` | `hooks` | UserHookRunner, RegisterInto, shell exec, template substitution |
| `internal/hooks/runner_test.go` | `hooks` | Runner tests: execution, blocking, template vars, timeout |
| `internal/hooks/trust.go` | `hooks` | Trust gate: hash, check, approve |
| `internal/hooks/trust_test.go` | `hooks` | Trust tests |
| `internal/config/config.go` | `config` | Add HooksConfig, HookRuleConfig |
| `internal/config/agentmd.go` | `config` | LoadAgentMDWithHooks with frontmatter parsing |
| `internal/config/agentmd_test.go` | `config` | Frontmatter parsing tests |
| `internal/skills/types.go` | `skills` | Add PriorityUserHook = 5 |
| `internal/store/store.go` | `store` | Add hook_approvals table |
| `internal/agent/agent.go` | `agent` | WithUserHooks option, wiring |
| `cmd/rubichan/main.go` | `main` | Load hooks, create runner, pass to agent |

## Dependencies

- `gopkg.in/yaml.v3` — for AGENT.md frontmatter parsing (if not already in go.mod)
- `crypto/sha256` — for hook content hashing (stdlib)
- No other new dependencies
