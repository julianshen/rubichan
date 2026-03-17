# Hierarchical Permissions — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-17 · **Status:** Approved
> **Milestone:** 6, Phase 3
> **Parent:** [Spec Amendments](2026-03-16-spec-amendments-design.md), Amendment 6
> **FRs:** FR-7.16, FR-7.17, FR-7.18, FR-7.19, FR-7.20

---

## Overview

Add a `HierarchicalChecker` that implements `agentsdk.ApprovalChecker` and evaluates tool/shell/file permissions against a cascade of policy files: Organization → Project → User. It slots into the existing `CompositeApprovalChecker` chain as the first checker, ensuring policy-level decisions take priority over session cache and config trust rules.

## Existing Infrastructure

The codebase already provides:
- **`ApprovalChecker`** (`pkg/agentsdk/approval.go`) — interface returning `ApprovalResult` (AutoApproved, AutoDenied, TrustRuleApproved, ApprovalRequired)
- **`CompositeApprovalChecker`** — chains multiple checkers; first non-`ApprovalRequired` result wins
- **`TrustRuleChecker`** (`internal/agent/approval.go`) — regex/glob pattern matching for trust rules
- **`ApprovalFunc`** — async fallback for TUI prompting when result is `ApprovalRequired`

The hierarchical checker operates within this existing framework. No interface changes needed.

## Components

### 1. Policy Types (`internal/permissions/policy.go`)

```go
package permissions

// Policy represents a permission policy loaded from a single source.
type Policy struct {
    Level  string     // "org", "project", "user"
    Source string     // file path this was loaded from
    Tools  ToolPolicy
    Shell  ShellPolicy
    Files  FilePolicy
    Skills SkillPolicy
}

type ToolPolicy struct {
    Allow  []string `toml:"allow"`   // tool names always allowed
    Deny   []string `toml:"deny"`    // tool names always blocked
    Prompt []string `toml:"prompt"`  // tool names requiring approval each time
}

type ShellPolicy struct {
    AllowCommands  []string `toml:"allow_commands"`  // command prefixes always allowed
    DenyCommands   []string `toml:"deny_commands"`   // command prefixes always blocked
    PromptPatterns []string `toml:"prompt_patterns"` // command prefixes requiring approval
}

type FilePolicy struct {
    AllowPatterns  []string `toml:"allow_patterns"`  // glob patterns for allowed files
    DenyPatterns   []string `toml:"deny_patterns"`   // glob patterns for denied files
    PromptPatterns []string `toml:"prompt_patterns"` // glob patterns requiring approval
}

type SkillPolicy struct {
    AutoApprove []string `toml:"auto_approve"` // skill names auto-approved
    Deny        []string `toml:"deny"`         // skill names blocked
}
```

### 2. Policy Loading (`internal/permissions/policy.go`)

```go
// LoadPolicies reads policy files from up to three sources and returns
// them in precedence order (org first, project second, user third).
// Missing files are silently skipped.
func LoadPolicies(orgPath, projectPath string, userPermissions *UserPermissions) ([]Policy, error)
```

**File locations:**

| Level | Path | Created by |
|-------|------|-----------|
| Org | `~/.config/aiagent/org-policy.toml` | Fleet management / security team |
| Project | `.agent/permissions.toml` | Committed to repo by tech lead |
| User | `~/.config/aiagent/config.toml` `[permissions]` section | Individual developer |

**TOML format** (identical at all levels):

```toml
[tools]
allow = ["file", "code_search", "ast_search"]
deny = []
prompt = ["shell"]

[shell]
allow_commands = ["go test", "go build", "gofmt"]
deny_commands = ["rm -rf /", "rm -rf ~"]

[files]
allow_patterns = ["*.go", "*.md"]
deny_patterns = [".env", "*.pem", "*.key"]

[skills]
auto_approve = ["core-tools", "git"]
deny = []
```

For the user config, permissions are parsed from a `[permissions]` section in the existing `config.toml`. The `UserPermissions` type mirrors the TOML structure and is extracted from the config during loading.

**Loading behavior:**
- Files that don't exist are skipped (not an error).
- Files that exist but have parse errors return an error (fail-fast on malformed policy).
- Empty policies (all fields empty) are still included in the chain (no-op).

### 3. HierarchicalChecker (`internal/permissions/checker.go`)

```go
// HierarchicalChecker implements agentsdk.ApprovalChecker by evaluating
// tool calls against a cascade of permission policies.
type HierarchicalChecker struct {
    policies []Policy // ordered: org first, project second, user third
}

func NewHierarchicalChecker(policies []Policy) *HierarchicalChecker
```

**`CheckApproval(tool string, input json.RawMessage) ApprovalResult`:**

Resolution algorithm (three passes, strict precedence: **Deny > Prompt > Allow**):

1. **Deny pass** (all levels, org first): For each policy, check if the tool/input matches a deny rule. If any policy denies → return `AutoDenied`. Org deny cannot be overridden by project or user allow. **Note:** This intentionally uses `AutoDenied` (hard block, unbypassable) rather than `ApprovalRequired` used by existing `TrustRuleChecker`. The distinction is deliberate: policy-level denials are absolute and cannot be overridden by session cache or TUI "always approve".

2. **Prompt pass** (all levels, org first): For each policy, check if the tool/input matches a prompt rule (`Tools.Prompt`, `Shell.PromptPatterns`, `Files.PromptPatterns`). If any policy requires prompting → return `ApprovalRequired`. This ensures that a prompt requirement at any level is not silently overridden by an allow rule at a lower level.

3. **Allow pass** (all levels, org first): For each policy, check if the tool/input matches an allow rule. If any policy allows → return `TrustRuleApproved`.

4. **No match** → return `ApprovalRequired` (fall through to next checker in composite chain).

**Tool-specific matching:**

- **Generic tools:** Match tool name against `Tools.Allow`/`Deny`/`Prompt` lists.
- **Shell tool:** When `tool == "shell"`, extract the command string from `input` JSON (`{"command": "go test ./..."}`). Split command into first token (the executable) and remaining args. Match against `Shell.AllowCommands`/`DenyCommands` using **first-token + prefix** matching: the pattern must match the first token exactly, then optionally match the beginning of the remaining args. E.g., pattern `"go test"` matches `"go test ./..."` but NOT `"gorilla-tool"`. Pattern `"rm"` matches `"rm file.txt"` but NOT `"rmdir"`. Deny patterns are also checked against the **full command string** to catch injection like `"go test && rm -rf /"` (the `&&` creates a new command that should be checked independently).
- **File tool:** When `tool == "file"` and operation is `"write"` or `"patch"`, extract `path` from `input` JSON. Match against `Files.AllowPatterns`/`DenyPatterns` using `path.Match` glob patterns. **Note:** `path.Match` (like `filepath.Match`) supports single-directory wildcards (`*`) but NOT recursive `**` patterns. This is a known limitation — patterns like `internal/**/*.go` will not work. Use `internal/*.go` for single-level or multiple patterns for deeper nesting.

**Explain method:**

```go
// Explain re-evaluates the tool/input against policies and returns a
// human-readable reason for the decision. Returns empty string if no
// policy matched. This is stateless — it re-runs the resolution algorithm.
func (h *HierarchicalChecker) Explain(tool string, input json.RawMessage) string
```

Returns strings like:
- `"denied by org policy (~/.config/aiagent/org-policy.toml): tool 'shell' is in deny list"`
- `"allowed by project policy (.agent/permissions.toml): file pattern '*.go' matches"`

### 4. Composite Chain Integration

**In `cmd/rubichan/main.go`**, during agent setup, the hierarchical checker is inserted at the front of the composite chain:

```
[hierarchical] → session cache → pipeline rule engine → config trust rules
```

```go
orgPath := filepath.Join(configDir, "org-policy.toml")
projectPath := filepath.Join(workDir, ".agent", "permissions.toml")
userPerms := extractUserPermissions(cfg) // from config.toml [permissions]

policies, err := permissions.LoadPolicies(orgPath, projectPath, userPerms)
if err != nil {
    log.Printf("warning: failed to load permission policies: %v", err)
}

if len(policies) > 0 {
    hierarchicalChecker := permissions.NewHierarchicalChecker(policies)
    checkers = append([]agentsdk.ApprovalChecker{hierarchicalChecker}, checkers...)
}
```

This ensures:
- Org deny overrides everything (even session "always approve")
- Project allow pre-approves tools for all contributors
- User preferences fill gaps not covered by org/project
- Session cache and trust rules still work for anything not covered by policies

### 5. Config Extension

Add `[permissions]` section to `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    Permissions PermissionsConfig `toml:"permissions"`
}

type PermissionsConfig struct {
    Tools  ToolPolicy  `toml:"tools"`
    Shell  ShellPolicy `toml:"shell"`
    Files  FilePolicy  `toml:"files"`
    Skills SkillPolicy `toml:"skills"`
}
```

The types `ToolPolicy`, `ShellPolicy`, `FilePolicy`, `SkillPolicy` are defined in `internal/permissions/policy.go` and imported by the config package — or the config package defines its own mirror types to avoid a dependency from config → permissions. The latter is cleaner since `config` is a leaf package.

**Decision:** Define TOML struct types in `internal/permissions/policy.go`. The config package uses plain TOML maps or a separate struct that `LoadPolicies` converts. This avoids config → permissions import.

## Scope Exclusions

- **No Explain() wiring into TUI** — the method exists but the TUI does not display it yet (follow-up)
- **No skill permission integration** — `Skills.AutoApprove`/`Deny` are parsed but not wired to the skill runtime (ADR-009 interaction is a follow-up)
- **No org policy distribution** — we define the format, not how orgs distribute the file
- **No session-level grants via `/allow`** — the existing session cache already handles this

## File Summary

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/permissions/policy.go` | `permissions` | Policy struct, TOML parsing, LoadPolicies |
| `internal/permissions/policy_test.go` | `permissions` | Loading tests: valid, missing, malformed files |
| `internal/permissions/checker.go` | `permissions` | HierarchicalChecker, CheckApproval, Explain |
| `internal/permissions/checker_test.go` | `permissions` | Resolution tests: deny-wins, allow, shell/file matching |
| `internal/config/config.go` | `config` | Add PermissionsConfig struct |
| `cmd/rubichan/main.go` | `main` | Wire hierarchical checker into composite chain |

## Dependencies

- `github.com/BurntSushi/toml` — already in go.mod for config parsing
- No new external dependencies
- `internal/permissions` imports `pkg/agentsdk` for `ApprovalChecker` interface and `ApprovalResult` constants
- `internal/permissions` does NOT import `internal/agent`, `internal/config`, or `internal/tools` — clean dependency direction
