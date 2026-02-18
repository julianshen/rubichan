# Milestone 3: Skill System — Design

## Scope

**In scope:**
- Skill manifest schema (SKILL.yaml) + parser/validator
- Skill loader with 4-source discovery (built-in, user, project, inline)
- Trigger evaluation engine (file, keyword, language, mode)
- All 3 backends: Starlark engine, Go plugins (.so), external processes (JSON-RPC)
- All 5 skill types: tool, prompt, workflow, security-rule, transform
- Permission model with sandbox enforcement + SQLite approval store
- 9 lifecycle hook phases
- Skill CLI subcommands (list, install, remove, info, create, test, search, permissions)
- Remote skill registry client
- Built-in skills: core-tools, git
- 3 example skills: kubernetes, ddd-expert, rfc-writer
- Public SDK (`pkg/skillsdk/`)

**Out of scope (deferred to M4+):**
- Security engine integration (security-rule skills register scanners but engine doesn't exist)
- Wiki pipeline integration (wiki contributions register but pipeline doesn't exist)
- Built-in skills depending on missing subsystems (security-base, apple-dev, wiki-base, log-analysis, code-review)

## Architecture

### Package structure

```text
internal/skills/           — Runtime orchestrator, manifest, loader, triggers, hooks, types
internal/skills/starlark/  — Starlark engine + SDK built-in functions
internal/skills/process/   — External process backend (JSON-RPC over stdin/stdout)
internal/skills/goplugin/  — Go plugin (.so) loader
internal/skills/sandbox/   — Permission enforcement, rate limits
internal/store/            — SQLite abstraction (permissions, registry cache, skill state)
pkg/skillsdk/             — Public Go SDK for plugin authors
cmd/rubichan/skill.go     — Skill CLI subcommands
```

### Data flow

```text
Startup / User message
    |
Loader (4-source discovery)
    |
Trigger Engine (file, keyword, language, mode)
    |
Permission Check (sandbox + SQLite approval store)
    |
Backend Load (starlark | plugin | process)
    |
Skill Active
    |--- Tools registered in tools.Registry
    |--- Prompt fragments injected via OnBeforePromptBuild
    |--- Hooks registered in LifecycleManager
    |--- Workflows available for invocation
    |--- Security rules registered (adapter, no engine yet)
    |--- Transforms registered via OnAfterResponse
```

## Skill Manifest

Parsed from `SKILL.yaml` into:

```go
type SkillManifest struct {
    Name           string
    Version        string           // SemVer
    Description    string
    Types          []SkillType      // tool, prompt, workflow, security-rule, transform
    Author         string
    License        string
    Homepage       string
    Triggers       TriggerConfig
    Permissions    []Permission     // file:read, shell:exec, etc.
    Dependencies   []Dependency
    Implementation ImplementationConfig
    Prompt         PromptConfig
    Tools          []ToolDef
    SecurityRules  SecurityRuleConfig
    Wiki           WikiConfig
    Compatibility  CompatibilityConfig
}

type TriggerConfig struct {
    Files     []string   // glob patterns
    Keywords  []string
    Modes     []string   // interactive, headless, wiki
    Languages []string
}

type ImplementationConfig struct {
    Backend    BackendType // starlark, plugin, process
    Entrypoint string     // skill.star, plugin.so, main.py
}
```

Required fields: name, version, description, types. Validation enforces lowercase+hyphen names, valid SemVer, known permission strings, known skill types.

## Loader & Discovery

4-source discovery in priority order (higher priority wins on name collision):

1. **Built-in** — compiled into binary, returned from Go registration functions
2. **User** — `~/.config/rubichan/skills/<name>/SKILL.yaml`
3. **Project** — `.agent/skills/<name>/SKILL.yaml`
4. **Inline** — `--skills=name1,name2` flag or AGENT.md frontmatter

The loader walks each source, parses manifests, deduplicates by name, validates dependencies (missing required deps = error, missing optional = warning), and returns discovered skills. No activation at this stage.

## Trigger Engine

Evaluates each discovered skill's triggers against:

```go
type TriggerContext struct {
    ProjectFiles    []string
    DetectedLangs   []string
    BuildSystem     string
    LastUserMessage string
    Mode            string
    ExplicitSkills  []string
}
```

Activation rules:
- Explicit (`--skills` flag) always activates, no trigger check
- File triggers: any declared pattern matches a project file
- Keyword triggers: keyword found in user message
- Language triggers: detected language matches
- Mode triggers: current mode matches
- Any matching trigger activates the skill

## Skill Lifecycle

**States:** `Inactive -> Activating -> Active -> Error`

- `Inactive -> Activating`: trigger matches or explicit request
- `Activating -> Active`: backend loaded, permissions approved, OnActivate hook runs
- `Activating -> Error`: permission denied, backend load failure, hook error
- `Active -> Inactive`: OnDeactivate hook, then unload

### 9 Lifecycle Hook Phases

| Phase | When | Can modify? |
|-------|------|-------------|
| OnActivate | Skill just activated | No |
| OnDeactivate | Skill deactivating | No |
| OnConversationStart | New conversation begins | No |
| OnBeforePromptBuild | Before system prompt assembled | Inject prompt fragments |
| OnBeforeToolCall | Before tool executes | Modify input, cancel call |
| OnAfterToolResult | After tool returns | Modify result |
| OnAfterResponse | After LLM response complete | Transform output |
| OnBeforeWikiSection | Before wiki section generated | Inject context |
| OnSecurityScanComplete | After security scan | Add/filter findings |

LifecycleManager dispatches events to all active skills with registered handlers. Hooks run in priority order: built-in first, then user, then project.

## Three Backends

### Starlark (`internal/skills/starlark/`)

- Embeds `go.starlark.net` interpreter
- Each skill gets its own Starlark thread with fresh global scope
- SDK built-in functions injected as predefined globals
- Every SDK function checks permissions via sandbox before executing
- `register_tool()`, `register_hook()`, `register_workflow()`, `register_scanner()` for declaring contributions
- Single-threaded per skill (safe by design)

### Go Plugin (`internal/skills/goplugin/`)

- Uses Go `plugin.Open()` to load `.so` files
- Plugin exports `NewSkill() skillsdk.SkillPlugin`
- `skillsdk.SkillPlugin` interface: `Manifest()`, `Activate(ctx)`, `Deactivate(ctx)`
- `skillsdk.Context` provides typed Go equivalents of Starlark SDK functions
- Known limitation: must match exact Go version at compile time
- Platform-gated: Linux/macOS only (Go plugins don't support Windows)

### External Process (`internal/skills/process/`)

- Child process in any language (Python, Node, Rust, etc.)
- JSON-RPC 2.0 over stdin/stdout
- Protocol methods: `initialize`, `tool/execute`, `hook/handle`, `shutdown`
- ProcessManager handles lifecycle: start on activation, kill on deactivation, restart on crash (with backoff)
- Timeout on each RPC call from sandbox policy

### Common interface

```go
type SkillBackend interface {
    Load(manifest SkillManifest, sandbox *Sandbox) error
    Tools() []tools.Tool
    Hooks() map[HookPhase]HookHandler
    Unload() error
}
```

## Permission Model & SQLite Store

### Sandbox (`internal/skills/sandbox/`)

- 10 permission types: file:read, file:write, shell:exec, net:fetch, llm:call, git:read, git:write, env:read, env:write, skill:invoke
- Every SDK operation checks permissions before executing
- Rate limits per turn: MaxLLMCallsPerTurn, MaxShellExecPerTurn, MaxNetFetchPerTurn (configurable in config.toml)
- Timeouts: ShellExecTimeout, NetFetchTimeout

### Approval flow

- Interactive mode: TUI shows permission approval prompt on first activation. Options: "Allow once", "Allow always", "Deny"
- Headless mode: pre-approve via `--approve-skills=name` flag or config.toml `[skills.approved]` section
- "Allow always" persists in SQLite

### SQLite store (`internal/store/`)

- `modernc.org/sqlite` (pure Go, no CGO)
- Database: `~/.config/rubichan/rubichan.db`
- Tables: `permission_approvals`, `skill_state`, `registry_cache`
- Auto-migration on first open (CREATE TABLE IF NOT EXISTS)

```go
type Store struct { db *sql.DB }

func NewStore(dbPath string) (*Store, error)
func (s *Store) IsApproved(skill, permission string) (bool, error)
func (s *Store) Approve(skill, permission, scope string) error
func (s *Store) Revoke(skill string) error
func (s *Store) ListApprovals(skill string) ([]Approval, error)
func (s *Store) SaveSkillState(state SkillState) error
func (s *Store) GetSkillState(name string) (*SkillState, error)
func (s *Store) CacheRegistryEntry(entry RegistryEntry) error
func (s *Store) GetCachedRegistry(name string) (*RegistryEntry, error)
```

## Skill CLI & Registry

### CLI subcommands

```text
rubichan skill list                              # installed skills
rubichan skill list --available                  # from remote registry
rubichan skill search "kubernetes"               # search registry
rubichan skill install kubernetes                # install to user skills
rubichan skill install kubernetes@1.2.0          # specific version
rubichan skill install github.com/user/my-skill  # from git URL
rubichan skill install ./local-skill             # from local path
rubichan skill add kubernetes                    # add to project (.agent/skills/)
rubichan skill remove kubernetes                 # uninstall
rubichan skill info kubernetes                   # show manifest + permissions + state
rubichan skill create my-skill --type=tool       # scaffold new skill
rubichan skill test ./my-skill                   # run skill tests
rubichan skill permissions kubernetes            # show approvals
rubichan skill permissions kubernetes --revoke   # revoke all
```

### Registry client

- HTTPS JSON API at configurable URL (config.toml: `[skills] registry_url`)
- Operations: Search, GetManifest, Download (tarball)
- Results cached in SQLite with TTL (default 1 hour)
- Git install: clone to temp dir, validate SKILL.yaml, move to skills directory

### Skill scaffolding

`rubichan skill create my-skill --type=tool` generates:
```text
my-skill/
  SKILL.yaml       # pre-filled manifest template
  skill.star       # Starlark entrypoint (if starlark backend)
  README.md
```

## Five Skill Types

**Tool:** Register new tools into `tools.Registry` on activation. Removed on deactivation. Backend exposes tools via `SkillBackend.Tools()`.

**Prompt:** Inject system prompt fragments via `OnBeforePromptBuild` hook. Declared via `prompt.system_prompt_file` and `prompt.context_files` with `max_context_tokens` budget.

**Workflow:** Multi-step orchestrations invoked by name (`rubichan run <workflow>`) or keyword trigger. Handler has full SDK access (tools, LLM, files).

**Security Rule:** Register SAST patterns and scanner functions via `SecurityRuleAdapter`. Adapter stores registrations; actual security engine deferred to M4.

**Transform:** Post-process agent output via `OnAfterResponse` hook. Multiple transforms chain in priority order.

## Testing

- **Unit:** Manifest parsing, loader discovery (mock dirs), trigger evaluation, lifecycle states, hook dispatch, each Starlark SDK function, JSON-RPC encoding, plugin loading, permission checks, rate limits, SQLite CRUD, CLI flag parsing
- **Integration:** Full lifecycle (discover -> trigger -> approve -> activate -> tool call -> deactivate), Starlark skill end-to-end, external process with real child process, permission denial blocks execution, hook chain modifies tool I/O
- **Example skills:** kubernetes, ddd-expert, rfc-writer manifests validated and Starlark loaded in tests
- **SQLite tests:** in-memory databases (`:memory:`)
- **Process tests:** test helper binary built during `go test`
