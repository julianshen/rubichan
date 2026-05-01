# Query Loop Improvements: Batch 3.10 — Permission Modes

> **Status:** Planned, not started.  
> **Depends on:** None (extends existing permission system).  
> **Goal:** Add ccgo-style permission modes (plan/auto/fullAuto/bypass) for common developer workflows.

## Background

The current permission system (`internal/permissions/`) uses a hierarchical policy checker (`HierarchicalChecker`) that evaluates tool calls against org/project/user policies. Each policy has allow/deny/prompt lists for tools, shell commands, file patterns, and skills.

The checker returns one of four `ApprovalResult` values:
- `AutoApproved` — tool is unconditionally safe
- `TrustRuleApproved` — tool matched an allow rule
- `ApprovalRequired` — user must explicitly approve
- `AutoDenied` — tool is explicitly denied

However, there's no concept of a **mode** that changes the default behavior. Users must configure detailed policies for every project. ccgo and Claude Code support modes that change the default stance:

- **plan** (default): Ask for approval on all non-read-only tools
- **auto**: Auto-approve tools that match trust rules; prompt on everything else
- **fullAuto**: Auto-approve everything except explicitly denied tools
- **bypass**: Approve everything (dangerous, for emergency recovery)

## Design

### PermissionMode Enum

```go
// PermissionMode controls the default approval stance when no explicit
// policy rule matches.
type PermissionMode int

const (
    // ModePlan is the default: read-only tools are auto-approved,
    // everything else requires explicit approval.
    ModePlan PermissionMode = iota
    
    // ModeAuto auto-approves tools that match trust rules,
    // prompts on everything else.
    ModeAuto
    
    // ModeFullAuto auto-approves all tools except explicitly denied.
    ModeFullAuto
    
    // ModeBypass approves everything. Dangerous — for emergency use only.
    ModeBypass
)
```

### Mode-Aware Checker

Wrap `HierarchicalChecker` with a `ModeAwareChecker` that adjusts the fallback result based on the current mode:

```go
type ModeAwareChecker struct {
    mode    PermissionMode
    checker *permissions.HierarchicalChecker
    readOnlyTools map[string]bool
}

func (c *ModeAwareChecker) CheckApproval(tool string, input json.RawMessage) agentsdk.ApprovalResult {
    // First, evaluate hierarchical policies (deny always wins).
    result := c.checker.CheckApproval(tool, input)
    
    // If policy returned a definitive result, respect it.
    if result == agentsdk.AutoDenied {
        return agentsdk.AutoDenied
    }
    if result == agentsdk.TrustRuleApproved || result == agentsdk.AutoApproved {
        return result
    }
    
    // Policy returned ApprovalRequired. Apply mode logic.
    switch c.mode {
    case ModeBypass:
        return agentsdk.AutoApproved
    case ModeFullAuto:
        return agentsdk.AutoApproved
    case ModeAuto:
        // Only auto-approve if the hierarchical checker would have
        // approved (i.e., there's an allow rule). In practice this
        // means ModeAuto behaves like ModePlan when no allow rules
        // match, but the UI can communicate "auto mode" to the user.
        return agentsdk.ApprovalRequired
    case ModePlan:
        // Read-only tools are auto-approved in plan mode.
        if c.readOnlyTools[tool] {
            return agentsdk.AutoApproved
        }
        return agentsdk.ApprovalRequired
    }
    return agentsdk.ApprovalRequired
}
```

### Read-Only Tool Set

The `readOnlyTools` set includes: `read_file`, `grep`, `glob`, `list_dir`, `code_search`, `view`, `ls`, `cat`, `find`.

### Configuration

Add `mode` field to `PermissionsConfig`:

```toml
[permissions]
mode = "plan"  # plan | auto | fullAuto | bypass
```

### CLI Flag

Add `--mode` flag to override config:

```bash
rubichan --mode=fullAuto
rubichan --mode=bypass  # requires confirmation
```

## Implementation Plan

### Task 1: Add PermissionMode enum

File: `pkg/agentsdk/approval.go`

Add `PermissionMode` type and constants. Add `String()` method.

### Task 2: Add ModeAwareChecker

File: `internal/permissions/mode_checker.go`

Create `ModeAwareChecker` struct that wraps `HierarchicalChecker`.

### Task 3: Add mode to config

File: `internal/config/config.go`

Add `Mode string` field to `PermissionsConfig`.

### Task 4: Parse mode in main

File: `cmd/rubichan/main.go`

Parse `cfg.Permissions.Mode` and create appropriate `ModeAwareChecker`.

### Task 5: Add CLI flag

File: `cmd/rubichan/main.go`

Add `--mode` flag that overrides config.

### Task 6: Tests

- `TestModeAwareChecker_ModePlan_ReadOnlyApproved`
- `TestModeAwareChecker_ModePlan_WriteRequiresApproval`
- `TestModeAwareChecker_ModeFullAuto_ApprovesAllExceptDenied`
- `TestModeAwareChecker_ModeBypass_ApprovesAll`
- `TestModeAwareChecker_DenyAlwaysWins`
- `TestModeAwareChecker_String`

## Files to modify

| File | Changes |
|------|---------|
| `pkg/agentsdk/approval.go` | Add PermissionMode enum |
| `internal/permissions/mode_checker.go` | New ModeAwareChecker |
| `internal/permissions/mode_checker_test.go` | Tests |
| `internal/config/config.go` | Add Mode field |
| `cmd/rubichan/main.go` | Parse mode, wire checker |

## Risks

- **Security**: ModeBypass is dangerous; should require explicit confirmation
- **UX**: Users may not understand mode differences; need clear documentation
- **Backward compatibility**: Default mode (plan) must match current behavior

## Acceptance Criteria

- [ ] ModePlan auto-approves read-only tools, prompts on writes
- [ ] ModeFullAuto auto-approves all except explicitly denied
- [ ] ModeBypass auto-approves everything (with warning)
- [ ] Deny rules win in all modes
- [ ] Mode configurable via TOML and CLI flag
- [ ] Default mode (plan) preserves existing behavior
- [ ] All tests pass
