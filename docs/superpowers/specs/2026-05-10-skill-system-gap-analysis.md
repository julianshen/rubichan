# Skill System Gap Analysis: ccgo, claude-code, rubichan

**Date:** 2026-05-10
**Scope:** Cross-project skill system research to identify port opportunities for rubichan

---

## Executive Summary

| System | Paradigm | Strengths | Key Gaps in Rubichan |
|--------|----------|-----------|---------------------|
| **ccgo** | File-based markdown skills | Simple discovery, async prefetch, compaction rules | No prefetch, no `.kilo/skills/` discovery paths |
| **claude-code** | Prompt-template skills | Rich frontmatter, forked execution, permission integration, bundled skills | No markdown skill format, no forked execution, no bundled skill API |
| **rubichan** | Multi-runtime (Starlark/Go/MCP/Process) | Full lifecycle, hooks, triggers, sandbox, registry | No simple markdown authoring, no async prefetch, no skill event bus |

---

## 1. ccgo Skill System

### Architecture

ccgo's skill system is **minimal and file-based**. Skills are markdown files discovered from well-known directories:

```text
~/.claude/skills/<name>/SKILL.md
~/.claude/skills/<name>.md
~/.opencode/skills/<name>/SKILL.md
~/.kilo/skills/<name>/SKILL.md
./.claude/skills/<name>/SKILL.md
./.opencode/skills/<name>/SKILL.md
./.kilo/skills/<name>/SKILL.md
```

### Key Components

#### 1.1 SkillTool (`tool/skills_todos.go:405-513`)

- **Single tool** that loads skill content by name
- **No formal registration** — skills are discovered at call time
- **Security:** Rejects `..`, `/`, and path separators in skill names
- **Returns raw markdown** as tool result for the LLM to process
- **Search precedence:** User dirs > project dirs (implicit via iteration order)

#### 1.2 SkillPrefetch (`query/prefetch.go`)

- **Async preloading** of skills before explicit request
- `SkillPrefetchHandle` with states: Pending, Settled, Consumed, Error
- Thread-safe with mutex + done channel
- Settled skills are consumed once, then marked Consumed

#### 1.3 Compaction Rules (`compact/compact.go:179-188`)

- **Strips skill discovery messages** during compaction
- `isReinjectedAttachmentType()` filters `skill_discovery` and `skill_listing` roles
- Prevents skill metadata from bloating context window

#### 1.4 System Prompt Integration (`query/context.go`)

- Skills advertised via `SkillPrompt` struct in `<skills>` XML tags
- Token budget for skills: 8K characters (1% of context window)

### What Rubichan Can Port

| Feature | Priority | Effort | Value |
|---------|----------|--------|-------|
| `.kilo/skills/` discovery paths | P1 | Low | Immediate compatibility |
| Async skill prefetch | P2 | Medium | Latency reduction |
| Skill compaction rules | P3 | Low | Context efficiency |
| Skill token budgeting | P2 | Medium | Context management |

---

## 2. Claude-Code Skill System

### Architecture

claude-code's skill system is **rich and declarative**. Skills are markdown files with YAML frontmatter that act as **prompt templates**:

```markdown
---
name: commit
description: Generate a conventional commit message
allowed-tools: [Read, Git]
context: inline
---

Analyze the staged changes and write a commit message...
```

### Key Components

#### 2.1 Skill Format (`src/skills/loadSkillsDir.ts`)

- **YAML frontmatter** with rich metadata:
  - `name`, `description`, `version`
  - `allowed-tools`: Tool permission restrictions
  - `model`: LLM model override
  - `context`: `inline` (default) or `fork` (isolated sub-agent)
  - `effort`: Token budget/thinking level
  - `paths`: Conditional activation (gitignore-style matching)
  - `hooks`: Lifecycle hooks
  - `arguments`: Parameter definitions with `$ARGUMENTS` substitution
- **Shell execution**: `!command` and `` ```! ... ``` `` blocks execute and substitute output
- **Conditional skills**: Only activate when matching files are touched

#### 2.2 Skill Tool (`src/tools/SkillTool.ts`)

- **Input schema:** `{ skill: string, args?: string }`
- **Two execution modes:**
  - **Inline:** Expands into current conversation
  - **Forked:** Runs in isolated sub-agent with separate context
- **Permission integration:** Skills participate in general permission framework
- **Auto-allow:** Safe skills (no custom permissions, no dangerous flags) bypass user prompt
- **Context modifiers:** `allowedTools`, `model`, `effort`

#### 2.3 Bundled Skills (`src/skills/bundledSkills.ts`)

- **Registration API:** `registerBundledSkill(definition)`
- **File extraction:** Reference docs lazily extracted to temp directory
- **Lazy loading:** Heavy content dynamically imported
- Examples: `/simplify`, `/batch`, `/skillify`, `/claude-api`

#### 2.4 Commands Registry (`src/commands.ts`)

- **Loading order (precedence):** bundled > builtin plugin > skill dir > workflow > plugin > built-in
- **Dynamic insertion:** Discovered skills inserted at specific registry positions
- **Availability filtering:** Gates by auth type and feature flags

#### 2.5 MCP Skill Bridge (`src/skills/mcpSkillBuilders.ts`)

- **Dependency cycle breaker:** Write-once registry for MCP skill builders
- MCP skills use same frontmatter parser as file-based skills

### What Rubichan Can Port

| Feature | Priority | Effort | Value |
|---------|----------|--------|-------|
| Markdown skill format with frontmatter | P1 | Medium | Simpler skill authoring |
| `allowed-tools` permission restrictions | P2 | Medium | Better security model |
| Forked execution (`context: fork`) | P3 | High | Isolation for untrusted skills |
| Bundled skill registration API | P2 | Medium | Built-in capabilities |
| Argument substitution (`$ARGUMENTS`) | P2 | Low | Better UX |
| Conditional activation (`paths:`) | P2 | Medium | Smart skill loading |
| Skill token budgeting (1% context) | P2 | Low | Context management |
| Skill extraction (`/skillify`) | P4 | High | Workflow capture |

---

## 3. Rubichan Existing Skill System

### Architecture

rubichan has a **multi-runtime skill system** with full lifecycle management:

```text
internal/skills/
  manifest.go          # SKILL.yaml parsing, validation
  loader.go            # Discovery from user/project/MCP dirs
  runtime.go           # Activation, deactivation, registry integration
  types.go             # SkillBackend interface, lifecycle states
  broker.go            # CapabilityBroker for permission enforcement
  hooks.go             # LifecycleManager for hook dispatch
  triggers.go          # Trigger evaluation for auto-activation
  integration.go       # PromptCollector, WorkflowRunner, SecurityAdapter
  registry.go          # Remote registry client (HTTP API)
  declarative.go       # Manifest-defined slash commands
  
  starlark/            # Starlark backend (sandboxed Python-like)
  goplugin/            # Go plugin backend (shared library)
  process/             # External process backend (JSON-RPC)
  mcpbackend/          # MCP server backend
  sandbox/             # Sandboxed execution
  builtin/             # Built-in skills (git, wiki, code review, etc.)
```

### Strengths

1. **Full lifecycle:** Inactive → Activating → Active → Error with valid transitions
2. **Multi-backend:** Starlark, Go plugin, external process, MCP
3. **Hook system:** 14 hook phases with priority-based dispatch
4. **Trigger system:** File, keyword, language, mode matching with scoring
5. **Sandbox:** PermissionChecker with per-call enforcement via CapabilityBroker
6. **Context budget:** Token-based prompt fragment budgeting
7. **Registry client:** Remote skill registry with caching
8. **Instruction skills:** SKILL.md with YAML frontmatter (already supported!)

### Gaps

| Gap | Description | Impact |
|-----|-------------|--------|
| **No markdown-native skills** | Only SKILL.md with frontmatter; no pure markdown skill format like ccgo/claude-code | Higher barrier to entry |
| **No async prefetch** | Skills loaded synchronously on first use | Latency on skill invocation |
| **No `.kilo/skills/` paths** | Only `.claude/skills/` and `.opencode/skills/` in some contexts | Branding inconsistency |
| **No forked execution** | All skills run in main agent context | No isolation for untrusted skills |
| **No bundled skill API** | Built-in skills registered manually | No standardized bundled skill pattern |
| **No skill event bus** | No pub/sub for skill state changes | Limited observability |
| **No dynamic reloading** | Skills discovered once at startup | No hot-reload during development |
| **Limited permission granularity** | `ToolsAllow`/`ToolsDeny` exist but not enforced per-call | Security gap |
| **No skill compaction** | Skill discovery messages not stripped | Context bloat |

---

## 4. Priority Recommendations

### Phase 1: Quick Wins (P1)

1. **Add `.kilo/skills/` discovery paths**
   - Update `Loader` to search `~/.kilo/skills/` and `./.kilo/skills/`
   - Effort: ~20 lines in `loader.go`

2. **Enhance markdown skill support**
   - Pure markdown skills (no frontmatter) — just load and return content
   - Effort: ~50 lines in `loader.go` + `scanDir()`

### Phase 2: Core Improvements (P2)

3. **Async skill prefetch**
   - Add `PrefetchHandle` pattern from ccgo
   - Preload likely-to-activate skills based on triggers
   - Effort: New file `internal/skills/prefetch.go` + integration in runtime

4. **Skill token budgeting**
   - Cap skill descriptions in system prompt (1% context window)
   - Truncate non-bundled skill descriptions
   - Effort: ~30 lines in prompt building

5. **ToolsAllow/ToolsDeny enforcement**
   - Actually enforce `ToolsAllow`/`ToolsDeny` in CapabilityBroker
   - Effort: ~20 lines in `broker.go`

6. **Bundled skill registration API**
   - `RegisterBundledSkill()` pattern from claude-code
   - Effort: New method on `Loader` + `Runtime`

### Phase 3: Advanced Features (P3)

7. **Forked skill execution**
   - `context: fork` equivalent — run skill in sub-agent
   - Effort: Integration with existing subagent system

8. **Skill event bus**
   - Pub/sub for skill state changes
   - Effort: New file `internal/skills/events.go`

9. **Skill compaction**
   - Strip skill discovery messages during compaction
   - Effort: Integration with existing compaction system

### Phase 4: Nice-to-Have (P4)

10. **Skill extraction (`/skillify`)**
    - Capture session as reusable skill
    - Effort: New skill + UI integration

11. **Dynamic skill reloading**
    - Watch skill directories for changes
    - Effort: File watcher + hot-reload logic

---

## 5. Implementation Order

```text
P1.1: .kilo/skills/ discovery paths
P1.2: Pure markdown skill support
P2.1: Async skill prefetch
P2.2: Skill token budgeting
P2.3: ToolsAllow/ToolsDeny enforcement
P2.4: Bundled skill registration API
P3.1: Forked skill execution
P3.2: Skill event bus
P3.3: Skill compaction
P4.1: Skill extraction
P4.2: Dynamic skill reloading
```

Each phase should be a separate PR with tests, following TDD.

---

## 6. Key Design Decisions

### 6.1 Markdown vs YAML Skills

- **Keep SKILL.yaml** for complex skills (multi-backend, hooks, workflows)
- **Add pure markdown** for simple prompt skills (like ccgo/claude-code)
- **SKILL.md with frontmatter** already works — enhance it

### 6.2 Discovery Paths

```text
Priority (highest first):
1. Built-in skills (code-registered)
2. Inline/explicit skills (--skills flag)
3. User skills: ~/.kilo/skills/ > ~/.claude/skills/ > ~/.opencode/skills/
4. Project skills: ./.kilo/skills/ > ./.claude/skills/ > ./.opencode/skills/
5. Configured skill roots
6. MCP servers
```

### 6.3 Prefetch Strategy

- Evaluate triggers against current context
- Preload skills with score >= threshold
- Use `SkillPrefetchHandle` pattern (async, cancellable)
- Consume on first use, skip if not needed

### 6.4 Permission Model

- **Current:** `Permissions` array checked at activation
- **Gap:** `ToolsAllow`/`ToolsDeny` declared but not enforced
- **Fix:** Add to CapabilityBroker.CheckExecution()
- **Future:** Consider claude-code's `allowed-tools` model

---

## 7. Files to Modify

| File | Changes |
|------|---------|
| `internal/skills/loader.go` | Add `.kilo/skills/` paths, pure markdown support |
| `internal/skills/runtime.go` | Add prefetch integration, bundled skill API |
| `internal/skills/broker.go` | Enforce ToolsAllow/ToolsDeny |
| `internal/skills/prefetch.go` | New: async prefetch handles |
| `internal/skills/events.go` | New: skill event bus |
| `internal/skills/types.go` | Add BundledSkillDefinition type |
| `internal/agent/prompt.go` | Add skill token budgeting |
| `internal/compact/` | Add skill message stripping |

---

## 8. Testing Strategy

- **Unit tests** for each new component (prefetch, events, broker changes)
- **Integration tests** for discovery path precedence
- **End-to-end tests** for full skill lifecycle with new features
- **Benchmarks** for prefetch latency improvement

---

*This analysis is based on code review of ccgo (commit unknown), claude-code-source-code-leak (commit unknown), and rubichan (current working tree).*
