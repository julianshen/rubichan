# Skill System Enhancements — Design Document

> **Date:** 2026-03-07 · **Status:** Implemented
> **Motivation:** Learnings from Claude Code's skill system architecture (DeepWiki analysis)

---

## Problem Statement

Rubichan had a solid skill foundation: multi-source discovery, prompt/tool/workflow/security-rule types, `SKILL.md` instruction skills, permissions, and a runtime with context budgeting. This milestone closed the biggest remaining gaps around ergonomics, activation quality, subagent semantics, and authoring UX.

Compared to Claude Code's skill system, the current implementation is weaker in five areas:

| Gap | Current Behavior | Impact |
|---|---|---|
| Discovery model | Fixed user/project dirs, one-level scan, explicit names | Hard to compose skill packs or develop local bundles |
| Instruction skill contract | `SKILL.md` is prompt-only metadata + body | Markdown skills cannot express richer behavior cleanly |
| Activation quality | Trigger evaluation is boolean OR | Too many skills activate, and prompt budget is spent by source priority instead of relevance |
| Subagent semantics | No explicit skill inheritance / isolation model | Parent and child agent behavior is harder to reason about |
| Dev UX | No `skill why`, no trace mode, no watch mode | Hard to debug activation and author skills efficiently |

There are also two correctness issues that should be fixed before expanding the architecture:

1. `skill add` writes to `.agent/skills`, but runtime loads project skills from `.rubichan/skills`.
2. CLI flows still assume `SKILL.yaml`, even though runtime discovery supports instruction-only `SKILL.md`.

## Goals

1. Make skills easier to discover, inspect, and author.
2. Improve activation quality so only relevant skills consume context.
3. Expand `SKILL.md` into a richer, structured skill format without forcing a backend.
4. Define explicit skill behavior for subagents.
5. Preserve backward compatibility for existing `SKILL.yaml` and `SKILL.md` skills.

## Non-Goals

- Replacing the existing runtime/backends model
- Removing `SKILL.yaml` manifests
- Reworking permissions or sandboxing in this project
- Building a remote marketplace or registry UX in this milestone

## Architecture

### 1. Discovery Sources

Extend skill discovery from:

- built-in
- user dir
- project dir
- MCP-derived synthetic skills
- explicit `--skills`

to:

- built-in
- configured skill-pack dirs
- user dir
- project dir
- external registered dirs (`skill add-dir`)
- MCP-derived synthetic skills
- explicit invocations

Each discovered skill keeps:

```go
type SkillProvenance struct {
    Name       string
    Source     Source
    RootDir    string
    SkillDir   string
    Discovery  string // builtin, recursive-scan, add-dir, mcp, explicit
    OverriddenBy string
}
```

This provenance is used by:

- `skill list --verbose`
- `skill info`
- `skill why`
- activation trace logs

### 2. Recursive Skill Packs

Replace one-level directory scans with recursive discovery under each configured root. A skill is any directory containing either:

- `SKILL.yaml`
- `SKILL.md`

Precedence remains deterministic:

```text
builtin > explicit > add-dir > user > project > configured-pack > mcp
```

Within the same source class, the first root wins and duplicate names are reported as warnings.

Rationale:

- makes nested packs like `skills/frontend/react/` practical
- enables curated skill collections without copying files
- supports a Claude-like "install a directory of skills" workflow

### 3. Richer Instruction Skills

`SKILL.md` should evolve from prompt-only metadata into a lightweight structured skill format.

#### Current

```yaml
name:
version:
description:
types:
triggers:
permissions:
```

#### Proposed Additional Fields

```yaml
priority: 0
tools_allow: ["read_file", "search"]
tools_deny: ["bash"]
model_hint: "gpt-5"
references:
  - path: references/react.md
    when: "working on React UI"
commands:
  - name: react-audit
    description: Audit a React component
agents:
  - name: react-reviewer
    description: Review React changes
subagent_policy:
  inherit_skills: true
  extra_skills: ["react-best-practices"]
  disable_skills: ["generic-frontend"]
```

Rules:

- `SKILL.md` may still default to prompt-only
- unsupported fields fail validation clearly
- command and agent definitions from markdown skills remain declarative only
- tool-providing behavior still requires a backend

### 4. Relevance-Based Activation

Current activation is boolean matching:

- files
- keywords
- languages
- modes
- explicit source

Proposed activation becomes score-based:

```go
type ActivationScore struct {
    Explicit        int
    Mode            int
    CurrentPath     int
    ProjectFiles    int
    Languages       int
    UserMessage     int
    RecentTools     int
    PriorityHint    int
    DependencyBoost int
    Total           int
}
```

Activation flow:

1. Discovery returns all candidate skills
2. Each skill gets an `ActivationScore`
3. Skills below threshold are skipped
4. Remaining skills are sorted by score
5. Prompt fragments import in that order
6. Source priority is only a tie-breaker, not the primary selector

Benefits:

- fewer irrelevant prompt fragments
- more predictable context allocation
- easier debugging via `skill why`

### 5. Budgeting by Relevance

Current budget enforcement truncates by source priority. Replace this with:

- activation score first
- explicit source override second
- source priority as tie-breaker
- optional per-skill `priority` hint from manifest/frontmatter

The runtime should expose both:

```go
GetBudgetedPromptFragments()
GetActivationReport()
```

The activation report contains:

- matched triggers
- score breakdown
- import decision
- truncation decision

### 6. Subagent Skill Model

Define subagent behavior explicitly.

#### Default Policy

- Child agent inherits parent active skill indexes
- Child imports full bodies only for skills relevant to the child prompt
- Child may receive additional skills from the selected agent definition
- Child may opt out of inheritance

#### Agent Definition Extensions

```go
type AgentDefinition struct {
    ...
    InheritSkills bool
    ExtraSkills   []string
    DisableSkills []string
}
```

This keeps subagents isolated while still leveraging discovered skill context.

### 7. Developer Experience

Add four new workflows:

1. `rubichan skill why <name>`
2. `rubichan skill trace`
3. `rubichan skill dev <path>`
4. `rubichan skill add-dir <path>`

`skill trace` should show:

- discovered skills
- overridden duplicates
- activation scores
- loaded prompt fragments
- truncated fragments
- permissions requested

`skill dev` should:

- watch files under a skill directory
- re-parse on change
- print validation errors immediately

## CLI Changes

### New Commands

```text
rubichan skill add-dir <path>
rubichan skill why <name>
rubichan skill trace
rubichan skill dev <path>
rubichan skill lint <path>
```

### Updated Commands

- `skill info` supports `SKILL.md`
- `skill test` supports `SKILL.md`
- `skill install` supports instruction-only skills
- `skill create` supports `--type=instruction`
- `skill add` writes to the same project path the loader reads

## Data Model Changes

### Config

```toml
[skills]
dirs = ["/opt/rubichan-skills", "/Users/me/dev/skills"]
activation_threshold = 25
trace = false
```

### Store

The existing permission store can remain unchanged for this milestone. No DB migration is required unless we later persist registered external dirs or activation history in SQLite rather than config.

## Integration Impact

### Likely Touched Files

| Area | Files |
|---|---|
| Discovery | `internal/skills/loader.go`, `internal/skills/types.go` |
| Instruction manifests | `internal/skills/manifest.go` |
| Activation | `internal/skills/triggers.go`, `internal/skills/runtime.go`, `internal/skills/integration.go` |
| Subagents | `internal/agent/subagent.go`, `internal/tools/task.go`, agent-definition types |
| CLI | `cmd/rubichan/skill.go`, `cmd/rubichan/main.go` |
| Config | `internal/config/config.go` |

## Migration Strategy

1. Fix correctness issues first
2. Add recursive/configured discovery without changing existing defaults
3. Add scoring alongside current boolean triggers
4. Switch budget ordering to activation score after tests pass
5. Expand `SKILL.md` parser with additive fields
6. Add subagent inheritance controls
7. Add watch/trace/lint tooling

## Risks

| Risk | Mitigation |
|---|---|
| Overcomplicating activation logic | Keep score breakdown explicit and testable |
| Breaking existing skills | Make all new fields optional and additive |
| Too much prompt churn | Gate imports behind activation threshold + trace output |
| Recursive discovery surprises | Make precedence deterministic and visible in CLI |
| Subagent unpredictability | Add explicit inheritance flags and tests |

## Success Criteria

- Instruction-only skills are first-class in CLI and runtime
- Skill packs can be registered and discovered recursively
- Activation decisions are explainable with score breakdowns
- Prompt budgeting prefers relevant skills over merely high-priority sources
- Subagent skill behavior is explicit and test-covered
- Skill authors have a usable debug/watch/lint loop
