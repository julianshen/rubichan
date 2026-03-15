# Spec Amendments: Lessons from Claude Code Architecture

> **Version:** 1.0 · **Date:** 2026-03-16 · **Status:** Proposed
> **Applies to:** spec.md v1.1
> **Motivation:** Architecture comparison with Claude Code identified six capabilities that would strengthen Rubichan's design without conflicting with existing architectural decisions.

---

## Table of Contents

- [Amendment 1: Subagent/Worker System](#amendment-1-subagentworker-system)
- [Amendment 2: File Checkpoints](#amendment-2-file-checkpoints)
- [Amendment 3: Session Forking](#amendment-3-session-forking)
- [Amendment 4: User-Level Hooks](#amendment-4-user-level-hooks)
- [Amendment 5: Context Inspector](#amendment-5-context-inspector)
- [Amendment 6: Hierarchical Permissions](#amendment-6-hierarchical-permissions)
- [New Functional Requirements Summary](#new-functional-requirements-summary)
- [New ADR Summary](#new-adr-summary)
- [Roadmap Impact](#roadmap-impact)
- [Spec Integration Guide](#spec-integration-guide)

---

## Amendment 1: Subagent/Worker System

### Problem

The current spec assumes a single agent loop per session. When the agent needs to explore a large codebase (reading 50+ files), search multiple directories, or work on independent subtasks, all context accumulates in one conversation window. This causes:

1. **Context degradation** — long sessions lose early instructions as the window fills.
2. **No parallelism** — the agent processes one tool call at a time within the loop.
3. **No isolation** — a deep-dive into one area pollutes context for unrelated follow-up work.

Claude Code solves this with subagents: isolated agent instances that get their own context window, do focused work, and return only a summary to the parent.

### Proposed Changes

#### New Section: 3.3.1 Subagent System

Add after Section 3.3 (Agent Core):

The Agent Core supports spawning **subagents** — isolated agent instances that run in their own goroutine with a dedicated conversation history and context window. Subagents share the same Tool Layer, Skill Runtime, and LLM Provider as the parent but maintain completely separate conversation state.

```
┌──────────────────────────────────────────────────────┐
│  Parent Agent Loop                                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │
│  │Agent Loop │  │Conversa- │  │Context Window    │   │
│  │(main)     │  │tion Mgr  │  │Manager (main)    │   │
│  └─────┬─────┘  └──────────┘  └──────────────────┘   │
│        │                                              │
│        │ spawn                                        │
│  ┌─────▼──────────────────────────────────────────┐   │
│  │  Subagent Pool                                  │   │
│  │  ┌───────────┐ ┌───────────┐ ┌───────────┐     │   │
│  │  │ Worker 1  │ │ Worker 2  │ │ Worker N  │     │   │
│  │  │ (own ctx) │ │ (own ctx) │ │ (own ctx) │     │   │
│  │  └─────┬─────┘ └─────┬─────┘ └─────┬─────┘     │   │
│  │        │              │              │           │   │
│  │        └──────────────┼──────────────┘           │   │
│  │                       ▼                          │   │
│  │              Summary Results                     │   │
│  └──────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────┘
```

**Subagent lifecycle:**

1. Parent constructs a `SubagentRequest` with a task description, optional tool whitelist, and context budget.
2. The Subagent Pool creates a new goroutine with a fresh `ConversationManager` and `ContextWindowManager`.
3. The subagent runs its own Plan→Act→Observe loop against the same LLM provider.
4. On completion, the subagent returns a `SubagentResult` containing a text summary and optional structured data.
5. Only the summary is appended to the parent's conversation — the subagent's full conversation is discarded (or optionally persisted to SQLite for debugging).

**Concurrency control:**

- Maximum concurrent subagents configurable via `[agent] max_subagents` (default: 3).
- Subagents share the parent's rate limiter for LLM API calls.
- Subagents inherit the parent's permission grants but cannot escalate (no new approvals).
- Structured concurrency via `sourcegraph/conc` — parent blocks on `errgroup` or selects on channels.

**File write conflict resolution:**

- Subagents acquire an advisory file lock (via the CheckpointManager from Amendment 2) before any file write/patch operation.
- If two subagents attempt to write the same file, the second blocks until the first completes. This serializes conflicting writes without serializing all tool execution.
- The parent agent's file writes also participate in the same locking scheme.
- Read operations are never blocked — only writes are serialized per-file.

#### New Interface: SubagentManager

Add to Section 6 (Key Interface Definitions):

```go
type SubagentManager interface {
    // Spawn creates a new subagent with an isolated context window.
    Spawn(ctx context.Context, req SubagentRequest) (SubagentHandle, error)

    // SpawnParallel launches multiple subagents and waits for all to complete.
    SpawnParallel(ctx context.Context, reqs []SubagentRequest) ([]SubagentResult, error)

    // ActiveCount returns the number of currently running subagents.
    ActiveCount() int
}

type SubagentRequest struct {
    // Task is the natural language description sent as the subagent's initial prompt.
    Task        string

    // SystemPrompt overrides the default system prompt. If empty, inherits parent's.
    SystemPrompt string

    // Tools restricts which tools the subagent can use. Nil means all parent tools.
    Tools       []string

    // MaxTurns caps the subagent's agent loop iterations.
    MaxTurns    int

    // ContextBudget is the max tokens for this subagent's context window.
    ContextBudget int

    // Skills restricts which skills are active. Nil means inherit parent's active skills.
    Skills      []string
}

type SubagentResult struct {
    // Summary is the subagent's final text response — the only thing added to parent context.
    Summary     string

    // Structured holds optional typed data (e.g., []Finding, []FilePath).
    Structured  json.RawMessage

    // TokensUsed is the total tokens consumed by this subagent.
    TokensUsed  int

    // Error is non-nil if the subagent failed.
    Error       error
}

type SubagentHandle struct {
    ID     string
    // Cancel requests the subagent to stop. If graceful is true,
    // the subagent finishes its current tool call before stopping.
    Cancel func(graceful bool)
    Result <-chan SubagentResult
}
```

#### New FR: FR-7.1

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-7.1 | Agent Core supports spawning subagents with isolated context windows for parallel task execution | P1 |
| FR-7.2 | Subagents return only a summary to the parent, preserving parent context budget | P1 |
| FR-7.3 | Maximum concurrent subagents configurable; shared rate limiter for LLM API calls | P1 |
| FR-7.4 | Subagents inherit parent permission grants without escalation | P0 |

#### New ADR: ADR-011

### ADR-011: Subagent System for Context Isolation and Parallelism

**Status:** Proposed

**Context:** Long interactive sessions degrade as context fills with tool outputs from exploration. The agent cannot parallelize independent subtasks (e.g., searching three directories simultaneously). Claude Code demonstrated that subagents with isolated context windows solve both problems.

**Decision:** The Agent Core supports spawning subagent goroutines. Each subagent gets its own `ConversationManager` and `ContextWindowManager` but shares the parent's Tool Layer, Skill Runtime, LLM Provider, and permission grants. Only the subagent's summary text returns to the parent context.

**Rationale:**

- **Context isolation:** A subagent reading 50 files doesn't consume parent context — only the summary does.
- **Parallelism:** Independent tasks (search codebase, run tests, analyze security) can execute concurrently via `SpawnParallel`.
- **Composability:** Workflow Skills can orchestrate subagents as steps in multi-stage pipelines.
- **Goroutine-native:** Go's concurrency model makes this natural. `sourcegraph/conc` provides structured concurrency guarantees.

**Trade-offs:**

- Adds LLM API cost (each subagent makes its own calls). Mitigated by configurable budget caps and shared rate limiters.
- Subagent results are lossy (summary only). Mitigated by optional structured data field and SQLite persistence for debugging.
- Complexity in permission inheritance. Mitigated by simple rule: inherit, never escalate.

**Consequences:**

- `SubagentManager` added to Agent Core. Workflow Skills gain access via SDK.
- `[agent] max_subagents` config option. Default 3.
- Subagent conversations optionally persisted to SQLite for debugging.

---

## Amendment 2: File Checkpoints

### Problem

The current spec has no mechanism for undoing file edits within a session. If the agent makes a bad edit, the user must manually revert via git or filesystem tools. Claude Code snapshots every file before editing, enabling instant undo independent of git state. This is especially valuable when:

- The user hasn't committed recent work yet (git revert would lose their changes too).
- The agent makes multiple edits and the user wants to revert only some.
- The project isn't a git repository.

### Proposed Changes

#### New Section: 3.3.2 File Checkpoint System

Add after the new Section 3.3.1:

Before every file write or patch operation, the Agent Core captures a **checkpoint** — a snapshot of the file's current contents. Checkpoints are stored in-memory during the session and optionally persisted to a temporary directory for crash recovery.

```
┌───────────────────────────────────────────────────────┐
│  Checkpoint Stack (per session)                        │
│                                                        │
│  [Turn 5] edit main.go:42-50        ← most recent     │
│  [Turn 4] create new_file.go                          │
│  [Turn 3] edit handler.go:10-15                       │
│  [Turn 2] edit handler.go:5-8                         │
│  [Turn 1] edit go.mod                ← oldest          │
│                                                        │
│  Undo: revert to state before any checkpoint          │
│  Rewind: revert to state before checkpoint N          │
└───────────────────────────────────────────────────────┘
```

**Checkpoint lifecycle:**

1. Before `FileWrite` or `FilePatch` tool execution, the Tool Layer reads the current file contents (or records "file did not exist" for new files).
2. A `Checkpoint` struct is pushed onto the session's checkpoint stack with the file path, original contents, turn number, and timestamp.
3. On undo, the checkpoint is popped and the file is restored to its previous state. New files are deleted.
4. On rewind-to-turn-N, all checkpoints after turn N are popped in reverse order.

**Crash recovery:**

Checkpoints are written to `$TMPDIR/aiagent/checkpoints/<session-id>/` as individual files. On abnormal termination, the next session start detects orphaned checkpoint directories and offers recovery.

**Memory budget:**

In-memory checkpoint storage is capped at 100MB by default (configurable via `[agent] checkpoint_memory_budget`). When the budget is exceeded, the oldest checkpoints are evicted to the crash recovery temp directory. Files larger than 1MB are always checkpointed directly to disk rather than held in memory. This prevents memory pressure when the agent edits large files (e.g., database dumps, generated code, binary assets).

**Behavior across session forks (see Amendment 3):**

Forked sessions start with an **empty checkpoint stack**. The fork preserves conversation history but not file-edit state. Rationale: since parent and fork share the same working directory, copying checkpoints would create ambiguity about which session "owns" the undo state for a given file. The fork point effectively becomes a new baseline.

#### New Interface: CheckpointManager

Add to Section 6:

```go
type CheckpointManager interface {
    // Capture snapshots a file before modification.
    Capture(ctx context.Context, filePath string, turn int) error

    // Undo reverts the most recent checkpoint and returns the file path affected.
    Undo(ctx context.Context) (string, error)

    // RewindToTurn reverts all checkpoints after the given turn number.
    RewindToTurn(ctx context.Context, turn int) ([]string, error)

    // List returns all checkpoints in the current session.
    List() []Checkpoint

    // Cleanup removes all checkpoint data for the session.
    Cleanup() error
}

type Checkpoint struct {
    FilePath     string
    Turn         int
    Timestamp    time.Time
    Operation    string    // "create", "write", "patch"
    OriginalData []byte    // nil if file did not exist (creation checkpoint)
    Size         int64
}
```

#### Modification to Tool Layer (Section 3.4)

The `FileWrite` and `FilePatch` tools are wrapped with checkpoint capture:

```go
func (t *FileWriteTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
    // Capture checkpoint BEFORE writing
    if err := t.checkpoints.Capture(ctx, req.Path, t.currentTurn); err != nil {
        return ToolResult{}, fmt.Errorf("checkpoint capture failed: %w", err)
    }
    // Proceed with write
    return t.doWrite(ctx, req)
}
```

#### New FR: FR-7.5

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-7.5 | File contents are checkpointed before every write/patch operation | P1 |
| FR-7.6 | Users can undo the most recent edit or rewind to any previous turn | P1 |
| FR-7.7 | Checkpoints persist to temp directory for crash recovery | P2 |
| FR-7.8 | Checkpoint system works independent of git — supports non-git directories | P1 |

#### Interactive Mode TUI Integration

Add to FR-1 table:

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-1.11 | TUI supports undo (revert last edit) and rewind-to-turn via keyboard shortcut or `/undo` command | P1 |

---

## Amendment 3: Session Forking

### Problem

FR-1.8 specifies session persistence and resume. However, there is no mechanism to **branch** a conversation — trying a different approach while preserving the original session. Currently, if the user wants to explore an alternative, they lose their current conversation state.

Claude Code supports `--fork-session`: a new session ID is created with the full conversation history copied, leaving the original session untouched.

### Proposed Changes

#### Modification to Section 3.3 (Agent Core — Conversation Manager)

Add to Conversation Manager responsibilities:

- **Session forking:** The Conversation Manager can create a new session by deep-copying the current conversation history up to the current turn. The original session is preserved. The forked session gets a new ID and continues independently.

#### New Interface Methods

Add to Conversation Manager (implied in Section 3.3, formalized here):

```go
type ConversationManager interface {
    // ... existing methods ...

    // Fork creates a new session with a copy of the current conversation history.
    // The original session is unchanged. Returns the new session ID.
    Fork(ctx context.Context) (sessionID string, error)

    // Resume loads a previous session's conversation history and continues from
    // where it left off. Returns the restored turn count.
    Resume(ctx context.Context, sessionID string) (turnCount int, error)

    // ListSessions returns all sessions for the current working directory,
    // sorted by last activity.
    ListSessions(ctx context.Context) ([]SessionSummary, error)
}

type SessionSummary struct {
    ID          string
    CreatedAt   time.Time
    LastActive  time.Time
    TurnCount   int
    ForkedFrom  string    // empty if not a fork
    WorkDir     string
    Summary     string    // LLM-generated one-line summary of the conversation
}
```

#### CLI Changes

Add to Appendix A (Interactive Mode):

```bash
aiagent --continue                    # resume most recent session
aiagent --resume <session-id>         # resume specific session
aiagent --continue --fork             # fork most recent session and continue on fork
aiagent --resume <session-id> --fork  # fork specific session
aiagent session list                  # list sessions for current directory
aiagent session show <session-id>     # show session summary
aiagent session delete <session-id>   # delete a session
```

#### TUI Commands

Add to FR-1 table:

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-1.12 | `/fork` command creates a new session branching from current conversation state | P2 |
| FR-1.13 | `/sessions` command lists previous sessions with summaries | P2 |

#### New FR: FR-7.9

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-7.9 | Sessions can be forked, creating a new independent session with copied conversation history | P2 |
| FR-7.10 | Session metadata tracks fork relationships (parent session ID) | P2 |
| FR-7.11 | Sessions scoped to working directory; `session list` shows only current directory's sessions | P2 |

---

## Amendment 4: User-Level Hooks

### Problem

The current spec defines skill lifecycle hooks (Section 4.6: `on_activate`, `on_before_tool_call`, `on_after_response`, etc.). These are powerful but require creating a skill — writing a `SKILL.yaml`, implementing a Starlark or Go handler, and managing the skill lifecycle.

Claude Code offers a simpler layer: **user hooks** — shell commands that fire on tool events, configurable via a settings file. No code, no skill manifest, no sandbox. Examples: "run `go test` after every file edit", "run `golangci-lint` before commit".

This is complementary to skill hooks, not a replacement. User hooks are for personal workflow automation; skill hooks are for distributable, sandboxed extensions.

### Proposed Changes

#### New Section: 3.3.3 User Hooks

Add after the new Section 3.3.2:

User hooks are shell commands that execute in response to agent events. They are configured in the user's config file or in the project's `AGENT.md` frontmatter. Unlike skill hooks, user hooks:

- Run as plain shell commands (no sandbox).
- Are not distributable (personal or project-scoped configuration).
- Can block or modify tool execution based on exit codes.

**Hook events:**

| Event | Fires When | Hook Can |
|-------|------------|----------|
| `pre_tool` | Before any tool executes | Block execution (exit 1), modify input (stdout) |
| `post_tool` | After any tool completes | Log, trigger side effects |
| `pre_edit` | Before file write/patch | Block edit, run formatter |
| `post_edit` | After file write/patch | Run linter, run tests |
| `pre_shell` | Before shell command execution | Block dangerous commands |
| `post_response` | After agent produces a text response | Log, notify |
| `session_start` | When a new session begins | Set up environment |
| `session_end` | When a session ends | Clean up, generate reports |

**Hook configuration:**

```toml
# ~/.config/aiagent/config.toml

[[hooks]]
event = "post_edit"
pattern = "*.go"          # only trigger for Go files
command = "gofmt -w {file}"
description = "Auto-format Go files after edit"

[[hooks]]
event = "post_edit"
pattern = "*.go"
command = "go test ./..."
description = "Run tests after Go file changes"
timeout = "60s"

[[hooks]]
event = "pre_shell"
command = "echo {command} | grep -qv 'rm -rf' || exit 1"
description = "Block rm -rf commands"

[[hooks]]
event = "pre_edit"
pattern = "*.generated.*"
command = "exit 1"
description = "Prevent editing generated files"
```

**Hook execution model:**

- Hooks run synchronously in the agent's shell environment.
- `pre_*` hooks can **block** by returning exit code 1. The tool call is cancelled and the agent is informed.
- `pre_*` hooks can **modify** by writing to stdout (e.g., a pre_edit hook can reformat the proposed content).
- `post_*` hooks are fire-and-forget — exit code is logged but doesn't affect the agent.
- Template variables: `{file}`, `{tool}`, `{command}`, `{turn}`, `{session_id}`.
- Timeout defaults to 30s, configurable per hook.

**Priority vs. skill hooks:**

User hooks fire **before** skill hooks for `pre_*` events and **after** skill hooks for `post_*` events. If a user hook blocks, skill hooks are not invoked. This gives users ultimate control over their environment.

#### New Interface: ShellHookRunner

Add to Section 6:

> **Note:** Types are prefixed with `Shell` to distinguish from the existing skill lifecycle hook types (`HookEvent`, `HookResult`) defined in Section 4.6.

```go
type ShellHookRunner interface {
    // Fire executes all user hooks registered for the given event.
    // Returns a ShellHookResult indicating whether to proceed, block, or modify.
    Fire(ctx context.Context, event ShellHookEvent) (*ShellHookResult, error)

    // Register adds a hook from configuration.
    Register(hook ShellHookConfig) error

    // List returns all registered hooks.
    List() []ShellHookConfig
}

type ShellHookConfig struct {
    Event       string   // "pre_tool", "post_edit", etc.
    Pattern     string   // file glob pattern (optional)
    Command     string   // shell command to execute
    Description string
    Timeout     time.Duration
    Source      string   // "config", "agent.md", "cli"
}

type ShellHookEvent struct {
    Type    string            // matches ShellHookConfig.Event
    File    string            // affected file path (if applicable)
    Tool    string            // tool name (if applicable)
    Command string            // shell command (if pre_shell)
    Turn    int
    Data    map[string]string // additional context
}

type ShellHookResult struct {
    Action   ShellHookAction   // proceed, block, modify
    Message  string            // reason for block, or modification description
    Modified []byte            // modified content (for pre_edit modify action)
}

type ShellHookAction int

const (
    ShellHookProceed ShellHookAction = iota
    ShellHookBlock
    ShellHookModify
)
```

#### New FR: FR-7.12

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-7.12 | User-configurable shell hooks fire on agent events (pre/post tool, edit, shell, response) | P2 |
| FR-7.13 | Pre-event hooks can block tool execution via exit code | P2 |
| FR-7.14 | Hooks configured in config.toml and/or AGENT.md frontmatter | P2 |
| FR-7.15 | Hook template variables provide context ({file}, {tool}, {command}) | P2 |

#### AGENT.md Hook Configuration

Add to Section 3.9 (Config & Storage, under Project Rules — `AGENT.md`):

```markdown
---
hooks:
  - event: post_edit
    pattern: "*.go"
    command: "gofmt -w {file}"
  - event: post_edit
    pattern: "*.go"
    command: "go vet ./..."
---

# Project Instructions
...
```

**Security: Project hooks require user trust approval.**

Hooks defined in `AGENT.md` are project-level and committed to git. Since a malicious repository could define hooks that execute arbitrary commands on any developer who clones the repo, project-level hooks are **not trusted by default**. The first time the agent encounters `AGENT.md` hooks in a project, it presents them to the user for approval:

```
This project defines shell hooks in AGENT.md:
  1. post_edit *.go → gofmt -w {file}
  2. post_edit *.go → go vet ./...

Trust these project hooks? [y]es / [n]o / [r]eview
```

Trust decisions are stored in SQLite per-project (keyed by project path + hook content hash). If the hook content changes, re-approval is required.

Config option to control default behavior:

```toml
[hooks]
trust_project_hooks = false    # default: require approval
# Set to true only if you control all repos you work with
```

Only hooks in the user's own `config.toml` run without approval — these are self-authored by definition.

---

## Amendment 5: Context Inspector

### Problem

The spec's Context Window Manager (Section 3.3) tracks token usage internally, but provides no user-facing visibility. Users have no way to understand:

- How much context budget remains.
- What's consuming context (conversation history vs. system prompt vs. skill prompts vs. tool outputs).
- Whether they should start a new session or compact.

Claude Code provides `/context` to inspect usage and `/compact` with focus directives for selective summarization.

### Proposed Changes

#### New TUI Command: `/context`

Add to FR-1 table:

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-1.14 | `/context` command displays context window usage breakdown by category | P1 |
| FR-1.15 | `/compact [focus]` command triggers manual compaction with optional focus directive | P2 |

#### Context Window Manager Enhancements (Section 3.3)

Extend the Context Window Manager's responsibilities:

**Context budget breakdown:**

The Context Window Manager tracks token usage by category and exposes a breakdown:

```go
type ContextBudget struct {
    Total          int   // max tokens for the model
    Used           int   // currently consumed
    Remaining      int   // available

    // Breakdown by category
    SystemPrompt   int   // static instructions + AGENT.md
    SkillPrompts   int   // active skill prompt injections
    SkillTools     int   // tool definitions from skills
    BuiltinTools   int   // built-in tool definitions
    Conversation   int   // user messages + assistant responses
    ToolOutputs    int   // results from tool calls
    MCPTools       int   // MCP server tool definitions
}
```

**Display format (TUI):**

```
Context Usage: 42,150 / 100,000 tokens (42%)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  System prompt      2,100 (  2%)  ██
  Skill prompts      3,400 (  3%)  ███
  Tool definitions   4,200 (  4%)  ████
  Conversation      18,450 ( 18%)  ██████████████████
  Tool outputs      14,000 ( 14%)  ██████████████
  Remaining         57,850 ( 58%)  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

**Compaction strategy:**

When context usage exceeds a configurable threshold (default: 80%), the Context Window Manager triggers automatic compaction:

1. **Phase 1 — Clear tool outputs:** Remove raw output from older tool calls, keeping only summaries.
2. **Phase 2 — Summarize conversation:** Replace older conversation turns with LLM-generated summaries.
3. **Phase 3 — Skill prompt trimming:** If skills have `context_files`, unload non-essential ones.

The `/compact` command triggers manual compaction. An optional focus directive tells the summarizer what to preserve:

```
/compact focus on the authentication changes
```

This passes the focus string to the LLM summarizer, which weighs conversation segments about authentication more heavily when deciding what to preserve.

#### New Interface Methods

Add to Context Window Manager interface:

```go
type ContextWindowManager interface {
    // ... existing methods ...

    // Budget returns the current context usage breakdown.
    Budget() ContextBudget

    // Compact triggers manual compaction. Focus directs the summarizer
    // on what conversation topics to preserve.
    Compact(ctx context.Context, focus string) error

    // SetCompactionThreshold sets the auto-compaction trigger percentage.
    SetCompactionThreshold(percent float64)
}
```

#### Configuration

Add to Appendix B (config.toml):

```toml
[agent]
# ... existing fields ...
compaction_threshold = 0.8     # auto-compact at 80% context usage
compaction_strategy = "smart"  # "smart" (phased), "aggressive", "off"
```

---

## Amendment 6: Hierarchical Permissions

### Problem

The current spec has a single `ApprovalFunc` (Section 6) injected by mode. Interactive mode prompts the user; headless mode checks a config list. There is no concept of:

- **Organization-wide policies** that apply to all team members (e.g., "never allow `rm -rf /`").
- **Project-level defaults** that apply to anyone working on this repo.
- **User-level preferences** that reflect personal trust levels.

Claude Code implements a cascading settings model: Organization → Project → User, where each level can allow, deny, or defer to the next level. This prevents individual users from overriding org security policies while still allowing personal customization.

### Proposed Changes

#### Modified Section: 3.9 Config & Storage — Permissions

Replace the simple `ApprovalFunc` with a hierarchical permission resolver:

**Permission levels (highest to lowest priority):**

| Level | Location | Scope | Who Sets It |
|-------|----------|-------|-------------|
| Organization | `~/.config/aiagent/org-policy.toml` (distributed via fleet management) | All users in org | Security team / Admins |
| Project | `.agent/permissions.toml` (committed to repo) | All contributors to this project | Tech lead / Project owner |
| User | `~/.config/aiagent/config.toml` `[permissions]` section | This user only | Individual developer |
| Session | CLI flags or `/allow` TUI command | Current session only | Individual developer |

**Resolution order:**

```
1. Check Org policy — if explicit deny → DENY (cannot override)
2. Check Org policy — if explicit allow → ALLOW
3. Check Project policy — if explicit deny → DENY (user cannot override)
4. Check Project policy — if explicit allow → ALLOW
5. Check User config — if explicit allow → ALLOW
6. Check Session grants — if explicit allow → ALLOW
7. Default → PROMPT (ask user in interactive) or DENY (headless)
```

**Policy format:**

```toml
# .agent/permissions.toml (project-level)

[tools]
# Tools that are always allowed for this project
allow = ["file_read", "file_write", "code_search", "ast_search", "git_status", "git_diff"]

# Tools that require explicit approval every time
prompt = ["shell_exec", "git_commit", "git_push"]

# Tools that are never allowed (even if user tries to approve)
deny = []

[shell]
# Shell commands/patterns always allowed
allow_commands = ["go test", "go build", "go vet", "golangci-lint", "gofmt"]

# Shell commands/patterns always blocked
deny_commands = ["rm -rf /", "rm -rf ~", "> /dev/sda"]

# Patterns that require approval
prompt_patterns = ["curl", "wget", "ssh"]

[files]
# File patterns the agent can always edit
allow_patterns = ["*.go", "*.md", "*.yaml", "*.toml"]

# File patterns the agent must never edit
deny_patterns = [".env", "*.pem", "*.key", "credentials.*"]

# File patterns requiring per-edit approval
prompt_patterns = ["go.mod", "go.sum", "Makefile", "Dockerfile"]

[skills]
# Skills auto-approved for this project
auto_approve = ["core-tools", "git", "apple-dev"]
```

```toml
# ~/.config/aiagent/org-policy.toml (organization-level)

[shell]
# Hard deny — no user or project can override
deny_commands = ["rm -rf /", ":(){ :|:& };:", "mkfs", "dd if=/dev/zero"]

[files]
# Organization-wide protected files
deny_patterns = [".env*", "*.pem", "*.key", "*.p12", "credentials*", "secrets*"]

[skills]
# Only allow verified registry skills
require_verified = true

# Block specific skills org-wide
deny = ["crypto-miner-disguised-as-formatter"]
```

#### New Interface: PermissionResolver

Replace `ApprovalFunc` in Section 6 with:

```go
type PermissionResolver interface {
    // Check evaluates whether an action is allowed at the current permission level.
    Check(ctx context.Context, action PermissionAction) (PermissionDecision, error)

    // Grant records a session-level permission grant.
    Grant(ctx context.Context, action PermissionAction) error

    // Revoke removes a session-level or user-level permission grant.
    Revoke(ctx context.Context, action PermissionAction) error

    // Explain returns a human-readable explanation of why an action was
    // allowed/denied and which policy level made the decision.
    Explain(ctx context.Context, action PermissionAction) (string, error)
}

type PermissionType int

const (
    PermissionTypeTool  PermissionType = iota
    PermissionTypeShell
    PermissionTypeFile
    PermissionTypeSkill
)

type PermissionAction struct {
    Type     PermissionType   // tool, shell, file, skill
    Tool     string           // tool name (if Type == PermissionTypeTool)
    Command  string           // shell command (if Type == PermissionTypeShell)
    FilePath string           // file path (if Type == PermissionTypeFile)
    Skill    string           // skill name (if Type == PermissionTypeSkill)
}

type PermissionDecision struct {
    Action   PermissionVerdict // allow, deny, prompt
    Level    string            // "org", "project", "user", "session", "default"
    Reason   string            // human-readable reason
}

type PermissionVerdict int

const (
    PermissionAllow PermissionVerdict = iota
    PermissionDeny
    PermissionPrompt
)
```

#### Migration from ApprovalFunc

The existing `ApprovalFunc` becomes a thin adapter over `PermissionResolver`:

```go
func approvalFromResolver(resolver PermissionResolver) ApprovalFunc {
    return func(ctx context.Context, action ToolAction) (bool, error) {
        decision, err := resolver.Check(ctx, PermissionAction{
            Type: PermissionTypeTool,
            Tool: action.ToolName,
        })
        if err != nil {
            return false, err
        }
        switch decision.Action {
        case PermissionAllow:
            return true, nil
        case PermissionDeny:
            return false, nil
        case PermissionPrompt:
            // delegate to mode-specific prompt (TUI or headless)
            return action.PromptUser(ctx, decision.Reason)
        }
        return false, nil
    }
}
```

This preserves backward compatibility — existing mode adapters continue to work while gaining hierarchical policy support.

#### Interaction with ADR-009 (Skill Permission Model)

ADR-009 defines a skill-specific permission model: skills declare required permissions in `SKILL.yaml`, and users approve on first use. The hierarchical permission system (ADR-012) operates as an **outer gate** around ADR-009's skill permissions:

1. **Hierarchical check first:** The `PermissionResolver` evaluates Org → Project → User → Session policies. If the hierarchical system returns `deny`, the action is blocked regardless of skill approval state.
2. **Skill permission check second:** If the hierarchical system returns `allow` or `prompt`, the ADR-009 skill permission model applies as an additional gate. A skill still needs its declared permissions approved by the user.
3. **Example flow:** A skill declares `permissions: [shell:exec]`. The org policy allows `shell:exec` for this project. The skill has not been approved by the user yet → user is prompted (per ADR-009). Once approved, subsequent `shell:exec` calls by this skill pass both gates automatically.

This means org deny always wins, project allow doesn't bypass skill approval, and skill approval doesn't bypass org/project deny. The two systems are complementary, not competing.

#### New FR: FR-7.16

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-7.16 | Permission policies cascade: Organization → Project → User → Session | P1 |
| FR-7.17 | Organization policies cannot be overridden by lower levels (hard deny) | P1 |
| FR-7.18 | Project-level permissions configured in `.agent/permissions.toml` (committed to repo) | P1 |
| FR-7.19 | Permission decisions include explanation of which policy level decided and why | P2 |
| FR-7.20 | Shell commands, file paths, and tool names all support glob pattern matching in policies | P2 |

#### New ADR: ADR-012

### ADR-012: Hierarchical Permission Model

**Status:** Proposed

**Context:** The original `ApprovalFunc` is a single-layer callback. In team environments, there's no way for security teams to enforce organization-wide policies, or for project leads to set project defaults. Individual developers may override safety controls that should be mandatory.

**Decision:** Replace `ApprovalFunc` with a `PermissionResolver` that cascades through four levels: Organization → Project → User → Session. Higher levels can set hard denies that lower levels cannot override. `ApprovalFunc` remains as a compatibility adapter.

**Rationale:**

- **Security posture:** Org policies prevent dangerous commands across all projects. No single developer can `rm -rf /` regardless of personal settings.
- **Project conventions:** Projects can pre-approve safe tools and block dangerous patterns without each contributor configuring individually.
- **User autonomy:** Within org/project constraints, users control their own experience.
- **Auditability:** Every decision includes which level made it and why.

**Trade-offs:**

- More complex than single `ApprovalFunc`. Mitigated by adapter pattern — simple cases still work simply.
- Org policy distribution is out of scope (fleet management, not agent responsibility). We define the format, not the delivery.
- Policy conflicts require clear precedence rules. Resolved by strict level ordering.

**Consequences:**

- `.agent/permissions.toml` added to project structure.
- `~/.config/aiagent/org-policy.toml` format defined.
- `PermissionResolver` replaces `ApprovalFunc` as the primary interface.
- Existing `ApprovalFunc` preserved as adapter for backward compatibility.

---

## New Functional Requirements Summary

#### FR-7: Agent Platform Enhancements

Cross-cutting capabilities that strengthen the agent platform: subagent parallelism, file safety, session management, workflow automation, context visibility, and permission governance. These requirements apply across all execution modes unless otherwise noted.

| ID | Requirement | Priority | Amendment |
|----|-------------|----------|-----------|
| FR-7.1 | Subagent spawning with isolated context windows | P1 | 1 |
| FR-7.2 | Subagent summary-only returns to parent context | P1 | 1 |
| FR-7.3 | Configurable max concurrent subagents with shared rate limiter | P1 | 1 |
| FR-7.4 | Subagents inherit permissions without escalation | P0 | 1 |
| FR-7.5 | File checkpoints before every write/patch | P1 | 2 |
| FR-7.6 | Undo last edit or rewind to any previous turn | P1 | 2 |
| FR-7.7 | Checkpoint crash recovery via temp directory | P2 | 2 |
| FR-7.8 | Checkpoints work without git | P1 | 2 |
| FR-7.9 | Session forking with copied conversation history | P2 | 3 |
| FR-7.10 | Fork metadata tracks parent session ID | P2 | 3 |
| FR-7.11 | Sessions scoped to working directory | P2 | 3 |
| FR-7.12 | User-configurable shell hooks on agent events | P2 | 4 |
| FR-7.13 | Pre-event hooks can block via exit code | P2 | 4 |
| FR-7.14 | Hooks configured in config.toml and AGENT.md | P2 | 4 |
| FR-7.15 | Hook template variables for context | P2 | 4 |
| FR-7.16 | Hierarchical permission cascade (Org → Project → User → Session) | P1 | 6 |
| FR-7.17 | Org-level hard deny cannot be overridden | P1 | 6 |
| FR-7.18 | Project permissions in `.agent/permissions.toml` | P1 | 6 |
| FR-7.19 | Permission decisions include level explanation | P2 | 6 |
| FR-7.20 | Glob pattern matching in permission policies | P2 | 6 |

Additional FR-1 additions (Interactive Mode):

| ID | Requirement | Priority | Amendment |
|----|-------------|----------|-----------|
| FR-1.11 | Undo/rewind via keyboard shortcut or `/undo` | P1 | 2 |
| FR-1.12 | `/fork` command for session branching | P2 | 3 |
| FR-1.13 | `/sessions` command for session listing | P2 | 3 |
| FR-1.14 | `/context` displays context budget breakdown | P1 | 5 |
| FR-1.15 | `/compact [focus]` triggers manual compaction | P2 | 5 |

## New ADR Summary

| ADR | Title | Status |
|-----|-------|--------|
| ADR-011 | Subagent System for Context Isolation and Parallelism | Proposed |
| ADR-012 | Hierarchical Permission Model | Proposed |

## Roadmap Impact

These amendments are scoped as a new milestone:

### Milestone 5.5: Agent Platform Enhancements (Weeks TBD)

| Phase | Features | Effort Estimate | Dependencies |
|-------|----------|-----------------|--------------|
| Phase 1 | File Checkpoints (Amendment 2) | 3–4 days | None — can start immediately |
| Phase 2 | Context Inspector (Amendment 5) | 2–3 days | None — extends existing ContextWindowManager |
| Phase 3 | Hierarchical Permissions (Amendment 6) | 4–5 days | None — replaces ApprovalFunc |
| Phase 4 | Session Forking (Amendment 3) | 4–5 days | Requires SQLite session storage (already exists); includes CLI flags, TUI commands, session summaries |
| Phase 5 | User Hooks (Amendment 4) | 3–4 days | Benefits from Hierarchical Permissions (hooks respect policies) |
| Phase 6 | Subagent System (Amendment 1) | 5–7 days | Requires Phase 1 (file lock coordination), Phase 3 (permission inheritance), Phase 2 (context budget per subagent) |

**Total estimated effort:** 21–28 days (5–7 weeks with testing and integration).

**Phasing rationale:**
- Checkpoints and Context Inspector are independent, low-risk, high-value — start there.
- Hierarchical Permissions is a foundational change that User Hooks and Subagents benefit from.
- Subagents are the most complex and depend on stable context management and permissions — build last.

---

---

## Spec Integration Guide

When integrating these amendments into `spec.md`, use this mapping to locate exact insertion points:

| Amendment | Target Section in spec.md | Action |
|-----------|--------------------------|--------|
| 1 (Subagents) | After §3.3 Agent Core (line ~319) | Insert new §3.3.1 Subagent System |
| 1 (Subagents) | §6 Key Interface Definitions (line ~1875) | Add `SubagentManager` interface after existing interfaces |
| 1 (Subagents) | §8 Architecture Decision Records (after ADR-010, line ~2230) | Add ADR-011 |
| 2 (Checkpoints) | After new §3.3.1 | Insert new §3.3.2 File Checkpoint System |
| 2 (Checkpoints) | §3.4 Tool Layer (line ~320) | Add checkpoint wrapping note to FileWrite/FilePatch tools |
| 2 (Checkpoints) | §6 Key Interface Definitions | Add `CheckpointManager` interface |
| 3 (Session Fork) | §3.3 Agent Core, Conversation Manager bullet (line ~315) | Extend with fork/resume methods |
| 3 (Session Fork) | §6 Key Interface Definitions | Add `ConversationManager` interface (formalizes what §3.3 describes narratively) |
| 3 (Session Fork) | Appendix A CLI Command Reference, Interactive Mode (line ~2348) | Add `--continue`, `--resume`, `--fork`, `session` subcommands |
| 4 (User Hooks) | After new §3.3.2 | Insert new §3.3.3 User Hooks |
| 4 (User Hooks) | §3.9 Config & Storage (line ~423) | Add AGENT.md hook configuration with trust gate |
| 4 (User Hooks) | §6 Key Interface Definitions | Add `ShellHookRunner` interface |
| 5 (Context Inspector) | §3.3 Agent Core, Context Window Manager bullet (line ~316) | Extend with budget breakdown, compaction strategy |
| 5 (Context Inspector) | §6 Key Interface Definitions | Extend `ContextWindowManager` interface |
| 5 (Context Inspector) | Appendix B Configuration Reference (line ~2427) | Add compaction config options |
| 6 (Permissions) | §3.9 Config & Storage (line ~423) | Add hierarchical permission levels table and resolution order |
| 6 (Permissions) | §6 Key Interface Definitions, replace ApprovalFunc (line ~1954) | Replace with `PermissionResolver`, keep `ApprovalFunc` as adapter |
| 6 (Permissions) | §8 Architecture Decision Records (after ADR-011) | Add ADR-012 |
| All | §2.2 Functional Requirements (after FR-6, line ~196) | Add FR-7 section header and FR-7.1–FR-7.20 |
| All | §2.2 FR-1 table (line ~103) | Add FR-1.11 through FR-1.15 |
| All | Appendix B Configuration Reference | Add `max_subagents`, `checkpoint_memory_budget`, `compaction_threshold`, `hooks` config |
| All | §10 Risk Assessment (line ~2326) | Add rows for subagent cost explosion, checkpoint memory pressure, hook security |

**Note on §3.3 subsections:** The original §3.3 has no numbered subsections. Adding §3.3.1–§3.3.3 does **not** require renumbering §3.4–§3.9, as these are peer sections at the same level.

**Note on MCPTools in ContextBudget:** The `MCPTools` field in `ContextBudget` is a new tracking category not present in the current Context Window Manager description. When integrating Amendment 5, note this explicitly as a new responsibility for the Context Window Manager.

---

*This document is a proposed amendment. To apply, each amendment should be integrated into spec.md via a separate structural commit after team review.*
