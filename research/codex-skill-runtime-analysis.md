# Research: Codex Skill Runtime — Lessons for Rubichan

**Date**: 2026-03-01
**Sources**: DeepWiki (openai/codex, openai/skills), OpenAI Developer Docs, blog.fsck.com
**Scope**: Architectural comparison of Codex skill runtime vs. Rubichan spec, with actionable enhancement proposals

---

## 1. Executive Summary

OpenAI's Codex (Rust-based, open source) has shipped a production skill system that prioritizes **simplicity, progressive disclosure, and filesystem-native discovery**. Rubichan's spec (v1.1) defines a more **feature-rich, programmatic skill system** with Starlark scripting, Go plugins, and JSON-RPC backends. Both are viable, but Codex offers several battle-tested patterns worth adopting — particularly around context efficiency, scope governance, and metadata-first loading.

This document identifies **8 concrete enhancements** to Rubichan's skill runtime, drawn from Codex's production experience.

---

## 2. Codex Skill Architecture Overview

### 2.1 Core Design Philosophy

Codex treats skills as **documentation-first modules**, not code-first plugins. A skill is fundamentally a directory with a `SKILL.md` file (human-readable instructions), not a code package with a manifest. This reflects a key insight: most agent skills are about *what to do*, not *how to compute*.

### 2.2 Structure

```
my-skill/
├── SKILL.md              # Required: YAML frontmatter + markdown body
├── agents/openai.yaml    # Optional: UI metadata, policy, dependencies
├── scripts/              # Optional: deterministic executables
├── references/           # Optional: loaded on-demand during execution
└── assets/               # Optional: templates, never loaded to context
```

### 2.3 Six-Tier Scope Hierarchy

Codex implements six priority levels for skill resolution (highest wins):

| Priority | Scope | Location |
|----------|-------|----------|
| 6 (highest) | Repo Local | `$CWD/.codex/skills` or `$CWD/.agents/skills` |
| 5 | Repo Parent | `$CWD/../.codex/skills` |
| 4 | Repo Root | `$REPO_ROOT/.codex/skills` or `$REPO_ROOT/.agents/skills` |
| 3 | User | `~/.codex/skills` |
| 2 | Admin | `/etc/codex/skills` |
| 1 (lowest) | System | Built-in (bundled with binary) |

### 2.4 Progressive Disclosure (Three-Phase Loading)

This is Codex's most distinctive pattern:

- **Phase 1 — BROWSE**: At startup, only YAML frontmatter is parsed (~100 words per skill: name + description). All discovered skills are listed as available in the system prompt.
- **Phase 2 — LOAD**: When a skill matches a task (explicit `$skill` mention or implicit description matching), the full `SKILL.md` body is loaded (~5,000 words max).
- **Phase 3 — USE**: During execution, scripts/references/assets are loaded on-demand, one file at a time.

**Key insight**: Context window budget is the scarcest resource. Codex optimizes for minimal upfront cost with lazy loading.

### 2.5 Invocation Model

Two activation modes:

- **Explicit**: User types `$skill-name` or uses `/skills` picker
- **Implicit**: Agent autonomously selects skill when task description matches skill description. Controlled by `allow_implicit_invocation` (default: true).

### 2.6 Tool Execution & Sandboxing

Codex uses a sophisticated multi-layer tool execution architecture:

- **ToolRouter**: Name-based dispatch to handlers
- **ToolOrchestrator**: Unified approval evaluation + sandbox policy
- **Platform sandboxes**: Bubblewrap/Landlock/seccomp (Linux), Seatbelt (macOS), AppContainer (Windows)
- **MCP integration**: External tool providers via McpConnectionManager

### 2.7 Agent Loop Architecture

Queue-based submission/event protocol:

- **Op → Event**: Typed channels with correlation IDs
- **SessionConfiguration**: Frozen snapshot at session start (immutable during session)
- **ContextManager**: Token tracking, history compaction, JSONL persistence
- **Incremental requests**: Optimized prompt transmission on follow-up turns

---

## 3. Comparative Analysis: Codex vs. Rubichan

### 3.1 What Rubichan Already Does Better

| Area | Rubichan Advantage |
|------|--------------------|
| **Skill types** | Five typed skill categories (tool, prompt, workflow, security-rule, transform) vs. Codex's untyped skills |
| **Programmatic backends** | Starlark + Go plugins + JSON-RPC vs. Codex's markdown-only instructions |
| **Lifecycle hooks** | Nine-phase hook system (OnActivate through OnSecurityScanComplete) vs. no hooks in Codex |
| **Permission model** | Ten declarative permissions with approval flow (ADR-009) vs. Codex's tool-level approval only |
| **Dependency graph** | SemVer dependencies between skills with topological activation vs. no skill dependencies in Codex |
| **Security rules** | Skills can contribute SAST patterns and LLM analyzers vs. no security integration in Codex |
| **Skill SDK** | Stable public Go SDK + Starlark built-ins vs. no SDK (Codex skills are just markdown) |
| **Workflow orchestration** | Multi-step workflow skills with LLM/tool calls vs. instruction-only workflows |

### 3.2 What Codex Does Better

| Area | Codex Advantage |
|------|-----------------|
| **Progressive disclosure** | Three-phase lazy loading minimizing context consumption |
| **Scope hierarchy** | Six-tier governance (system → admin → user → repo) vs. Rubichan's four sources |
| **Admin scope** | `/etc/codex/skills` for enterprise governance — missing in Rubichan |
| **Directory-walk discovery** | Scans every directory from CWD to repo root — more granular than Rubichan |
| **Context budget management** | Explicit `SKILL.md` size limits (500 lines), metadata budget (~100 words) |
| **Implicit invocation control** | Per-skill `allow_implicit_invocation` toggle |
| **Documentation-first philosophy** | Skills-as-instructions enables non-programmers to create skills |
| **Frozen session config** | Config snapshot at session start prevents mid-session drift |
| **Platform-native sandboxing** | OS-level sandbox (Bubblewrap, Seatbelt, AppContainer) vs. language-level only |
| **JSONL conversation persistence** | Event-sourced conversation log for replay/rollback |

---

## 4. Proposed Enhancements for Rubichan

### Enhancement 1: Progressive Disclosure for Skill Loading

**Problem**: Rubichan's spec loads full SKILL.yaml manifests + backend code at discovery time. With 20+ active skills, this consumes significant context budget.

**Codex pattern**: Three-phase loading (BROWSE → LOAD → USE).

**Proposal**: Add progressive disclosure to Rubichan's Loader:

```
Phase 1 — Index:   Parse only SKILL.yaml frontmatter (name, description, types, triggers)
                   Cost: ~50 tokens per skill. Used for system prompt skill list.

Phase 2 — Resolve: On trigger match, parse full SKILL.yaml (permissions, tools, schemas)
                   Cost: ~500 tokens per skill. Used for permission checks + tool registration.

Phase 3 — Execute: Load backend code (Starlark scripts, start processes)
                   Cost: Full. Used only when skill is actually invoked.
```

**Changes to spec**:
- `Loader.Discover()` returns `SkillIndex` (lightweight) instead of full `Manifest`
- New `Loader.Resolve(name)` materializes full manifest
- `Runtime.Activate()` calls Resolve before backend creation
- `PromptCollector` uses index-level data for skill listing

**Impact**: Reduces startup token cost from O(N × full_manifest) to O(N × ~50_tokens) for N discovered skills.

### Enhancement 2: Admin Scope for Enterprise Governance

**Problem**: Rubichan has four skill sources (built-in, user, project, inline) but no admin/enterprise scope.

**Codex pattern**: `/etc/codex/skills` admin scope between system and user.

**Proposal**: Add `SourceAdmin` at priority 1.5:

```
Priority 1 (lowest):  Built-in Skills (compiled into binary)
Priority 1.5 (new):   Admin Skills (/etc/aiagent/skills/ or $AIAGENT_ADMIN_DIR)
Priority 2:           User Skills (~/.config/aiagent/skills/)
Priority 3:           Project Skills (.agent/skills/)
Priority 3.5:         MCP Skills
Priority 4 (highest): Inline Skills (--skills flag)
```

**Use case**: Enterprise security teams mandate compliance skills (HIPAA scanner, PCI-DSS rules) that cannot be overridden by user or project skills. Admin skills with `mandatory: true` flag cannot be deactivated.

**Changes to spec**:
- Add `SourceAdmin` to skill source enum
- Add admin directory to Loader.Discover scan order
- Add `mandatory` field to SKILL.yaml (admin-only)
- Mandatory skills auto-activate and cannot be deactivated

### Enhancement 3: Directory-Walk Discovery within Repositories

**Problem**: Rubichan only scans `.agent/skills/` at project root. Monorepos with per-package skills are unsupported.

**Codex pattern**: Scan `.agents/skills` in every directory from CWD to repo root.

**Proposal**: Add monorepo-aware discovery:

```
CWD: /repo/packages/frontend/src/
Scan order:
  1. /repo/packages/frontend/src/.agent/skills/
  2. /repo/packages/frontend/.agent/skills/
  3. /repo/packages/.agent/skills/
  4. /repo/.agent/skills/
```

Skills found closer to CWD take higher priority. This enables:
- Package-specific skills in monorepos (e.g., React skills for frontend, Go skills for backend)
- Shared skills at repo root
- Subdirectory skills for specialized workflows

**Changes to spec**:
- `SourceProject` becomes `SourceProjectLocal`, `SourceProjectParent`, `SourceProjectRoot`
- Loader walks from CWD to repo root during discovery
- Priority: local > parent > root within project scope

### Enhancement 4: Implicit Invocation Control

**Problem**: Rubichan's trigger system auto-activates skills via file/keyword/language/mode triggers, but there's no per-skill control over whether the LLM can autonomously invoke a skill without explicit user mention.

**Codex pattern**: `allow_implicit_invocation` boolean per skill.

**Proposal**: Add invocation policy to SKILL.yaml:

```yaml
invocation:
  implicit: true          # LLM can choose this skill (default: true)
  explicit_only: false    # Only activate via $skill or --skills flag
  approval_required: false # Require user confirmation before activation
```

**Rationale**: Some skills (e.g., a deployment workflow) should never auto-activate. Others (e.g., a linter) should always be available implicitly. This gives skill authors fine-grained control.

### Enhancement 5: Context Budget Management

**Problem**: Rubichan's PromptCollector has `max_context_tokens` per skill, but no global budget or enforcement strategy for the aggregate context cost of all active skills.

**Codex pattern**: Strict size limits (~100 words metadata, ~5000 words body) plus context-aware loading.

**Proposal**: Add a global skill context budget:

```go
type ContextBudget struct {
    MaxTotalTokens     int  // Global cap for all skill prompt fragments
    MaxPerSkillTokens  int  // Per-skill cap (overrides SKILL.yaml if lower)
    ReserveFraction    float64  // Fraction of context reserved for conversation (e.g., 0.7)
}
```

Enforcement:
1. After all skills activate, PromptCollector sums fragment sizes
2. If over budget, skills are deprioritized by: explicit > triggered > implicit
3. Lower-priority skills get truncated or deferred to on-demand loading
4. Log warnings when budget pressure is high

**Changes to spec**: Add ContextBudget to Runtime config. PromptCollector enforces during assembly.

### Enhancement 6: Documentation-First Skill Type (Instruction Skills)

**Problem**: Rubichan requires every skill to have a `SKILL.yaml` manifest and implementation backend. This is heavy for simple skills that are just instructions/prompts.

**Codex pattern**: Skills-as-markdown — a `SKILL.md` file is the entire skill.

**Proposal**: Add a lightweight "instruction skill" mode:

```
my-instruction-skill/
├── SKILL.md              # Frontmatter + instructions (replaces SKILL.yaml for simple cases)
└── references/           # Optional on-demand files
```

`SKILL.md` frontmatter:
```yaml
---
name: react-best-practices
description: React development patterns and conventions
types: [prompt]
triggers:
  files: ["*.tsx", "*.jsx"]
  languages: [typescript, javascript]
---

## Instructions

When working with React components in this project...
```

The Loader detects `SKILL.md`-only directories and creates a synthetic Prompt skill. No backend, no schema files, no Starlark. Just system prompt injection on trigger.

**Rationale**: Lowers the barrier to skill creation dramatically. Non-programmers can create and share domain knowledge as skills. This is the single most impactful lesson from Codex's adoption success.

### Enhancement 7: Frozen Session Configuration

**Problem**: Rubichan's spec doesn't address what happens if config/skills change during a session.

**Codex pattern**: `SessionConfiguration` is a frozen snapshot at session start.

**Proposal**: Add session-scoped configuration snapshotting:

```go
type SessionConfig struct {
    Skills          map[string]*SkillSnapshot  // Frozen at session start
    Permissions     map[string][]Permission     // Granted permissions
    ContextBudget   ContextBudget
    CreatedAt       time.Time
    ConfigHash      string                     // Detect drift
}
```

Rules:
- Config is snapshotted when a session starts
- New skills installed mid-session are not visible until next session
- Permission grants are the only mutable state during a session
- `--hot-reload` flag opts into live skill reloading (for development)

**Rationale**: Prevents surprising behavior changes mid-conversation. Matches Codex's production behavior.

### Enhancement 8: Platform-Native Sandboxing for External Process Backend

**Problem**: Rubichan's spec defines process-level isolation for external process skills but doesn't specify OS-level sandboxing.

**Codex pattern**: Bubblewrap (Linux), Seatbelt (macOS), AppContainer (Windows).

**Proposal**: Add platform-native sandbox support for the External Process backend:

```go
type ProcessSandbox interface {
    Wrap(cmd *exec.Cmd, policy SandboxPolicy) (*exec.Cmd, error)
}

// Implementations:
// - BubblewrapSandbox (Linux): bwrap with Landlock + seccomp
// - SeatbeltSandbox (macOS): sandbox-exec with custom profiles
// - AppContainerSandbox (Windows): restricted tokens
// - NoopSandbox: fallback when platform sandbox unavailable
```

This wraps the external process command with OS-level restrictions based on the skill's declared permissions. A skill with `file:read` but not `file:write` gets a read-only filesystem sandbox.

**Changes to spec**: Add PlatformSandbox to process backend. SandboxFactory creates appropriate implementation per GOOS.

---

## 5. Enhancement Priority Matrix

| # | Enhancement | Impact | Effort | Priority |
|---|-------------|--------|--------|----------|
| 1 | Progressive Disclosure | High (context savings) | Medium | **P0** |
| 6 | Instruction Skills (SKILL.md) | High (adoption) | Low | **P0** |
| 5 | Context Budget Management | High (reliability) | Medium | **P1** |
| 7 | Frozen Session Config | Medium (correctness) | Low | **P1** |
| 4 | Implicit Invocation Control | Medium (safety) | Low | **P1** |
| 2 | Admin Scope | Medium (enterprise) | Low | **P2** |
| 3 | Directory-Walk Discovery | Medium (monorepo) | Medium | **P2** |
| 8 | Platform-Native Sandboxing | High (security) | High | **P2** |

**Recommended implementation order**: Start with P0 items (progressive disclosure + instruction skills) as they provide the highest value with moderate effort and directly address the biggest gap between Rubichan and Codex's production-proven patterns.

---

## 6. Patterns NOT to Adopt from Codex

Not everything from Codex is appropriate for Rubichan:

| Codex Pattern | Why NOT for Rubichan |
|---------------|----------------------|
| **No typed skills** | Rubichan's five skill types enable richer integration (security rules, transforms, workflows). Codex's untyped model is simpler but less powerful. |
| **No programmatic backends** | Rubichan needs Starlark/Go plugins for compute-heavy skills (security scanners, code analysis). Codex's instruction-only model can't support this. |
| **No lifecycle hooks** | Rubichan's hook system enables skill composition (e.g., a transform modifying another skill's output). Codex has no equivalent. |
| **No dependency graph** | Rubichan's SemVer dependencies enable composable skill ecosystems. Codex skills are standalone islands. |
| **Restart-required loading** | Codex requires restart to pick up new skills. Rubichan's dynamic activation is superior. |
| **No permission model** | Codex uses tool-level approval only. Rubichan's 10-permission declarative model (ADR-009) is more fine-grained and auditable. |

---

## 7. Key Takeaways

1. **Context efficiency is paramount**: Codex's progressive disclosure is its killer feature. Every token of skill metadata in the system prompt is a token not available for conversation. Rubichan must adopt lazy loading.

2. **Lower the skill creation barrier**: Codex's biggest adoption success is that anyone can create a skill with a markdown file. Rubichan should support both simple (SKILL.md) and complex (SKILL.yaml + backend) skill formats.

3. **Governance matters at scale**: Codex's six-tier scope hierarchy with admin scope shows enterprise readiness. Rubichan should add admin scope and directory-walk discovery.

4. **Freeze session state**: Mid-session config changes cause subtle bugs. Snapshot at start, reload on restart.

5. **OS-level sandboxing is table stakes**: Language-level sandboxing (Starlark) is good for scripting skills, but external process skills need OS-level isolation. Bubblewrap/Seatbelt/AppContainer are the industry standard.

---

## Sources

- [OpenAI Codex Architecture — DeepWiki](https://deepwiki.com/openai/codex/7-model-context-protocol-(mcp))
- [OpenAI Skills Catalog — DeepWiki](https://deepwiki.com/openai/skills)
- [Using Skills with Codex — DeepWiki](https://deepwiki.com/heilcheng/awesome-agent-skills/7.3-using-skills-with-codex)
- [Codex Developer Guide — DeepWiki](https://deepwiki.com/openai/codex/4-developer-guide)
- [Agent Skills — OpenAI Developer Docs](https://developers.openai.com/codex/skills/)
- [Skills in OpenAI Codex — blog.fsck.com](https://blog.fsck.com/2025/12/19/codex-skills/)
- [OpenAI Skills Catalog — GitHub](https://github.com/openai/skills)
- [Codex CLI Features — OpenAI Developer Docs](https://developers.openai.com/codex/cli/features/)
