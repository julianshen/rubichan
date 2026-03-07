# Git Worktree Support — Design Document

> **Date:** 2026-03-07 · **Status:** Approved

## Motivation

Rubichan needs git worktree support for three use cases:

1. **Parallel subagent isolation** — Subagents working on independent tasks need their own filesystem to avoid file conflicts.
2. **User-facing session isolation** — Users can experiment in a worktree without touching their main checkout (`rubichan --worktree feature-auth`).
3. **Headless batch mode** — Multiple headless agents run in parallel, each producing a separate PR.

### Prior Art

- **Claude Code** stores worktrees at `<repo>/.claude/worktrees/<name>/` with named branches (`worktree-<name>`). Auto-cleans on no changes. Supports `WorktreeCreate`/`WorktreeRemove` hooks for non-git VCS. Subagent isolation via `isolation: worktree` in agent definitions. `/batch` command orchestrates parallel agents in worktrees.
- **Codex** stores worktrees at `~/.codex/worktrees/<id>/<project>` using detached HEAD. Pairs worktrees with OS-level sandboxing (Landlock/Seatbelt). Configurable retention limit. Supports handoff between local and worktree modes.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage location | `<repo>/.rubichan/worktrees/<name>/` | Co-located with project, easy to discover |
| Branch strategy | Named branches `worktree-<name>` | Predictable, easy to push/PR |
| Cleanup | Auto-cleanup on no changes + retention limit | Prevents sprawl without losing work |
| Extensibility | Lifecycle hooks from the start | Supports non-git VCS and custom setup |
| Architecture | Worktree as Agent Option (Approach A) | Minimal coupling, agent core stays clean |

## Architecture

The worktree manager is a standalone component in `internal/worktree/`. Callers (CLI, subagent spawner, headless batch) use it to create worktrees, then pass the path to the agent via `WithWorkingDir(path)`.

```
CLI (--worktree flag)  ─┐
Subagent spawner       ─┤──▶ WorktreeManager.Create() ──▶ Agent(WithWorkingDir(path))
Headless batch         ─┘         │
                            WorktreeManager.Cleanup()
```

The agent core does not know about worktrees — it just receives a working directory. This matches the existing pattern where tools already take `rootDir`/`workDir` parameters.

## Components

### 1. WorktreeManager (`internal/worktree/`)

```go
type Manager struct {
    repoRoot string
    config   Config
    hooks    HookDispatcher
}

type Config struct {
    MaxWorktrees int    // default: 5
    BaseBranch   string // default: "" (auto-detect from origin/HEAD)
}

type Worktree struct {
    Name       string
    Path       string    // <repoRoot>/.rubichan/worktrees/<name>/
    Branch     string    // worktree-<name>
    CreatedAt  time.Time
    HasChanges bool
}
```

**Operations:**

- `Create(ctx, name)` — Runs `git worktree add -b worktree-<name> .rubichan/worktrees/<name>/ <base-branch>`. Fires `worktree.create` hook before the git command. Runs `Cleanup()` after to enforce retention limit.
- `Remove(ctx, name)` — Runs `git worktree remove` + `git branch -D worktree-<name>`. Fires `worktree.remove` hook before cleanup.
- `List(ctx)` — Returns all managed worktrees with status (clean/modified).
- `Cleanup(ctx)` — Auto-removes worktrees with no changes. If count exceeds `MaxWorktrees`, removes oldest idle worktrees.
- `HasChanges(ctx, name)` — Checks `git status --porcelain` and `git log <base>..HEAD` in the worktree.

**Concurrency:** File lock at `.rubichan/worktrees/.lock` prevents races when multiple agents create/remove worktrees simultaneously.

### 2. Agent Core Integration

Minimal change — a new `WithWorkingDir(dir string)` agent option:

```go
func WithWorkingDir(dir string) AgentOption {
    return func(a *Agent) { a.workingDir = dir }
}
```

All `os.Getwd()` calls in initialization check `a.workingDir` first, fall back to `os.Getwd()` if empty. The working directory propagates to:

- `FileTool(rootDir)`, `ShellTool(workDir)` — already take directory params
- `GitRunner(workDir)` — already takes directory param
- Skill discovery path (`.rubichan/skills/`)
- Session creation (`WorkingDir` field)
- Memory loading (keyed by normalized path)

**No changes needed** in `internal/tools/file.go`, `internal/tools/shell.go`, `internal/store/store.go`, or `internal/integrations/git_runner.go` — they already accept directory parameters.

### 3. CLI Integration

**New `--worktree` flag:**

```
rubichan --worktree feature-auth       # Interactive mode in a worktree
rubichan --headless --worktree ci-fix  # Headless mode in a worktree
```

Behavior:
1. Resolve repo root via `git rev-parse --show-toplevel`
2. `WorktreeManager.Create(name)` — creates if new, reuses if exists
3. Pass worktree path as `WithWorkingDir(path)` to the agent
4. On session end: if no changes, auto-remove; if changes exist, print preservation message

**New `worktree` subcommand group:**

```
rubichan worktree list                 # List all managed worktrees
rubichan worktree remove <name>        # Manually remove a worktree
rubichan worktree cleanup              # Force GC (enforce retention limit)
```

### 4. Subagent Integration

`AgentDefinition` gains an `Isolation` field:

```go
type AgentDefinition struct {
    Name        string
    Description string
    // ... existing fields ...
    Isolation   string // "", "worktree"
}
```

When `Isolation: "worktree"`:
1. Parent calls `WorktreeManager.Create(subagent-<taskid>)`
2. Spawns subagent with `WithWorkingDir(worktreePath)`
3. On completion: auto-cleanup if no changes, report path/branch if changes exist

### 5. Headless Batch Mode

```
rubichan --headless --batch tasks.json
```

Orchestrator:
1. Creates N worktrees (`batch-<task-id>`)
2. Runs N headless agents in parallel via goroutines
3. Collects results (commits, diffs, errors)
4. Reports summary, optionally creates PRs

### 6. Lifecycle Hooks

Two new hook phases:

```go
const (
    HookWorktreeCreate HookPhase = "worktree.create"
    HookWorktreeRemove HookPhase = "worktree.remove"
)
```

**Payloads:**

```go
type WorktreeCreateEvent struct {
    Name       string `json:"name"`
    Path       string `json:"path"`
    BaseBranch string `json:"base_branch"`
    RepoRoot   string `json:"repo_root"`
}

type WorktreeRemoveEvent struct {
    Name     string `json:"name"`
    Path     string `json:"path"`
    RepoRoot string `json:"repo_root"`
}
```

- `worktree.create` fires before the git command. If a hook handles creation, the default `git worktree add` is skipped.
- `worktree.remove` fires before git cleanup. Same override pattern.
- Hook timeouts: 30s for create, 15s for remove (configurable).

### 7. Configuration

New `[worktree]` section in `config.toml`:

```toml
[worktree]
max_count = 5
base_branch = ""
auto_cleanup = true
cleanup_timeout = "30s"
```

### 8. Directory Structure

```
.rubichan/
├── worktrees/
│   ├── .lock
│   ├── feature-auth/
│   │   ├── .git          # file pointing to main .git
│   │   ├── AGENT.md
│   │   ├── internal/
│   │   └── ...
│   └── batch-task-01/
│       └── ...
└── skills/
```

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `internal/worktree/manager.go` | Create | WorktreeManager implementation |
| `internal/worktree/manager_test.go` | Create | Tests for WorktreeManager |
| `internal/worktree/config.go` | Create | Worktree config types |
| `internal/agent/agent.go` | Modify | Add `WithWorkingDir` option, thread through init |
| `internal/agent/agentdef.go` | Modify | Add `Isolation` field to `AgentDefinition` |
| `internal/skills/types.go` | Modify | Add `HookWorktreeCreate`, `HookWorktreeRemove` phases |
| `internal/config/config.go` | Modify | Add `[worktree]` config section |
| `cmd/rubichan/main.go` | Modify | Add `--worktree` flag, `worktree` subcommands |
