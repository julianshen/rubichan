# Modular Redesign Review — A Minimal Core with Modules

**Status:** Proposal / design review
**Date:** 2026-07-16
**Question:** Can we redesign Rubichan around a *minimal core with modules*, so that we don't have to bind everything together (TUI, skills, security, ACP, …) into one unit — making the agent easy to extend and easy to embed in other apps? Inspired by the design philosophy of [pi.dev](https://pi.dev/).

---

## 1. TL;DR

**Yes — and the codebase is already halfway there, but the two halves have drifted apart.**

- The *dependency direction* is mostly correct: the TUI, provider, and tool layers point **at** the core, not the other way around. `internal/agent` does **not** import Bubble Tea or the TUI, and the core talks to the outside world through a channel of `TurnEvent`s. That is the right shape.
- The *core itself is not minimal*. `internal/agent.Agent` has become a **god object**: a 2,910-line `agent.go`, a struct with **~60 fields**, and **35 `With…` constructor options** that bind in checkpointing, knowledge graph, persona, memory, auto-dream, compaction strategies, ACP, budget, wake manager, prefetch, and more. You cannot take "just the loop" without transitively dragging in ~18 internal packages.
- There are effectively **two divergent agent stacks**. The *type contracts* are shared — `internal` aliases the SDK types (`tools.Tool = agentsdk.Tool`, `provider.LLMProvider = agentsdk.LLMProvider`, `sdk_aliases.go` re-exports the rest) — but the *behavior* is duplicated:
  1. `pkg/agentsdk` — a clean, public, **506-line** minimal agent loop with **zero `internal/` imports**, plus its own concrete `Registry`. This is the "embed in other apps" story.
  2. `internal/agent` — the **2,910-line** monolith the real app (TUI, headless, wiki, ACP) actually runs on, with a separate `runLoop` and a separate concrete `internal/tools.Registry`.
  So the *interfaces* line up but the *loop is written twice* and has diverged. External embedders get the toy loop; the real capability is locked in `internal/`.
- `cmd/rubichan/main.go` is **3,383 lines** and imports ~40 internal packages — this is the actual "bind everything together" point.

**Recommendation:** Collapse to **one** minimal core that both the SDK and the full app share, and move every optional subsystem behind a small set of **module extension seams** (tool provider, middleware/hooks, context strategy, event sink, transport). This is exactly pi.dev's "small core with programmable edges," and it is an *incremental, structural* refactor — no behavior change required to start.

---

## 2. What "good" looks like — the pi.dev model

Pi decomposes into four deliberately decoupled packages ([pi.dev](https://pi.dev/), [mariozechner.at](https://mariozechner.at/posts/2025-11-30-pi-coding-agent/)):

| pi package | Responsibility | Rubichan analogue |
|---|---|---|
| `pi-ai` | Provider abstraction over many LLM APIs, streaming, tool-calling, serialization | `internal/provider` + `pkg/agentsdk` provider iface — **already clean** |
| `pi-agent-core` | The loop: tool execution, validation, event emission, **transport abstraction** (direct / JSON-stream / RPC) | *split & duplicated* across `pkg/agentsdk` and `internal/agent` |
| `pi-tui` | A **replaceable** rendering layer built *on top of* the core | `internal/tui` — **already only imported by `main`** |
| `pi-coding-agent` | The harness that wires everything: config, sessions, slash commands, context files | `cmd/rubichan` — **3,383-line `main.go`, the binding point** |

Principles worth stealing:

1. **Small core, programmable edges.** The core ships a loop and a handful of tools; *everything else* (todos, plan mode, sub-agents, background jobs) is externalized to the filesystem or CLI rather than baked in. "What you leave out matters more than what you put in."
2. **The core is an SDK first, a CLI second.** `pi-agent-core` runs headless via JSON streaming / RPC. The TUI is one consumer among many.
3. **Tools return dual-format results** — text for the LLM, structured data for the UI — so no UI type leaks into the core.
4. **Fewer tools compose better.** Pi ships 4 tools (`read`, `write`, `edit`, `bash`) and lets the model reach everything else through bash. Rubichan currently registers **~36** distinct tools.

The goal is **not** to shrink Rubichan to four tools — it has real, differentiated capability (security engine, wiki pipeline, skills, knowledge graph). The goal is to make those capabilities **modules that plug into a small core**, not fields welded onto a god object.

---

## 3. Current-state assessment

### 3.1 What is already right — keep it

| Property | Evidence |
|---|---|
| Core does **not** depend on the TUI | `grep bubbletea internal/agent` → none. Only `cmd/rubichan/main.go` imports `internal/tui`. |
| Core speaks to the outside via **events**, not UI calls | `Agent.Turn()` returns `<-chan TurnEvent`; the TUI/headless/wiki adapters consume the channel. |
| Provider layer is **interface-based & self-registering** | `LLMProvider` interface (`pkg/agentsdk/provider.go:6`); concretes register via `init()` + `RegisterProvider` (`internal/provider/factory.go`). Adding a provider needs no core change — the cleanest seam in the codebase. |
| Tool registration is **interface-based** | `Tool` interface + `Registry.Register(Tool)` (`internal/tools/registry.go`). |
| SDK **type contracts are unified via aliases** | `internal/tools/interface.go:10` (`Tool = agentsdk.Tool`), `internal/provider/types.go:10` (`LLMProvider = agentsdk.LLMProvider`), `internal/agent/sdk_aliases.go`. The *types* are already one set — only the *loop* is forked. |
| ACP package is **standalone** | `internal/acp` does not import `internal/agent` or `internal/tui`; no cycle. |
| A **public SDK** exists with the intended shape | `pkg/agentsdk` documents `NewAgent(provider, WithTools(…), …).Turn(ctx, msg)` and has zero `internal/` imports. |
| Spec **already mandates** this | ADR-002: *"No UI dependencies in core. Features injected via interfaces."* |

### 3.2 The problems — what blocks "minimal core + modules"

**Problem A — The core is a god object, not a minimal core.**

`internal/agent.Agent` (`internal/agent/agent.go`):
- `agent.go` alone is **2,910 lines**; the struct has **~60 fields**.
- **35** `With…` options on the constructor, each stapling in a concrete subsystem:
  `WithCheckpointManager`, `WithKnowledgeGraph`, `WithMemoryStore`, `WithSessionMemory`, `WithAutoDream`, `WithCompactionStrategies`, `WithCollapseStore`, `WithCacheBreakDetector`, `WithPrefetchManager`, `WithWakeManager`, `WithPersona`(MD), `WithACP`, `WithSkillRuntime`, `WithStore`, `WithUserHooks`, `WithStopHookRegistry`, … .
- The core transitively imports **~18** internal packages: `acp, checkpoint, config, evaluator, hooks, knowledgegraph, persona, provider, session, skills, store, text, toolexec, tools, …`.

Consequence: there is no "just the loop." Every feature is a field. Every new feature grows the struct and the option list. This is the opposite of pi's "small core."

**Problem B — Two divergent stacks (the most important finding).**

| Concept | Public SDK (`pkg/agentsdk`) | Real app (`internal/agent`) |
|---|---|---|
| Agent loop | `agent.go`, **506 lines**, 0 internal imports | `agent.go`, **2,910 lines**, ~18 internal imports |
| `Tool` interface | `pkg/agentsdk/tool.go` | **shared** via alias (`= agentsdk.Tool`) ✔ |
| `Registry` (concrete) | `pkg/agentsdk` Registry | **separate** `internal/tools.Registry` |
| Loop internals | own `runLoop`, `executeTools`, `requestToolApproval` | own `runLoop` (line ~1501), `executeTools`, approval, recovery… |

The good news: the **interfaces are already one set** — `internal/agent` imports `pkg/agentsdk` for types/enums (`Tool`, `LLMProvider`, `AgentDefinition`, `TurnExitReason`, …) and aliases them, so contracts don't drift. The bad news: the **loop is implemented twice**, and `internal/agent`'s loop does **not** call the SDK's. So the "public SDK" is a *parallel, simplified reimplementation* of the loop that has drifted from the code the product actually runs. An external app that embeds `pkg/agentsdk` today gets an agent with **no skills, no compaction, no checkpoint, no knowledge graph, no ACP** — none of what makes Rubichan Rubichan. This directly defeats "easy to bind with other apps."

**Problem C — `main.go` is the true monolith.**

`cmd/rubichan/main.go` is **3,383 lines** and imports ~40 internal packages, wiring providers, tools, skills (including four hard-coded builtin skill packages), security scanners/analyzers, knowledge graph, checkpoint, hooks, persona, ACP, and the TUI in one place. All construction/knowledge of the whole system lives in one file. Any new mode or embedder must re-derive this wiring.

**Problem D — Feature creep has no "edge."**

Capabilities that pi.dev deliberately keeps *outside* the core (todos, plan mode, sub-agents, background bash, prefetch, auto-dream) are all **core struct fields** here. There is no seam that says "this is a module." Adding a capability means editing the core.

**Problem E — The modular seam that exists is scaffolded but not adopted.**

`internal/modes/{interactive,headless,wiki}` are already the *intended* thin adapters — each is a small ACP client that imports only `internal/acp` + `pkg/agentsdk`, not `internal/agent` or the TUI. (161–297 lines counts each package's `acp_client.go` alone; the packages are larger — see the endgame audit below.) That is exactly the right shape. But grepping non-test references shows they are **effectively unwired**: the real interactive, headless, and wiki flows are implemented **inline in `main.go`** (headless around `main.go:2320`, wiki via `wireWiki`/`wiki.Run`, interactive via `tui.NewModel` binding straight to `internal/agent.Agent`). So there are two parallel interactive front-ends — a direct-binding TUI (production) and an ACP-mediated one (`modes/interactive`, dormant). Relatedly, several ACP handlers are explicit stubs (`handleToolExecute` → `"not_implemented"`; `Invoke`/`List`/`Scan` return placeholder values). The elegant "every mode is an ACP client over one core" design is **drawn but not finished** — which reads like good news for the redesign: the target shape is already sketched in the tree, and needs only to become the real path. **A later audit found that framing wrong, not merely optimistic: not one of the four adapter operations can complete against the server. See the Phase 3 endgame note below.**

---

## 4. Target architecture — one minimal core, modules at the edges

```
        ┌──────────────── Adapters / Harnesses (event sinks + transports) ─────────────┐
        │  TUI (Bubble Tea)   Headless runner   Wiki pipeline   ACP server   Web/RPC    │
        └───────────────┬──────────────┬──────────────┬───────────┬───────────┬─────────┘
                        │  consume TurnEvent stream + drive Transport            │
        ┌───────────────▼──────────────────────────────────────────────────────▼────────┐
        │                        CORE  (pkg/…, embeddable, tiny)                          │
        │   Agent loop  ·  Conversation  ·  Tool router  ·  Event stream  ·  Approval     │
        │                                                                                 │
        │   depends ONLY on these interfaces ───────────────────────────────────────────┐│
        │   • LLMProvider     • Tool / ToolRegistry     • Approval/UIRequest             ││
        │   • Middleware (before/after turn & tool)   • ContextStrategy   • EventSink     ││
        │   • Transport                                                                  ││
        └───────────────────────────────────────────────────────────────────────────────┘
                        ▲              ▲              ▲              ▲
        register at composition time (NOT compiled into the core)
        ┌───────────────┴───┐ ┌────────┴─────┐ ┌──────┴───────┐ ┌────┴──────────────────┐
        │ Provider modules  │ │ Tool modules │ │ Middleware   │ │ Context-strategy       │
        │ anthropic/openai/ │ │ file/shell/  │ │ modules:     │ │ modules:               │
        │ ollama            │ │ git/mcp/…    │ │ checkpoint,  │ │ compaction, memory,    │
        │                   │ │              │ │ hooks, eval, │ │ knowledge graph,       │
        │                   │ │              │ │ security     │ │ persona/prompt frags   │
        └───────────────────┘ └──────────────┘ └──────────────┘ └────────────────────────┘
```

**The core knows about a handful of interfaces and nothing else.** Every subsystem that is a field today becomes a **module** registered at composition time.

### 4.1 The extension seams (there should be ~7, not 35)

Most of today's 35 options collapse into a few well-named seams. The core defines the interface; modules implement it; the harness wires them.

> **Placement constraint:** every seam interface below must be defined in `pkg/` (or use only stdlib types), and `pkg/agentsdk` should keep **zero `internal/` imports**. Note the *reason* — it is **not** Go's `internal` visibility rule: because `pkg/agentsdk` sits under the same module root (`github.com/julianshen/rubichan`) as `internal/`, it is actually *permitted* to import `internal/…`, and an external module importing `pkg/agentsdk` still compiles fine. The constraint is architectural, not a compiler requirement: (a) **dependency inversion** — if the SDK imported `internal/skills`/`internal/toolexec`, the "core" would again be welded to concrete subsystems, defeating the whole redesign; and (b) **portability** — it keeps the door open to later extracting the SDK into its *own* Go module (`…/rubichan-sdk`), at which point internal imports genuinely *would* break external consumers. Modules *implement* the interfaces from `internal/`; the core only ever depends on the `pkg/` interface. This is the single hardest invariant to hold during Phase 1 (below) and the one that makes "embed the real agent elsewhere" actually work.

1. **`LLMProvider`** — already exists. Providers are modules. *(No change.)*
2. **`Tool` + `ToolRegistry`** — one interface, promoted to the public package; `internal/tools` implements/uses it instead of redefining it. Tools (incl. skills, MCP) are modules.
3. **`Middleware` (tool execution) + turn-level hooks** — two related but distinct seams:
   - *Tool-execution middleware* — a `before/after Tool` pipeline. **This pattern already exists** as `toolexec.Middleware` (`internal/toolexec/middleware.go`, with `CheckpointMiddleware`, `ClassifierMiddleware`, `HookMiddleware`, `PostHookMiddleware`, `OutputManagerMiddleware`). Use it as the model: **checkpointing, evaluator/classifier, security gating, output offloading, diff tracking** are middlewares, not fields. Promote its `Middleware` type to `pkg/`.
   - *Turn-level hooks* — `before/after Turn` events. Modeled by `internal/hooks` (user-configured shell/HTTP/prompt hooks) plus `internal/agent`'s stop-hook registry; generalize these into one event dispatcher.
4. **`ContextStrategy`** — pluggable context-window management. **Compaction strategies, memory injection, knowledge-graph selection, persona/prompt fragments** become strategies the core calls *synchronously* at prompt-build and post-turn time, rather than ~15 dedicated fields. (**Prefetch is *not* here** — see #7; it is async.)
5. **`EventSink` / observer** — the core already emits `TurnEvent`. Formalize it: TUI, headless formatter, wiki progress, ACP notifications, and session persistence are all just sinks. **`store`, `session`, `activity_summarizer`** attach here.
6. **`Transport`** — direct (in-proc), JSON-stream (headless/CI), and RPC/stdio (ACP). This is how the *same* core serves every mode and every embedder. ACP becomes *a transport*, not a core field.
7. **`BackgroundCoordinator` (async optimizations)** — `prefetch` (`internal/agent/prefetch.go`) is an **asynchronous** optimization: it spawns goroutines to load memory/skill context *in parallel with* the LLM call and consumes the result via a handle. It is a lifecycle/background concern, not a synchronous context strategy — grouping it under #4 would misrepresent its shape. Model it as a background coordinator that a `ContextStrategy` may *consume from*, keeping the sync/async boundary explicit. Auto-dream and other fire-and-forget work attach here too.

Rule of thumb after the redesign: **adding a capability adds a module, never a core struct field.**

### 4.2 Where the modes go

Interactive / Headless / Wiki / ACP stop being "modes the core knows about" (today `WithMode("interactive")` is a core string) and become **adapters**: an `EventSink` + a `Transport` + a tool/middleware selection. The mode-specific `acp_client.go` files (already thin, 161–297 lines) are the right size; they should sit *outside* the core and depend only on the public interfaces.

---

## 5. Migration path — incremental, structural-first, small PRs

This respects `CLAUDE.md`: **separate structural from behavioral changes, structural first, tests green at every step, small focused PRs.** No step below changes behavior; each is a refactor validated by the existing suite.

**Phase 0 — Unify the concrete `Registry` (structural, cheap).**
The `Tool` interface is already shared via alias — good. The remaining fork is the **concrete `Registry`** (`internal/tools.Registry` vs `pkg/agentsdk.Registry`). Pick one canonical registry (promote `internal/tools.Registry` to the public package, or make the SDK re-export it) and delete the other. One tool interface, one registry. *Low risk, removes a maintenance fork.*

**Phase 1 — One loop (the pivotal step).**
Make `internal/agent` build on the `pkg` core loop instead of reimplementing `runLoop`/`executeTools`/approval. Either (a) move the real loop into `pkg` and have the SDK use it directly, or (b) express the SDK as the core and `internal/agent` as core + modules. End state: **exactly one** agent loop. Delete `pkg/agentsdk/agent.go`'s parallel copy or promote it — do not keep both. This is what makes the "embed the *real* agent in another app" story true. **Watch the invariant:** the promoted core must keep zero `internal/` imports (see §4.1 placement constraint). It compiles either way *today* (same module root), so the regression is silent — an accidental `internal/skills`/`internal/toolexec` import re-welds the core to a concrete subsystem and pre-commits the future SDK-as-own-module split to breakage. Gate it in CI with an import-linter rule rather than relying on the compiler to catch it. **(Done, 2026-07: `TestPublicPackagesHaveNoInternalImports` in `pkg/agentsdk` walks the transitive import graph of every `pkg/...` package and fails with a blame chain on any `internal/` dependency — the gate runs with `go test`, no external linter. Verified to catch a leaf `internal/` import the compiler accepts. All public packages pass today.)**

> **Status update (post-implementation, 2026-07):** Steps 1–3 of this phase shipped as #298, #299, #300 — `StreamAccumulator`, `ApprovalFlow`, and the tool-execution core (`ExecuteTool` + event constructors) are now single, shared, zero-`internal/`-import implementations in `pkg/agentsdk`, used by both loops with byte-identical wire shapes and unmodified existing test suites throughout. `internal/agent.Agent` shrank from 2,910 to 2,786 lines.
>
> **A 4th step — literally making `internal/agent`'s `runLoop` call into `pkg/agentsdk`'s `runLoop`, or vice versa — turned out not to be a mechanical next step.** What remains of `internal/agent`'s `runLoop` (~615 lines) is not duplication with the SDK's `runLoop` (~60 lines); it's orchestration for roughly 15 internal-only subsystems that have no SDK counterpart at all: skill activation, compaction, prompt-fragment/cache-breakpoint assembly, tool deferral/budget, prefetch, provider retry+fallback+escalation (a real recovery state machine — `errorclass.Classify` + `withheldErrors` + `attemptRecovery` — vs. the SDK's simple context-overflow check on a structured provider interface; these are two different designs for the same problem, not duplicate code), no-progress detection, task_complete signaling, wake events, session memory, and auto-dream. Two concrete candidates were checked and rejected as unsafe merges: `makeDoneEvent` (internal's reads `a.context.Budget()` / `a.diffTracker`, both internal-only) and stream-error classification (structurally different mechanisms, not shared logic).
>
> **The real blocker: Phase 1's "one loop" goal is gated on Phase 2's module seams existing first**, not the other way around as originally sequenced above. Those ~15 subsystems *are* the feature modules Phase 2 describes extracting behind `Middleware` / `ContextStrategy` / `BackgroundCoordinator` / `Transport`. Until those seams exist, there is nothing for a shared loop to plug them into — the loop skeleton and the modules have to be designed together. Forcing a merge now would mean either (a) growing the SDK's public loop to embed 615 lines of internal-only orchestration (an immediate `internal/` import, violating the zero-import invariant this same section calls out), or (b) a big-bang rewrite inventing all of Phase 2's seams at once under one PR — exactly what §6 Risks warns against.
>
> **Revised plan:** treat "one loop" as complete for the parts that were genuinely safe to unify (done: accumulation, approval, execution — the primitives). Re-sequence the remainder as Phase 2 first (introduce one seam at a time, migrating one subsystem behind it per PR, same Tidy-First discipline as steps 1–3) and revisit full loop unification once enough subsystems have moved behind seams that what's left in `internal/agent.runLoop` is small enough to converge safely. Phase 1 and Phase 2 are therefore interleaved in practice, not sequential.

**Phase 2 — Extract feature modules out of the god struct (structural, one subsystem per PR).**
For each subsystem, introduce the right seam interface and move it behind it, replacing the `With…` field option with a `Use(module)` registration: checkpoint/evaluator/security → *tool-execution middleware* (`toolexec.Middleware`); knowledge-graph/memory/persona/prompt-fragments/compaction → *`ContextStrategy`*; prefetch/auto-dream → *`BackgroundCoordinator`* (async); ACP → *`Transport`*. Struct shrinks one subsystem at a time; tests stay green.

> **Status update (2026-07): Phase 2 complete — all four seams are in place.**
> *Middleware* (#302–#304): `Pipeline`/`Middleware` promoted to `pkg/agentsdk`; composition is agent-owned (`WithToolMiddlewares` slots around a core chain of canonicalize → hooks → checkpoint → fused verdict+offload), which revived three production subsystems that main.go's wholesale `WithPipeline` replacement had silently dropped; hook dispatch (before and after) has a single site in the pipeline.
> *BackgroundCoordinator*: `agentsdk.BackgroundTask` — started before each model call, joined after tool execution, signalled at session end on every loop exit. Prefetch and auto-dream moved behind it; their dedicated Agent fields are gone and the loop dispatches generically. Moving auto-dream fixed a latent placement defect (its trigger sat only on the max-turns exit, so normally-ending sessions never consolidated). Note: neither prefetch nor auto-dream is currently registered by `cmd/rubichan/main.go` — wiring them into the product is a pending product decision, separate from the seam.
> *Transport*: ACP is no longer a core field — `WithACP`/`ACPServer()` and the `acpServer`/`acpRegistry`/`useACP` fields are gone; `agent.NewACPServer(core)` composes the JSON-RPC server over a plain agent at the composition root. Notable: production (`cmd/rubichan/main.go`) had been constructing an ACP server on every run and never serving it — `ACPServer()` had zero non-test callers — so main simply dropped its `WithACP` lines.
> *BackgroundCoordinator addendum*: session-memory extraction (async, per the same rule that keeps prefetch out of ContextStrategy) moved onto the seam as a built-in task — its join counts each tool round and spawns the gated extraction; terminal tool turns now participate, fixing the silent loss of a session's final round.
> *ContextStrategy* (in progress, sliced): the prompt-build moment is done — `agentsdk.ContextStrategy` contributes system-prompt sections synchronously at prompt-build time (`WithContextStrategies`, per-strategy recover boundaries), and the four built-in dynamic sections (scratchpad, progress, knowledge-graph selection, cross-session memories) are now built-in strategies prepended in canonical order, collapsing their inline blocks in `buildSystemPromptWithFragments` into one dispatch loop. Compaction was already pluggable via `CompactionStrategy`/`SetStrategies`. Persona/static assembly is covered by `agentsdk.StaticPromptSource` (`WithStaticPromptSources`): construction-time cacheable sections rendered after the built-in System/Identity/Soul/AGENT.md/extra blocks, which stay as plain assembly — the seam's value is the extension point, and wrapping the built-ins would add code without deleting fields. The last slice — skill prompt fragments + the before-prompt-build hook mutation — was handled by **Extract Class rather than a new seam**: the hook can *replace the base system prompt wholesale*, a whole-prompt transform the append-only `ContributePromptSections` seam cannot express (the same limit that kept the fused `VerdictOffloadStage` out of pure middleware ordering). The ~130-line inline blob became `skillPromptContributor` (`internal/agent/skill_prompt.go`), shrinking `buildSystemPromptWithFragments` from ~180 lines to ~39 and giving the skill-runtime prompt coupling one named home. Third parties that want to contribute prompt content already have `ContextStrategy`; the contributor is the skill system's own integration, not a pluggable strategy.
>
> **Net effect of Phase 2:** every extension point now goes through a `pkg/agentsdk` interface instead of a dedicated `internal/agent.Agent` field or a hardcoded mode string; adding a capability adds a module, not a struct field. Three subsystems that were dead or defective in production were surfaced along the way (main.go's dropped middlewares, the never-served ACP server, auto-dream's max-turns-only trigger and session-memory's lost final round). Phase 1's "one loop" goal — deferred until enough subsystems sat behind seams — is now unblocked: what remains in `internal/agent.runLoop` after these extractions is small enough to converge with the SDK loop, which is the natural next step alongside Phase 3.

**Phase 3 — Adapters over the core.**
Reduce `cmd/rubichan/main.go` to composition only: build core, register modules, pick an adapter. Move mode wiring into `internal/modes/*` (or `pkg` for reusable ones). Target: `main.go` under a few hundred lines.

> **Status update (2026-07): Phase 3 started — five slices out, all extractions of things that were never composition.**
>
> The phase is being taken as a sequence of named clusters rather than one move, since main.go's contents are not homogeneous: some of it is genuinely composition (build a registry, pick a provider) and belongs where it is; the rest is subsystems that happen to be spelled inline.
>
> - **Slice 1 — `internal/diag`.** Process-level diagnostics: the session log, the JSONL event log, the stack dumps written on signal or panic (20 tests moved with it). File layout, permissions and log-writer swapping are not main's business.
> - **Slice 2 — `internal/folderaccess`.** The working-directory approval gate: prompt, interactive path, headless path. Whether an unapproved folder is a question or an error, and what `--approve-cwd`/`--auto-approve` mean, is product policy the entrypoint should call rather than contain. Its persistence dependency is a two-method `Store` interface declared in the package, so the policy does not import the persistence layer to state its own rules.
> - **Slice 3 — `internal/modelcheck`.** The `--test` flag's probes: report the capabilities rubichan believes a model has, then check the belief against the live endpoint with one completion and one tool call. What counts as a working model is product behaviour. This slice also shows where the phase's boundary actually falls — `runModelCapabilityTest` stayed in main.go, because loading config and building a provider *is* composition; only the probing moved, taking both as arguments.
> - **Slice 4 — `internal/skillruntime`.** Skill runtime construction: the loader, the integration objects, five backend adapters, the backend and sandbox factories, discovery, and trigger activation. Which backends exist, what each is handed, and which skills switch on are product behaviour. The composition inputs became an `Options` struct — config, mode, working directory, config directory, and the skill selection the flags resolved to.
> - **Slice 5 — `internal/subagents`.** The subagent system: the registry of agent definitions a task may spawn, the wake manager, the spawner, the worktree provider, and the task tools. Written twice — once in `runInteractive`, once in `runHeadless` — and policy rather than plumbing: which definitions ship built in, how config declarations are translated, whether a subagent gets its own worktree.
>
> **Measured: 3,383 lines before slice 1 (`5101d64`) → 2,582 now (`c9f5605`).** That is 801 lines, of which 788 is slice work; the rest is two unrelated commits that happened to land in the same file (#329's Ollama fix, +17; the provider-registry change, −30). Slice 5 alone accounts for 2,765 → 2,591, with its behavioural follow-ups taking it to 2,582. Against a target of "a few hundred" that is roughly a quarter of the distance — 801 done, ~2,280 to go — and the remaining ~2,280 lines are the harder part (the inline interactive/headless/wiki flows identified in Problem C, which this doc assumed would move onto the dormant `internal/modes/*` adapters — an assumption the endgame audit below overturns). Slice 4 also dropped **nine imports** from main.go — the four `builtin/` skill packages, all four skill backends, and `pkg/skillsdk` — which is the better measure of what moved: main.go no longer knows those subsystems exist. As the two unrelated commits above show, the line count moves independently of this work — treat it as a direction of travel, not a burn-down.
>
> **Slice 1 was a pure `[STRUCTURAL]` move** — bodies verbatim, tests moved with them, before-and-after test runs identical, which is what makes that shape cheap to review and safe to land alongside unrelated work in the same file.
>
> **Slice 2 started as one and did not stay one.** The move itself was verbatim, but giving the code a package boundary turned two of its implementation details into a published contract, and both were wrong:
>
> - `Prompt` wrapped the caller's `io.Reader` in a `bufio.Reader` and discarded it, so anything the stream offered after the response line was lost. Harmless while the reader was a local detail of main.go on a canonical-mode terminal (each `read(2)` ends at the newline); a real defect once the same `os.Stdin` is documented as passing to the TUI afterwards. Reachable today with piped stdin.
> - Replacing `bufio` then dropped its guard against readers that return `(0, nil)` forever, turning a prompt that errors into a prompt that hangs. Restored with the same 100-read threshold.
>
> Both went in as separate `[BEHAVIORAL]` commits after the structural one, per Tidy-First. The general lesson for the remaining slices: **an extraction is behaviour-preserving in the mechanical sense while still promoting private assumptions into public contracts.** Expect to find and fix a defect per slice, and expect the fix to be a separate commit rather than a reason to make the move impure.
>
> **Slice 3 was a clean move, and what it surfaced was dead code rather than a defect.** `Run`'s "Tool support: SKIPPED" branch is unreachable: `agentsdk.DefaultCapabilities` enables native tool use and no provider profile disables it, so `DetectCapabilities` returns true for every provider/model pair. The branch is kept — the capability is a real field the agent consults elsewhere — but it is now commented as untested for that reason rather than left looking like an oversight. Worth noting as a second thing extraction does reliably: **moving code into a package where its coverage is measured in isolation exposes which branches production never takes.** In main.go, at 3,000 lines, that signal was invisible.
>
> **Slice 4 surfaced duplication rather than a defect, which is the third pattern.** `createSkillRuntime` resolved the config directory itself with `os.UserHomeDir`, duplicating `configDir()`, even though all three of its callers had already resolved one — so the parameter replaced a computation rather than moving it. `registerBuiltinSkillPrompts` had two further callers in `skill.go` that built their own loaders; exporting it as `RegisterBuiltinPrompts` means the CLI subcommands and the runtime cannot drift over which built-ins exist. Neither was visible while the code sat in the same file as everything it duplicated. One contract did change rather than move: the old function checked `cfg == nil` *before* resolving the home directory, and the wrapper no longer resolves anything, so that ordering is gone. Every reachable case is identical — all three callers pass an already-resolved directory — but the extraction is not quite perfectly mechanical, and saying so is cheaper than having a reader discover it.
>
> Slice 4 also needed one refactoring to become testable: the backend routing was a forty-line closure capturing eight variables, reachable only by activating a skill of each backend type. Extracted as `newBackendFactory`, its arms are directly testable — including the default arm, where an unrecognised backend must be refused rather than falling through to the prompt-only path. Coverage went 35.8% → 94.4%, most of it in that one extraction. **Where a move stalls at a low coverage number, the obstacle is usually a closure that should have been a function.**
>
> **Slice 5's duplication had drifted three ways, and the fourth pattern is what that cost — which turned out to be less than it looked.** The subagent block existed in both long mode functions. `runHeadless` discarded the worktree manager `setupWorkingDir` returned and built a second over the same repository; interactive discarded agent-definition registration errors that headless logged; and headless skipped construction entirely when both task tools were disabled, where interactive did not.
>
> **The first of those was initially recorded here as a live defect, and that was wrong.** The claim was that two managers each enforced `MaxWorktrees` against their own count. `worktree.Manager` keeps no such count: `list` enumerates a shared `.rubichan/worktrees` directory, `Cleanup` applies the limit to that shared listing, and both instances take the same lock file. Given that both were built from the same root with the same `Config` and no lifecycle hook, the second was redundant. (Not that same-root managers are interchangeable in general — `Manager` does hold per-instance state in its `Config` and an optional `SetHookFunc` hook. The first correction overshot in that direction and was itself corrected in review.) Passing the session's manager removes a redundant construction; it does not fix a limit. Both reviewers caught the error independently after being asked to check it — the claim had been derived by reading the code rather than running it, and the reasoning was plausible and wrong.
>
> The **second** divergence was a real if quiet defect: a config declaring a duplicate agent name produced a subagent the user could not spawn, silently, in interactive only. The **third** turned out to matter in the opposite direction from expected — see below.
>
> **The sequencing lesson survived; the execution of it did not.** The extraction was supposed to parameterise every divergence so the move stayed behaviour-preserving, with each fix landing afterwards as a reviewable `[BEHAVIORAL]` commit. It parameterised two of three. The third — the construction gate — was silently resolved in headless's favour, which changed interactive: it had always resolved a worktree provider, and after the move it did so only when the task tools were enabled. Review caught it. **Parameterising divergences is the right technique; the failure mode is believing you have enumerated them all.** Diff each copy against the extracted result per call site, not against each other.
>
> The general point still holds: **two copies of a block in two long functions will drift, and nothing surfaces it until they are forced into one place.** What this slice adds is that the drift is easier to find than to characterise — the duplication was obvious, its consequences were not, and the confident-sounding version was wrong.

> **Audit (2026-08): the Phase 3 endgame has no destination to migrate to — the adapters do not speak the server's protocol.**
>
> After five slices, `main.go` is 2,582 lines and `runInteractive` (554) + `runHeadless` (435) are 38% of it. The plan for those has always been "make `internal/modes/*` the real path". Before starting that, the two paths were compared. They are not two implementations of the same thing, and the gap is not drift.
>
> **What the adapters actually are.** 991 lines of production code across the three packages — `headless` 165, `interactive` 665, `wiki` 161 — plus 1,090 lines of in-package tests (and 638 more in the sibling `test/` subpackages, for 1,728 in all), which pass at 24.5% / 56.6% / 39.5% statement coverage. They are thin JSON-RPC clients, not mode implementations. Their entire operational surface is `RunCodeReview` and `RunSecurityScan` (headless), `GenerateDocs` (wiki), and `Initialize`/`Prompt` plus session-management helpers (interactive). None of them composes a provider, a tool registry, skills, hooks, checkpointing or approval — which is what the 989 inline lines in `main.go` almost entirely consist of.
>
> **What the adapters ask for, and what the server answers.** This is the finding that matters, and it is more basic than anything about streaming. The full set of methods `agent.NewACPServer` registers is `agent/prompt` and `tool/execute` (`internal/agent/acp_handlers.go`), plus `skill/invoke`, `skill/list`, `skill/manifest`, `security/scan` and `security/approve` from the two capability groups. Against that, the adapters' four operations fare as follows:
>
> | adapter operation | method sent | outcome against the real server |
> |---|---|---|
> | `interactive.Prompt` | `agent/prompt` | **fails** — sends `{"turn": …}`; the handler reads `{"prompt": …}` and rejects the empty value with "prompt cannot be empty" |
> | `headless.RunCodeReview` | `agent/codeReview` | **fails** — not registered |
> | `wiki.GenerateDocs` | `wiki/generate` | **fails** — not registered |
> | `headless.RunSecurityScan` | `security/scan` | **fails** — reaches the handler, but sends an empty `Target`, which `Agent.Scan` rejects with "target cannot be empty". Its signature is `RunSecurityScan(interactive bool)`: there is no parameter through which a target could be passed, so it cannot be made to succeed without changing the API. |
>
> **Not one of the four can complete even its buffered request/response**, never mind stream. So the adapters are not a destination that needs one more capability; they are a client for a server that was never built. **Any migration onto them starts by rewriting them**, and the earlier framing of this audit — that streaming was *the* blocker — understated that.
>
> **Why no test caught it.** The `test/` subpackages do construct each client against a real `agent.NewACPServer` — but they assert only that construction succeeded and never issue a request. A wiring test that stops at `NewACPClient(...) != nil` passes identically whether or not the server implements a single method the client calls.
>
> **The streaming gap is real too, and is the second blocker.** `tool/execute` is an explicit stub returning `{"status": "not_implemented"}`, pointing callers at `agent/prompt`; `agent/prompt` runs a turn and **drains the entire event channel into an array before returning**, its own comment conceding that "full multi-turn support would require async event streaming." Note what that does and does not mean: sequential turns are fine — a buffered call per turn is exactly a REPL. What a buffered call cannot carry is anything *during* a turn: token-by-token output, and interactive tool approval, which needs a round trip mid-turn.
>
> **That gap is in the implementation, not the wire format.** `internal/acp/types.go` already models notifications — a `Notification` type plus `notifications/progress` and `notifications/log` — but nothing constructs one. `ResponseDispatcher` correlates strictly by request ID and drops any message that matches no pending request, and the server always replies with an ID-bearing `Response`. There is no server-initiated dispatch path. So the fix is server-initiated notification support in `internal/acp/{dispatcher,server}.go`, not a new protocol.
>
> **This corrects the Problem C framing**, which said the design "just needs to become the real path". The honest ordering is: (1) decide whether ACP gains server-initiated events, then rewrite the adapters against the methods the server actually serves, then migrate; or (2) decide the adapters are not the destination and extract the mode flows in place.
>
> **Option 2 deserves weight it has not been given.** The adapters are 991 lines that no production path exercises, whose tests never send them a request, and every one of whose operations would fail if they did. That is not scaffolding awaiting a capability; it is code that has never worked against the thing it targets. Deleting it is a legitimate outcome of this audit, and much cheaper than the migration the doc has been assuming.
>
> No recommendation is made here between (1) and (2) — that is a product call about whether ACP is a real external interface or an internal aspiration. What the audit settles is that **the choice cannot be deferred by doing more extraction**: the remaining 38% of `main.go` is exactly the part that depends on the answer.

**Phase 4 — Publish the module API.**
Document the ~7 seams in `pkg/…` with examples (`examples/` already exists). External apps now embed the **real** core and opt into exactly the modules they want (e.g. a NATS bridge with tools + checkpoint but no TUI).

> **Status update (2026-07): first embedder example landed.** `examples/embed` composes `agent.New` with three seams at once — a custom `ContextStrategy`, a `BackgroundTask`, and a tool-execution middleware — over a self-contained canned provider, running one turn with no TUI/headless/ACP and no API key. A race-clean integration test asserts each module actually fires (the strategy's section reaches the system prompt, the middleware wraps the tool call, the task observes start/join/end), making the "adding a capability adds a module" thesis executable and regression-guarded. **One reachability gap remains, documented in the example:** the registration options (`WithContextStrategies`, `WithBackgroundTasks`, `WithToolMiddlewares`, …) still live on `internal/agent`, so a *different-module* embedder cannot call them yet — the interfaces are already public in `pkg/agentsdk`, but the core constructor must move to `pkg/` (Phase 1's "one loop" end state) to close it. That makes Phase 1 the highest-leverage remaining work: it's what turns "embeddable within this module" into "embeddable by anyone."

> **Status update (2026-07): closing the reachability gap via Phase 1 option (b), not by moving `agent.New`.** Investigation showed the literal "move the core constructor to `pkg/`" is *not* a bounded step — it is the terminal state of the whole redesign. `internal/agent.New` returns the god struct (`*Agent`, importing ~15 internal-only subsystems) via internal `AgentOption func(*Agent)`; a `pkg/` function that constructed it would import `internal/`, tripping the §4.1 guard (`TestPublicPackagesHaveNoInternalImports`), and Go's `internal/` visibility rule blocks any external module from importing even a helper package that transitively touches it. So external embedding genuinely requires the *entire* core to be internal-free — the endpoint, not a PR. (Notably, three of `New`'s four inputs are already `pkg`-native aliases — `provider.LLMProvider = agentsdk.LLMProvider`, `tools.Registry = agentsdk.Registry` (Phase 0 done), `ApprovalFunc = agentsdk.ApprovalFunc`; only `*config.Config` and the god-struct return remain internal.) The safe, incremental route is the doc's own **Phase 1 option (b): grow the already-portable SDK loop (`pkg/agentsdk.Agent`, zero internal imports) into the embeddable core by wiring the public seams into it, one per PR.** That loop shipped honoring none of the four seams; it now honors **`ContextStrategy`** (`agentsdk.WithContextStrategies`) — the system prompt is rebuilt each iteration as base + every strategy's non-blank sections, mirroring `internal/agent`'s dynamic-section behavior (blank-skip, per-strategy recover, unmutated stored prompt) — and **`BackgroundTask`** (`agentsdk.WithBackgroundTasks`): `StartTurn` before each model call so async work overlaps the provider round-trip, joins after tool execution on every path that executed tools (including the cancelled batch), and `EndSession` once per loop exit on its own goroutine with a fresh context. Each stage has its own recover boundary — turn-goroutine panics would abort the user's turn and starve siblings; the unsupervised `EndSession` goroutine would take down the process. — and **tool-execution middleware** (`agentsdk.WithToolMiddlewares`): execution routes through `agentsdk.Pipeline` with the shared execution core as base handler, so an embedder can gate, trace, or rewrite every tool call. That last slice needed **no new public API** — `internal/toolexec`'s `Middleware`/`HandlerFunc`/`ToolCall`/`Result`/`Pipeline` are all type aliases to the `agentsdk` ones, so the pkg-native middleware type was already the canonical one. Registration is a single ordered list (first outermost) rather than the internal agent's before/after slots, which exist only because it owns a fixed core chain (hooks, checkpoint, verdict) to splice around. Middlewares wrap execution only: a call the approval flow denies never reaches them.

**The portable core now honors all four seams.** An out-of-module program can attach dynamic prompt content, concurrent work, and tool-execution wrappers to the real core with no `internal/` dependency.

> **Status update (2026-07): the payoff claim is now executable and guarded.** `examples/embed` composes **`agentsdk.NewAgent`** using only public packages — the reachability gap it used to document in its own header is closed. Its integration tests pass *verbatim* against the new core (same `[start join start end]` lifecycle, same single middleware-wrapped tool call, same strategy section reaching the system prompt), which is the evidence that swapping `internal/agent.New` for the portable core preserved observable behavior rather than merely compiling. A second gate, `TestPortableExampleHasNoInternalImports`, walks the example's transitive imports and fails on any `internal/` dependency — verified to have teeth the same way §4.1's gate was: adding an `internal/tools` import to the example **builds cleanly** (the compiler permits it within the module) while the guard fails with a blame chain. "A different module can embed the real agent, with modules" is therefore a checked fact, not a doc comment.
>
> **What remains for full Phase 1.** This does not make the *production* agent portable: `internal/agent.Agent` still owns the ~15 internal-only subsystems (skills, compaction, prefetch, provider retry/fallback, wake, checkpoints, …) and all three modes still build on it. Two loops still exist, contrary to Phase 1's "exactly one" end state. What changed is that the portable loop is now a genuine embedding target rather than a stub, so the remaining convergence work can proceed against a core that already has the seams to plug subsystems into.

> **Status update (2026-07): the convergence decision — "exactly one loop" stays the target; it is deferred again, with a concrete next step rather than a date.**
>
> Phase 2's seams are in place and `internal/agent.runLoop` is down from ~615 lines to 160 (#317, #319–#321, four `[STRUCTURAL]` extractions). That was the precondition this section set for revisiting convergence, so the question was finally answerable on evidence.
>
> **The skeletons already match** at the level that matters for convergence: guard the context → prepare the request → start background work → call the provider → consume the stream → assemble the assistant turn → execute tools → repeat. `pkg/agentsdk.runLoop` (82 lines) is `internal/agent.runLoop` with every extension point removed. This is a shared high-level shape, not a beat-for-beat identity — `internal/agent` also runs `runStopHooks` as a distinct branch between assembly and the tool phase, which is one of the extension points a merged loop would have to express. The remaining textual difference is not duplicated logic; it is (a) six internal-only preparation steps before the model call — skill activation, compaction, prompt-fragment and cache-breakpoint assembly, tool deferral and budget, capability latches, window measurement — and (b) the fact that internal's beats can return a *retry* outcome (`loopStepOutcome`) where the SDK's can only proceed or return, because internal's provider call is a recovery state machine and the SDK's is a single structured-error check.
>
> **Merging the bodies would cost more than it saves.** The shared part is roughly twenty lines of sequencing. Hosting internal's beats in the public loop needs about five further seams (pre-turn preparation, provider-call recovery, turn assembly, stop hooks, tool-phase policy) on top of the four Phase 2 delivered — and every embedder would then pay the indirection for extension points they do not use. The SDK loop's readability is not incidental; it is what makes it a credible embedding target.
>
> **But the fork has already cost correctness, which is the part "one loop" was really protecting.** Five defects were found in `internal/agent`'s cancellation path across #322 and #324. Both defects in `pkg/agentsdk.executeTools` — the one step that was never unified — are still unfixed there, because nothing propagates a fix across the fork:
>
> 1. **No orphan sealing — live.** `pkg/agentsdk.executeTools` returns on cancellation leaving the remaining `tool_use` blocks unanswered, and the package has no `synthesizeMissingToolResults` equivalent anywhere. The conversation is created once in `NewAgent` and every `Turn` appends to it, so the next `Turn` sends an unanswered `tool_use` to the provider and fails a protocol check. This is #322's defect, and it bites an embedder the first time a user cancels mid-batch.
> 2. **Trailing-cancel blindness — latent.** The same function tests `ctx.Err()` only at the top of its loop and returns `false` at the end, so a cancellation landing during the final tool is misclassified as a clean batch. Unlike `internal/agent` — where the `task_complete` path acted on that answer immediately and reported `ExitTaskComplete` — the SDK loop's next-iteration context check emits the same error and done events one beat later, so today there is no observable difference. The misclassification is real; its consequence is not, until some caller acts on the return value the way `runToolPhase` does.
>
> That is measured divergence in the exact code path that consumed two PRs and five review findings — one live defect and one latent one, in the single step that stayed forked while the primitives around it were unified.
>
> **Decision: unify the forked steps now; one loop remains the end state.**
>
> The immediate work is to make batch execution and its cancellation contract (orphan sealing, trailing-cancel detection, partial-result commit) a single `pkg/agentsdk` implementation both loops call — the same treatment `StreamAccumulator`, `ApprovalFlow`, and `ExecuteTool` got in #298–#300. Those three have not drifted precisely because there is one of each; `executeTools` never joined them, and that is where both defects above live. This is tracked as #325 and is worth doing regardless of what happens to the loop bodies.
>
> **An earlier revision of this record went further and declared the two-body structure the end state, superseding Phase 1's "delete the parallel copy or promote it — do not keep both." That was wrong on two counts,** and is retracted:
>
> - **It contradicts an accepted ADR without revising it.** `spec.md` ADR-002 (*Shared Agent Core Across All Modes*, Accepted) decides that all modes share a single Agent Core, on the rationale that "bug fixes benefit all modes" and "testing on one path." Two permanent loops keep that true for the three product modes — they all sit on `internal/agent` — but not for embedders, who would be told they are running "the real core" while running a second implementation of it. The redesign's own payoff claim (§Phase 4, "external apps embed the **real** core") is what makes ADR-002's rationale extend to them. A doc under `docs/` cannot quietly narrow an accepted ADR; that needs an ADR revision, argued on its own terms.
> - **It assumed correctness lives only in extractable steps.** The two defects above are themselves the counter-example: both sit in `executeTools`, but their *consequences* differed entirely because of loop control — `internal/agent`'s `task_complete` path consumed the return value immediately and misreported the exit reason, while the SDK loop's next-iteration check masked it. Sharing every step would not have made those two loops agree, because what a step's answer *means* is decided by the body that calls it. Sharing steps narrows the divergence surface; it does not close it.
>
> **What the cost analysis above does establish** is sequencing, not a permanent split: merging the bodies today would mean inventing roughly five more seams in one change, which is what §6 Risks warns against and what deferred this work in the first place. The honest position is that full convergence is still the target, still gated on further Phase 2-style extraction, and now has a concrete next step — unify `executeTools` — rather than a date.

> **Status update (2026-07): drift audit — the eight-file estimate was wrong; the known duplication is `agent.go` plus six seam-dispatch functions.**
>
> After #327 closed the `executeTools` fork, the obvious follow-up was to ask what *else* the two cores implement twice. The premise was sound: a forked step accumulates defects silently, and `executeTools` carried two that took five review findings on the internal side before anyone checked the portable one.
>
> **The first pass over-counted by roughly 8×.** Ten filenames exist in both `internal/agent` and `pkg/agentsdk`; eight of those have non-trivial line and declaration counts on the internal side, which looked like eight parallel implementations. Comparing actual top-level declaration *overlap* gives a different answer:
>
> | File | Overlapping decls | Verdict |
> |---|---|---|
> | `agent.go` | 9 — `Agent`, `Turn`, `runLoop`, `executeTools`, `executeSingleTool`, `makeDoneEvent`, three options | **genuine fork** — the two loops |
> | `conversation.go` | 8 | overlapping, behaviorally identical |
> | `registry.go` | 3 — `Get`, `Names`, `Register` | not a fork: internal's is an `AgentRegistry` over agent *definitions*, the SDK's is a *tool* `Registry`; the shared names are generic registry vocabulary |
> | `approval.go` | 1 — `CheckApproval` | contracts in `pkg`, implementations (`TrustRule`, `TrustRuleChecker`, `SecurityAwareApprovalChecker`) in `internal` |
> | `subagent.go`, `summarizer.go`, `fork.go`, `background.go`, `context_strategy.go` | 0 each | contract-vs-implementation layering, as designed — the SDK holds `Summarizer` / `BackgroundTask` / `ContextStrategy` / `SubagentConfig`, internal holds `LLMSummarizer` / `sessionMemoryBackgroundTask` / the four concrete strategies / `DefaultSubagentSpawner` |
>
> **`conversation.go` was checked method by method and agrees.** `Messages()` copies on both sides; `AddUser` and `AddToolResult` route through `provider.NewUserMessage` / `NewToolResultMessage` in one core and inline literals in the other, producing byte-identical shapes; `AddAssistant`, `Clear` and `SystemPrompt` are the same. The divergence is asymmetric *features* — internal has `Len`, `AddSystem`, `LoadFromMessages`, `DrainMessages` and the tombstone family; the SDK has `EstimateTokens` — each tied to a subsystem the other core does not have. None is a fix the other is missing.
>
> One residual risk worth naming: those two constructions agree by coincidence of separate struct literals, with nothing enforcing it. A change to `provider.NewToolResultMessage` would not follow into the SDK. Low severity — six-line literals — but it is the same class of hazard, and the cheapest guard would be a shared constructor rather than a test.
>
> **A second pass, ignoring filenames, found duplication the first pass could not see.** Intersecting *all* top-level declaration names across the two packages (71 common, 30 of them type aliases) surfaces six functions implemented twice in files that do not share a name:
>
> | Function | `internal/agent` | `pkg/agentsdk` |
> |---|---|---|
> | `startBackgroundTurn`, `startTaskRecovering`, `recoveringJoin`, `endBackgroundSession` | `background.go` | `agent_background.go` |
> | `contributeStrategySections`, `strategySectionsRecovering` | `context_strategy.go` | `agent_context_strategy.go` |
>
> The same-filename pairing compared `background.go` against `background.go`, but the SDK splits the *interface* (`background.go`) from the *dispatch* (`agent_background.go`), so the dispatch pair was invisible to it. These are the seam plumbing added during Phase 2 and Phase 1(b) — the per-task recover boundaries and the strategy contribution loop.
>
> They have **not** drifted: `startBackgroundTurn`, `startTaskRecovering` and `recoveringJoin` are line-for-line identical down to the log messages. But nothing enforces that, and they are precisely the kind of defensive code that gains a fix on one side only — which is how `executeTools` acquired two defects.
>
> **What this means for convergence.** This audit found no additional *observed* duplicated defect. It does not establish that none exists: `agent.go` still holds two distinct, correctness-sensitive loop implementations (#327 repaired the portable cancellation behavior and shared the sealing primitive — it did not remove the duplicate loops), the six seam-dispatch functions above are duplicated though currently identical, and comparing declaration *names* cannot detect behaviorally equivalent code written under different names. The claim this record supports is narrower than "the fork surface is one file": it is that the eight-file estimate was wrong, and that the known duplication is `agent.go` plus the seam dispatch.
>
> **Method note, so this is not re-derived badly.** Two heuristics failed in sequence. Same-filename plus line count selects *for* the intended contract-in-`pkg` / implementation-in-`internal` architecture, so it over-counts. Same-filename declaration overlap then *under*-counts, because the SDK splits interface and dispatch into differently-named files. Intersect declaration names across whole packages, subtract the type aliases, then read what remains — and note that even that misses same-logic-different-name.

Each phase is independently shippable and leaves the product fully working.

---

## 6. Risks & trade-offs

- **Refactor scope is large.** Mitigation: the phased, structural-first plan above — behavior-preserving steps, existing tests as the guardrail, `>90%` coverage rule enforced per PR. Do **not** attempt a big-bang rewrite.
- **Over-abstraction (YAGNI).** Seven seams is the budget; resist inventing a plugin manager for things that have one implementation. A module seam earns its place only when there are ≥2 implementations or a real external embedder.
- **Interface churn at the ContextStrategy seam.** Compaction/memory/knowledge are genuinely entangled with the loop; expect to iterate on that interface. Land it last (Phase 2 tail) once the cheaper wins are in.
- **Tool-count question is separate.** Whether Rubichan should follow pi and push some of its ~36 tools out to CLIs/skills (progressive disclosure, smaller context tax) is a *product* decision, not required by this refactor. Worth a follow-up, but out of scope here.
- **ACP positioning.** CLAUDE.md frames ACP as the backbone. This proposal keeps ACP first-class but reframes it as **a transport** over the shared core rather than a field baked into the agent — which actually strengthens the "standardized backbone for any client" story.

---

## 7. Answer to the question

> Can we redesign to a minimum core with modules, so we don't bind everything together (like TUI), making it more extendable and easy to bind with other apps?

**Yes.** The intended architecture (ADR-002: shared, UI-free core; features injected via interfaces) is already the stated design, and the good bones are present — event-driven core, UI only at the edge, interface-based providers and tools, a standalone ACP package, and even a public SDK skeleton. What has happened in practice is **drift**: the core grew into a 60-field god object, and the SDK forked into a second, weaker implementation.

The redesign is therefore **consolidation, not invention**: unify the duplicated loop/tool/registry, define ~7 module seams, and move today's 35 baked-in options out to modules — turning "everything compiled together" into "a tiny core plus the modules this deployment chose." That is precisely pi.dev's "small core with programmable edges," and it makes embedding Rubichan in another app a matter of picking modules and a transport instead of inheriting the TUI.

---

### Sources
- Pi coding agent — [pi.dev](https://pi.dev/)
- Mario Zechner, *What I learned building an opinionated and minimal coding agent* — [mariozechner.at](https://mariozechner.at/posts/2025-11-30-pi-coding-agent/)
- Rubichan `spec.md` §3 (System Architecture), ADR-002 (Shared Agent Core Across All Modes)
