# Milestone 3: Skill System — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a fully functional skill runtime supporting 5 skill types, 3 backends (Starlark, Go plugins, external processes), 9 lifecycle hooks, permission model with SQLite, CLI management commands, and a remote registry client.

**Architecture:** Layered packages under `internal/skills/` with a shared `internal/store/` for SQLite. The runtime orchestrator discovers skills from 4 sources, evaluates triggers, enforces permissions via a sandbox, loads backends, and integrates tools/prompts/hooks into the existing agent loop. `pkg/skillsdk/` provides the public Go SDK for plugin authors.

**Tech Stack:** Go 1.26, `go.starlark.net` (Starlark), `modernc.org/sqlite` (pure Go SQLite), Cobra (CLI), existing agent/provider/tools packages.

---

## Tasks

### Task 1: SQLite store - schema and basic CRUD

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

**Step 1: Write the failing tests**

Tests for: `NewStore` with in-memory DB, `Approve`/`IsApproved` (always scope), `Approve` (once scope), `Revoke`, `ListApprovals`, `SaveSkillState`/`GetSkillState`, `GetSkillState` not found, `CacheRegistryEntry`/`GetCachedRegistry`.

8 test functions covering all CRUD operations.

Types needed:
- `Approval{Skill, Permission, Scope, ApprovedAt}`
- `SkillInstallState{Name, Version, Source, InstalledAt}`
- `RegistryEntry{Name, Version, Description, CachedAt}`

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -v`
Expected: FAIL - package does not exist

**Step 3: Write minimal implementation**

- `NewStore(dbPath)` opens SQLite via `modernc.org/sqlite`, runs CREATE TABLE IF NOT EXISTS for 3 tables: `permission_approvals` (skill, permission, scope, approved_at; PK skill+permission), `skill_state` (name PK, version, source, installed_at), `registry_cache` (name PK, version, description, cached_at)
- `IsApproved` checks for `scope='always'` row
- `Approve` uses INSERT OR REPLACE
- `Revoke` deletes all rows for a skill
- `ListApprovals` returns all rows for a skill
- `SaveSkillState`/`GetSkillState` - standard CRUD, GetSkillState returns nil for not found
- `CacheRegistryEntry`/`GetCachedRegistry` - standard CRUD

**Step 4: Add dependency and run tests**

Run: `go get modernc.org/sqlite && go test ./internal/store/ -v`
Expected: All 8 tests PASS

**Step 5: Commit**

```bash
git add internal/store/ go.mod go.sum
git commit -m "[BEHAVIORAL] Add SQLite store with permission approvals, skill state, registry cache"
```

---

### Task 2: Manifest types and YAML parser

**Files:**
- Create: `internal/skills/manifest.go`
- Create: `internal/skills/manifest_test.go`

**Step 1: Write the failing tests**

8 test functions:
- `TestParseManifestMinimal` - name, version, description, types, implementation only
- `TestParseManifestFull` - all fields: triggers, permissions, dependencies, prompt, tools, compatibility
- `TestParseManifestMissingName` - error containing "name"
- `TestParseManifestMissingTypes` - error containing "types"
- `TestParseManifestInvalidPermission` - error for "invalid:perm"
- `TestParseManifestInvalidBackend` - error for "unknown" backend
- `TestParseManifestInvalidSkillType` - error for "bogus" type
- `TestParseManifestInvalidName` - error for "My Skill!" (must be lowercase+hyphens)

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Types: `SkillType` enum (tool, prompt, workflow, security-rule, transform), `BackendType` enum (starlark, plugin, process), `Permission` string type with 10 known values (file:read, file:write, shell:exec, net:fetch, llm:call, git:read, git:write, env:read, env:write, skill:invoke).

`SkillManifest` struct with all fields. `ParseManifest([]byte) (*SkillManifest, error)` using `gopkg.in/yaml.v3`. Validation: required fields, name regex `^[a-z][a-z0-9-]*$`, known permissions, known backends, known types.

**Step 4: Add dependency and run tests**

Run: `go get gopkg.in/yaml.v3 && go test ./internal/skills/ -v`
Expected: All 8 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/manifest.go internal/skills/manifest_test.go go.mod go.sum
git commit -m "[BEHAVIORAL] Add skill manifest types and YAML parser with validation"
```

---

### Task 3: Permission sandbox

**Files:**
- Create: `internal/skills/sandbox/sandbox.go`
- Create: `internal/skills/sandbox/sandbox_test.go`

**Step 1: Write the failing tests**

6 test functions:
- `TestCheckPermissionAllowed` - pre-approve in store, check passes
- `TestCheckPermissionDenied` - no approval, check fails with "not approved"
- `TestCheckPermissionNotDeclared` - permission not in declared list, fails with "not declared"
- `TestRateLimitShellExec` - 2 allowed, 3rd fails
- `TestResetRateLimits` - after reset, counter resets
- `TestAutoApproveMode` - auto-approved skills bypass store check

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/sandbox/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`SandboxPolicy` with MaxLLMCallsPerTurn, MaxShellExecPerTurn, MaxNetFetchPerTurn, ShellExecTimeout, NetFetchTimeout. `DefaultPolicy()` returns sensible defaults.

`Sandbox` holds store, declared permissions map, policy, auto-approve set, rate counters (map[string]int). `CheckPermission` checks declared then approved. `CheckRateLimit` increments counter and checks limit. `ResetTurnLimits` zeros counters. `SetAutoApprove` sets the auto-approve list.

**Step 4: Run tests**

Run: `go test ./internal/skills/sandbox/ -v`
Expected: All 6 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/sandbox/
git commit -m "[BEHAVIORAL] Add permission sandbox with rate limits and auto-approve"
```

---

### Task 4: Skill loader - 4-source discovery

**Files:**
- Create: `internal/skills/loader.go`
- Create: `internal/skills/loader_test.go`

**Step 1: Write the failing tests**

6 test functions using temp directories with SKILL.yaml files:
- `TestDiscoverUserSkills` - finds skills in user dir
- `TestDiscoverProjectSkills` - finds skills in project dir
- `TestDiscoverExplicitSkills` - explicit list marks skills as explicit source
- `TestDiscoverDeduplication` - user skill overrides project skill with same name
- `TestDiscoverMissingRequiredDep` - returns error
- `TestDiscoverMissingOptionalDep` - returns warning, not error

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run TestDiscover -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`Loader{userDir, projectDir, builtins}`. `NewLoader(userDir, projectDir)`. `RegisterBuiltin(m)`. `Discover(explicit) ([]DiscoveredSkill, []string, error)`. `DiscoveredSkill{Manifest, Dir, Source}`.

Walk each directory, find `SKILL.yaml` files, parse, deduplicate by name (builtin > user > project), validate dependencies cross-referencing the discovered set.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -run TestDiscover -v`
Expected: All 6 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/loader.go internal/skills/loader_test.go
git commit -m "[BEHAVIORAL] Add skill loader with 4-source discovery and deduplication"
```

---

### Task 5: Trigger evaluation engine

**Files:**
- Create: `internal/skills/triggers.go`
- Create: `internal/skills/triggers_test.go`

**Step 1: Write the failing tests**

8 test functions:
- `TestTriggerExplicit` - explicit skills always match
- `TestTriggerFileMatch` - file pattern matches project files
- `TestTriggerFileNoMatch` - no patterns match
- `TestTriggerKeywordMatch` - keyword in user message
- `TestTriggerLanguageMatch` - detected language matches
- `TestTriggerModeMatch` - current mode matches
- `TestTriggerNoTriggers` - never auto-activates
- `TestEvaluateMultipleSkills` - returns only matching subset

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run TestTrigger -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`TriggerContext{ProjectFiles, DetectedLangs, BuildSystem, LastUserMessage, Mode, ExplicitSkills}`.

`EvaluateTriggers(skills, ctx) []DiscoveredSkill` - explicit always matches; file triggers use `filepath.Match`; keyword triggers use case-insensitive `strings.Contains`; language/mode use exact match. Any matching trigger activates.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -run TestTrigger -v`
Expected: All 8 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/triggers.go internal/skills/triggers_test.go
git commit -m "[BEHAVIORAL] Add trigger evaluation engine for skill auto-activation"
```

---

### Task 6: Skill types, backend interface, lifecycle states

**Files:**
- Create: `internal/skills/types.go`
- Create: `internal/skills/types_test.go`

**Step 1: Write the failing tests**

3 test functions:
- `TestSkillStateTransitions` - valid transitions
- `TestSkillStateInvalidTransition` - Active->Activating returns error
- `TestHookPhaseString` - each phase has string representation

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run "TestSkillState|TestHookPhase" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`SkillState` enum (Inactive, Activating, Active, Error). `HookPhase` enum (9 phases). `HookEvent`, `HookResult`, `HookHandler` types. `SkillBackend` interface with `Load`, `Tools`, `Hooks`, `Unload`. `Skill` struct with `Manifest`, `State`, `Dir`, `Source`, `Backend`. `Skill.TransitionTo(state)` validates transitions.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -run "TestSkillState|TestHookPhase" -v`
Expected: All 3 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/types.go internal/skills/types_test.go
git commit -m "[BEHAVIORAL] Add skill types, backend interface, lifecycle states, hook phases"
```

---

### Task 7: Lifecycle manager and hook dispatch

**Files:**
- Create: `internal/skills/hooks.go`
- Create: `internal/skills/hooks_test.go`

**Step 1: Write the failing tests**

6 test functions:
- `TestRegisterAndDispatchHook` - register handler, dispatch event, verify runs
- `TestDispatchHookMultipleSkills` - handlers run in priority order
- `TestDispatchHookNoHandlers` - returns nil result
- `TestBeforeToolCallCancel` - Cancel=true prevents execution
- `TestAfterToolResultModify` - modifies tool result content
- `TestBeforePromptBuildInject` - injects prompt fragment

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run "TestRegisterAnd|TestDispatchHook|TestBefore|TestAfter" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`LifecycleManager` with `handlers map[HookPhase][]skillHookEntry`. `skillHookEntry{skillName, priority, handler}`. Register, Unregister (by skill name), Dispatch. Dispatch iterates in priority order; for cancellable hooks returns early; for modifying hooks chains output.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -run "TestRegisterAnd|TestDispatchHook|TestBefore|TestAfter" -v`
Expected: All 6 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/hooks.go internal/skills/hooks_test.go
git commit -m "[BEHAVIORAL] Add lifecycle manager with hook registration and dispatch"
```

---

### Task 8: Public Go SDK (pkg/skillsdk/)

**Files:**
- Create: `pkg/skillsdk/sdk.go`
- Create: `pkg/skillsdk/sdk_test.go`

**Step 1: Write the failing tests**

3 test functions:
- `TestManifestType` - Manifest struct constructible
- `TestContextInterface` - compile-time interface check
- `TestMockContext` - mock implements interface

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/skillsdk/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`SkillPlugin` interface: `Manifest()`, `Activate(Context)`, `Deactivate(Context)`. `Context` interface with all SDK operations (ReadFile, WriteFile, Exec, Complete, Fetch, GitDiff, etc.). Supporting types: `ExecResult`, `FileInfo`, `GitCommit`, `GitFileStatus`, `Manifest`.

**Step 4: Run tests**

Run: `go test ./pkg/skillsdk/ -v`
Expected: All 3 tests PASS

**Step 5: Commit**

```bash
git add pkg/skillsdk/
git commit -m "[BEHAVIORAL] Add public Go SDK for skill plugin authors"
```

---

### Task 9: Starlark engine - basic execution

**Files:**
- Create: `internal/skills/starlark/engine.go`
- Create: `internal/skills/starlark/engine_test.go`

**Step 1: Write the failing tests**

4 test functions:
- `TestEngineExecSimple` - execute Starlark script with globals
- `TestEngineExecError` - syntax error returns error
- `TestEngineExecBuiltinAvailable` - SDK functions in scope
- `TestEngineExecRegisterTool` - `register_tool()` call, engine exposes via `Tools()`

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/starlark/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`Engine` struct with starlark thread, globals, collected tools/hooks, sandbox ref. `NewEngine(skillName, skillDir, sandbox)`. Implements `SkillBackend`. `Load` reads and executes the entrypoint .star file. Injects `register_tool`, `register_hook`, `log` as Starlark builtins. `register_tool` creates a `starlarkTool` wrapper implementing `tools.Tool`.

**Step 4: Add dependency and run tests**

Run: `go get go.starlark.net && go test ./internal/skills/starlark/ -v`
Expected: All 4 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/starlark/ go.mod go.sum
git commit -m "[BEHAVIORAL] Add Starlark engine with basic execution and register_tool"
```

---

### Task 10: Starlark SDK - file, shell, env built-ins

**Files:**
- Create: `internal/skills/starlark/builtins.go`
- Create: `internal/skills/starlark/builtins_test.go`

**Step 1: Write the failing tests**

9 test functions:
- `TestBuiltinReadFile` - returns file contents
- `TestBuiltinReadFilePermissionDenied` - fails without file:read
- `TestBuiltinWriteFile` - creates file
- `TestBuiltinListDir` - returns entries
- `TestBuiltinSearchFiles` - glob matching
- `TestBuiltinExec` - returns stdout
- `TestBuiltinExecPermissionDenied` - fails without shell:exec
- `TestBuiltinEnv` - returns env var value
- `TestBuiltinProjectRoot` - returns project root

Use temp directories with pre-approved sandbox permissions.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/starlark/ -run TestBuiltin -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Each built-in is a `starlark.Builtin` that: checks sandbox permission, executes operation, returns Starlark value. `read_file` -> `starlark.String`. `list_dir` -> `starlark.List`. `exec` -> struct with stdout/stderr/exit_code. `env` -> `starlark.String`. `project_root` -> `starlark.String`.

**Step 4: Run tests**

Run: `go test ./internal/skills/starlark/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/skills/starlark/
git commit -m "[BEHAVIORAL] Add Starlark SDK built-ins for file, shell, env operations"
```

---

### Task 11: Starlark SDK - LLM, network, git built-ins

**Files:**
- Modify: `internal/skills/starlark/builtins.go`
- Modify: `internal/skills/starlark/builtins_test.go`

**Step 1: Write the failing tests**

8 test functions using mock providers:
- `TestBuiltinLLMComplete` - mock provider, returns text
- `TestBuiltinLLMCompletePermissionDenied` - fails without llm:call
- `TestBuiltinFetch` - mock HTTP server, returns response
- `TestBuiltinFetchPermissionDenied` - fails without net:fetch
- `TestBuiltinGitDiff` - temp git repo, returns diff
- `TestBuiltinGitLog` - returns commits
- `TestBuiltinGitStatus` - returns file statuses
- `TestBuiltinInvokeSkill` - mock invoker, returns result

Inject mock `LLMCompleter`, `HTTPFetcher`, `GitRunner`, `SkillInvoker` interfaces into engine.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/starlark/ -run "TestBuiltinLLM|TestBuiltinFetch|TestBuiltinGit|TestBuiltinInvoke" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Define interfaces: `LLMCompleter`, `HTTPFetcher`, `GitRunner`, `SkillInvoker`. Add to Engine struct. Implement built-ins using these interfaces with sandbox permission checks.

**Step 4: Run tests**

Run: `go test ./internal/skills/starlark/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/skills/starlark/
git commit -m "[BEHAVIORAL] Add Starlark SDK built-ins for LLM, network, git, skill invocation"
```

---

### Task 12: Starlark SDK - register_hook, register_workflow, register_scanner

**Files:**
- Modify: `internal/skills/starlark/engine.go`
- Modify: `internal/skills/starlark/engine_test.go`

**Step 1: Write the failing tests**

5 test functions:
- `TestRegisterHook` - registers hook, engine exposes via `Hooks()`
- `TestRegisterHookInvalidPhase` - unknown phase returns Starlark error
- `TestRegisterWorkflow` - registers workflow, engine exposes via `Workflows()`
- `TestRegisterScanner` - registers scanner, engine exposes via `Scanners()`
- `TestHookHandlerCalled` - activate, verify OnActivate handler runs

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/starlark/ -run "TestRegisterHook|TestRegisterWorkflow|TestRegisterScanner|TestHookHandler" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add `register_hook(phase, handler)`, `register_workflow(name, handler)`, `register_scanner(name, handler)` Starlark builtins. Each stores the callable. Engine wraps them: hook callable -> `HookHandler` func, workflow callable -> workflow runner, scanner callable -> scanner func.

**Step 4: Run tests**

Run: `go test ./internal/skills/starlark/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/skills/starlark/
git commit -m "[BEHAVIORAL] Add register_hook, register_workflow, register_scanner to Starlark SDK"
```

---

### Task 13: Go plugin backend

**Files:**
- Create: `internal/skills/goplugin/goplugin.go`
- Create: `internal/skills/goplugin/goplugin_test.go`
- Create: `internal/skills/goplugin/testdata/testplugin.go`

**Step 1: Write the failing tests**

5 test functions:
- `TestLoadPlugin` - load test .so, verify SkillBackend interface
- `TestPluginTools` - plugin registers tools
- `TestPluginActivateDeactivate` - lifecycle methods called
- `TestLoadPluginInvalidPath` - error for missing .so
- `TestLoadPluginMissingSymbol` - error if NewSkill not exported

Build test plugin in TestMain.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/goplugin/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`GoPluginBackend` implements `SkillBackend`. `Load` calls `plugin.Open()`, looks up `NewSkill` symbol, casts to `func() skillsdk.SkillPlugin`, creates context backed by sandbox, calls `Activate(ctx)`.

**Step 4: Run tests**

Run: `go test ./internal/skills/goplugin/ -v`
Expected: All 5 tests PASS (platform-gated)

**Step 5: Commit**

```bash
git add internal/skills/goplugin/
git commit -m "[BEHAVIORAL] Add Go plugin backend for native skill loading"
```

---

### Task 14: External process backend - JSON-RPC protocol

**Files:**
- Create: `internal/skills/process/protocol.go`
- Create: `internal/skills/process/protocol_test.go`

**Step 1: Write the failing tests**

7 test functions:
- `TestEncodeRequest` - JSON-RPC 2.0 format
- `TestDecodeResponse` - success response
- `TestDecodeResponseError` - error response
- `TestEncodeInitialize` - initialize method
- `TestEncodeToolExecute` - tool/execute method
- `TestEncodeHookHandle` - hook/handle method
- `TestEncodeShutdown` - shutdown method

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/process/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`JSONRPCRequest{JSONRPC, ID, Method, Params}`, `JSONRPCResponse{JSONRPC, ID, Result, Error}`, `RPCError{Code, Message}`. Encode/Decode functions using `encoding/json`.

**Step 4: Run tests**

Run: `go test ./internal/skills/process/ -v`
Expected: All 7 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/process/
git commit -m "[BEHAVIORAL] Add JSON-RPC 2.0 protocol types for external process backend"
```

---

### Task 15: External process backend - ProcessManager

**Files:**
- Create: `internal/skills/process/manager.go`
- Create: `internal/skills/process/manager_test.go`
- Create: `internal/skills/process/testdata/echo_skill.go`

**Step 1: Write the failing tests**

5 test functions:
- `TestProcessStartStop` - start test process, initialize, stop
- `TestProcessToolExecute` - send tool/execute, get result
- `TestProcessHookHandle` - send hook/handle, get result
- `TestProcessCrashRestart` - kill process, verify restart
- `TestProcessTimeout` - slow process triggers timeout

Build `testdata/echo_skill.go` (a Go program that reads JSON-RPC from stdin, responds on stdout) in TestMain.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/process/ -run TestProcess -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`ProcessBackend` implements `SkillBackend`. Starts child process, communicates via JSON-RPC over stdin/stdout. `call(method, params)` sends request and reads response with timeout. Tools and hooks created from initialize response.

**Step 4: Run tests**

Run: `go test ./internal/skills/process/ -v`
Expected: All 5 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/process/
git commit -m "[BEHAVIORAL] Add external process backend with JSON-RPC communication"
```

---

### Task 16: Runtime orchestrator

**Files:**
- Create: `internal/skills/runtime.go`
- Create: `internal/skills/runtime_test.go`

**Step 1: Write the failing tests**

6 test functions using mock backends:
- `TestRuntimeDiscoverAndActivate` - discovery + activation flow
- `TestRuntimeTriggerActivation` - trigger context activates matching skill
- `TestRuntimePermissionDenied` - skill stays inactive on denial
- `TestRuntimeDeactivate` - tools/hooks removed
- `TestRuntimeGetActiveSkills` - returns active list
- `TestRuntimeToolRegistration` - activated skill's tools in registry

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run TestRuntime -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`Runtime` ties together Loader, Store, Sandbox, LifecycleManager, tools.Registry, skill map. `Discover` calls loader. `Activate` transitions state, creates backend, loads, checks permissions, registers tools/hooks. `Deactivate` reverses. `EvaluateTriggers` calls trigger engine then activates matches.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -run TestRuntime -v`
Expected: All 6 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/runtime.go internal/skills/runtime_test.go
git commit -m "[BEHAVIORAL] Add skill runtime orchestrator with discovery, activation, hook dispatch"
```

---

### Task 17: Five skill type integrations

**Files:**
- Create: `internal/skills/integration.go`
- Create: `internal/skills/integration_test.go`

**Step 1: Write the failing tests**

6 test functions:
- `TestToolSkillRegistersTools` - tools appear in registry
- `TestPromptSkillInjectsFragment` - prompt added via OnBeforePromptBuild
- `TestWorkflowSkillInvokable` - workflow invokable by name
- `TestSecurityRuleSkillRegisters` - scanner registered via adapter
- `TestTransformSkillModifiesOutput` - response modified via OnAfterResponse
- `TestMultiTypeSkill` - types: [tool, prompt] registers both

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run "TestToolSkill|TestPromptSkill|TestWorkflowSkill|TestSecurityRule|TestTransformSkill|TestMultiType" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`SecurityRuleAdapter` stores scanner registrations (adapter only, no engine). `PromptCollector` gathers fragments from active prompt skills. `WorkflowRunner` executes named workflows. Runtime's `Activate` checks types and: tool->registers tools, prompt->registers OnBeforePromptBuild hook, workflow->stores handler, security-rule->adapter, transform->registers OnAfterResponse hook.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/skills/integration.go internal/skills/integration_test.go
git commit -m "[BEHAVIORAL] Add integrations for all 5 skill types"
```

---

### Task 18: Agent integration - wire runtime into agent loop

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/agent_skills_test.go`
- Modify: `cmd/rubichan/main.go`

**Step 1: Write the failing tests**

4 test functions:
- `TestAgentWithSkillRuntime` - agent accepts skill runtime, skill tools appear in completions
- `TestAgentBeforeToolCallHook` - hook intercepts and modifies tool call
- `TestAgentAfterToolResultHook` - hook modifies tool result
- `TestAgentPromptInjection` - prompt fragment included in system prompt

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestAgentWith -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add optional `*skills.Runtime` field to `Agent`. In `runLoop`: before completion, dispatch `OnBeforePromptBuild` and append fragments to system prompt; before tool execution, dispatch `OnBeforeToolCall` (cancel if requested); after tool result, dispatch `OnAfterToolResult`; after response, dispatch `OnAfterResponse`.

In `cmd/rubichan/main.go`: create store, loader, runtime; discover skills; evaluate triggers; pass runtime to agent. Add `--skills` and `--approve-skills` flags.

**Step 4: Run tests**

Run: `go test ./internal/agent/ -v && go test ./cmd/rubichan/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/agent/ cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire skill runtime into agent loop with hook dispatch"
```

---

### Task 19: Registry client

**Files:**
- Create: `internal/skills/registry.go`
- Create: `internal/skills/registry_test.go`

**Step 1: Write the failing tests**

6 test functions using `httptest.NewServer`:
- `TestRegistrySearch` - returns matching skills
- `TestRegistryGetManifest` - fetch manifest for name+version
- `TestRegistryDownload` - download tarball, extract to dir
- `TestRegistryCachingHit` - second call uses SQLite cache
- `TestRegistryCachingExpired` - expired cache refetches
- `TestRegistryGitInstall` - clone git URL, validate SKILL.yaml

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/ -run TestRegistry -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`RegistryClient{baseURL, store, cacheTTL}`. `Search(query)` hits `/api/v1/search?q=`. `GetManifest(name, version)` hits `/api/v1/skills/{name}/{version}`. `Download(name, version, dest)` hits download URL, extracts tarball. `InstallFromGit(url, dest)` runs `git clone`, validates SKILL.yaml. Caching via store's registry_cache table.

**Step 4: Run tests**

Run: `go test ./internal/skills/ -run TestRegistry -v`
Expected: All 6 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/registry.go internal/skills/registry_test.go
git commit -m "[BEHAVIORAL] Add registry client with search, download, git install, caching"
```

---

### Task 20: Skill CLI - list, info, search

**Files:**
- Create: `cmd/rubichan/skill.go`
- Create: `cmd/rubichan/skill_test.go`

**Step 1: Write the failing tests**

3 test functions:
- `TestSkillListCommand` - outputs installed skills table
- `TestSkillInfoCommand` - outputs manifest details for a skill
- `TestSkillSearchCommand` - outputs registry search results

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/rubichan/ -run TestSkill -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add `skillCmd` as Cobra subcommand. `list` loads store, prints installed skills. `info <name>` loads manifest from skill dir, prints details. `search <query>` calls registry client. Add `skillCmd` to rootCmd in main.go.

**Step 4: Run tests**

Run: `go test ./cmd/rubichan/ -run TestSkill -v`
Expected: All 3 tests PASS

**Step 5: Commit**

```bash
git add cmd/rubichan/skill.go cmd/rubichan/skill_test.go
git commit -m "[BEHAVIORAL] Add skill CLI subcommands: list, info, search"
```

---

### Task 21: Skill CLI - install, remove, add

**Files:**
- Modify: `cmd/rubichan/skill.go`
- Modify: `cmd/rubichan/skill_test.go`

**Step 1: Write the failing tests**

5 test functions:
- `TestSkillInstallLocal` - copies from local path
- `TestSkillInstallFromRegistry` - downloads from registry
- `TestSkillInstallVersion` - name@version format
- `TestSkillRemove` - deletes from skills dir
- `TestSkillAdd` - copies to project .agent/skills/

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/rubichan/ -run "TestSkillInstall|TestSkillRemove|TestSkillAdd" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`install` parses source (local path, git URL, registry name@version), copies/downloads, validates SKILL.yaml, stores state. `remove` deletes directory and store entry. `add` copies to `.agent/skills/`.

**Step 4: Run tests**

Run: `go test ./cmd/rubichan/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add cmd/rubichan/skill.go cmd/rubichan/skill_test.go
git commit -m "[BEHAVIORAL] Add skill CLI subcommands: install, remove, add"
```

---

### Task 22: Skill CLI - create, test, permissions

**Files:**
- Modify: `cmd/rubichan/skill.go`
- Modify: `cmd/rubichan/skill_test.go`

**Step 1: Write the failing tests**

4 test functions:
- `TestSkillCreate` - scaffolds directory with SKILL.yaml + skill.star
- `TestSkillTest` - loads and validates a skill
- `TestSkillPermissions` - lists approvals
- `TestSkillPermissionsRevoke` - clears approvals

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/rubichan/ -run "TestSkillCreate|TestSkillTest|TestSkillPermissions" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

`create`: make dir, write template SKILL.yaml and skill.star. `test`: parse manifest, attempt backend load. `permissions`: query store, print table. `permissions --revoke`: call store.Revoke().

**Step 4: Run tests**

Run: `go test ./cmd/rubichan/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add cmd/rubichan/skill.go cmd/rubichan/skill_test.go
git commit -m "[BEHAVIORAL] Add skill CLI subcommands: create, test, permissions"
```

---

### Task 23: Built-in skills - core-tools and git

**Files:**
- Create: `internal/skills/builtin/core_tools.go`
- Create: `internal/skills/builtin/core_tools_test.go`
- Create: `internal/skills/builtin/git.go`
- Create: `internal/skills/builtin/git_test.go`

**Step 1: Write the failing tests**

4 test functions:
- `TestCoreToolsManifest` - valid manifest, types=[tool]
- `TestCoreToolsRegistersFileShell` - exposes file and shell tools
- `TestGitManifest` - valid manifest, types=[tool]
- `TestGitRegistersGitTools` - exposes git-diff, git-log, git-status

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/skills/builtin/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Built-in skills return a `SkillManifest` and a `SkillBackend` without filesystem discovery. `core-tools` wraps existing `tools.NewFileTool()` and `tools.NewShellTool()`. `git` creates tools that shell out to `git diff`, `git log`, `git status`.

**Step 4: Run tests**

Run: `go test ./internal/skills/builtin/ -v`
Expected: All 4 tests PASS

**Step 5: Commit**

```bash
git add internal/skills/builtin/
git commit -m "[BEHAVIORAL] Add built-in skills: core-tools and git"
```

---

### Task 24: Example skills

**Files:**
- Create: `examples/skills/kubernetes/SKILL.yaml`
- Create: `examples/skills/kubernetes/skill.star`
- Create: `examples/skills/ddd-expert/SKILL.yaml`
- Create: `examples/skills/ddd-expert/prompts/system.md`
- Create: `examples/skills/rfc-writer/SKILL.yaml`
- Create: `examples/skills/rfc-writer/skill.star`
- Create: `examples/skills/examples_test.go`

**Step 1: Write the failing tests**

1 test function with 3 subtests:
- `TestExampleManifestsValid/kubernetes` - SKILL.yaml parses and validates
- `TestExampleManifestsValid/ddd-expert` - SKILL.yaml parses and validates
- `TestExampleManifestsValid/rfc-writer` - SKILL.yaml parses and validates

**Step 2: Run tests to verify they fail**

Run: `go test ./examples/skills/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- kubernetes: type=tool, backend=starlark, skill.star registers kubectl_get tool
- ddd-expert: type=prompt, backend=starlark, prompts/system.md with DDD expertise
- rfc-writer: type=workflow, backend=starlark, skill.star registers rfc-writer workflow

**Step 4: Run tests**

Run: `go test ./examples/skills/ -v`
Expected: All 3 subtests PASS

**Step 5: Commit**

```bash
git add examples/skills/
git commit -m "[BEHAVIORAL] Add example skills: kubernetes, ddd-expert, rfc-writer"
```

---

### Task 25: Config integration for skills

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing tests**

3 test functions:
- `TestConfigWithSkillsSection` - parses [skills] from TOML
- `TestConfigSkillsDefaults` - default registry URL, empty approved
- `TestConfigSkillsApproved` - approved_skills list parsed

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestConfigWith -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add `SkillsConfig` to `Config`: `RegistryURL`, `ApprovedSkills`, `UserDir`, `RateLimits` (MaxLLMCallsPerTurn, MaxShellExecPerTurn, MaxNetFetchPerTurn). Set defaults in `DefaultConfig()`.

**Step 4: Run tests**

Run: `go test ./internal/config/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "[BEHAVIORAL] Add skills configuration section with registry URL and rate limits"
```

---

### Task 26: Full integration tests

**Files:**
- Create: `internal/skills/integration_full_test.go`

**Step 1: Write the integration tests**

4 test functions:
- `TestFullLifecycleStarlark` - create temp skill, discover, trigger, approve, activate, call tool, deactivate
- `TestFullLifecycleProcess` - same with external process backend
- `TestHookChainEndToEnd` - 2 skills with OnBeforeToolCall hooks, verify priority order
- `TestPermissionDenialEndToEnd` - unapproved permission blocks execution

**Step 2: Run all tests**

Run: `go test ./... -v`
Expected: All packages PASS

**Step 3: Commit**

```bash
git add internal/skills/integration_full_test.go
git commit -m "[BEHAVIORAL] Add full integration tests for skill lifecycle"
```
