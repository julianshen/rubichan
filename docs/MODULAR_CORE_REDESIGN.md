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

`internal/modes/{interactive,headless,wiki}` are already the *intended* thin adapters — each is a small (161–297-line) ACP client that imports only `internal/acp` + `pkg/agentsdk`, not `internal/agent` or the TUI. That is exactly the right shape. But grepping non-test references shows they are **effectively unwired**: the real interactive, headless, and wiki flows are implemented **inline in `main.go`** (headless around `main.go:2320`, wiki via `wireWiki`/`wiki.Run`, interactive via `tui.NewModel` binding straight to `internal/agent.Agent`). So there are two parallel interactive front-ends — a direct-binding TUI (production) and an ACP-mediated one (`modes/interactive`, dormant). Relatedly, several ACP handlers are explicit stubs (`handleToolExecute` → `"not_implemented"`; `Invoke`/`List`/`Scan` return placeholder values). The elegant "every mode is an ACP client over one core" design is **drawn but not finished** — which is good news for the redesign: the target shape is already sketched in the tree, it just needs to become the real path.

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

### 4.1 The extension seams (there should be ~6, not 35)

Most of today's 35 options collapse into a few well-named seams. The core defines the interface; modules implement it; the harness wires them.

1. **`LLMProvider`** — already exists. Providers are modules. *(No change.)*
2. **`Tool` + `ToolRegistry`** — one interface, promoted to the public package; `internal/tools` implements/uses it instead of redefining it. Tools (incl. skills, MCP) are modules.
3. **`Middleware` / hook seam** — a `before/after Turn` and `before/after Tool` interface. **Checkpointing, evaluator, security gating, user hooks, budget enforcement, diff tracking** all become middleware instead of fields. (`internal/hooks` already models this — generalize it.)
4. **`ContextStrategy`** — pluggable context-window management. **Compaction strategies, memory injection, knowledge-graph selection, persona/prompt fragments, prefetch** become strategies the core calls at prompt-build and post-turn time, rather than ~15 dedicated fields.
5. **`EventSink` / observer** — the core already emits `TurnEvent`. Formalize it: TUI, headless formatter, wiki progress, ACP notifications, and session persistence are all just sinks. **`store`, `session`, `activity_summarizer`** attach here.
6. **`Transport`** — direct (in-proc), JSON-stream (headless/CI), and RPC/stdio (ACP). This is how the *same* core serves every mode and every embedder. ACP becomes *a transport*, not a core field.

Rule of thumb after the redesign: **adding a capability adds a module, never a core struct field.**

### 4.2 Where the modes go

Interactive / Headless / Wiki / ACP stop being "modes the core knows about" (today `WithMode("interactive")` is a core string) and become **adapters**: an `EventSink` + a `Transport` + a tool/middleware selection. The mode-specific `acp_client.go` files (already thin, 161–297 lines) are the right size; they should sit *outside* the core and depend only on the public interfaces.

---

## 5. Migration path — incremental, structural-first, small PRs

This respects `CLAUDE.md`: **separate structural from behavioral changes, structural first, tests green at every step, small focused PRs.** No step below changes behavior; each is a refactor validated by the existing suite.

**Phase 0 — Unify the concrete `Registry` (structural, cheap).**
The `Tool` interface is already shared via alias — good. The remaining fork is the **concrete `Registry`** (`internal/tools.Registry` vs `pkg/agentsdk.Registry`). Pick one canonical registry (promote `internal/tools.Registry` to the public package, or make the SDK re-export it) and delete the other. One tool interface, one registry. *Low risk, removes a maintenance fork.*

**Phase 1 — One loop (the pivotal step).**
Make `internal/agent` build on the `pkg` core loop instead of reimplementing `runLoop`/`executeTools`/approval. Either (a) move the real loop into `pkg` and have the SDK use it directly, or (b) express the SDK as the core and `internal/agent` as core + modules. End state: **exactly one** agent loop. Delete `pkg/agentsdk/agent.go`'s parallel copy or promote it — do not keep both. This is what makes the "embed the *real* agent in another app" story true.

**Phase 2 — Extract feature modules out of the god struct (structural, one subsystem per PR).**
For each of checkpoint → knowledge graph → persona/prompt-fragments → memory/session-memory → auto-dream → prefetch → ACP: introduce the seam interface (Middleware / ContextStrategy / Transport), move the subsystem behind it, replace the `With…` field option with a `Use(module)` registration. Struct shrinks one subsystem at a time; tests stay green.

**Phase 3 — Adapters over the core.**
Reduce `cmd/rubichan/main.go` to composition only: build core, register modules, pick an adapter. Move mode wiring into `internal/modes/*` (or `pkg` for reusable ones). Target: `main.go` under a few hundred lines.

**Phase 4 — Publish the module API.**
Document the ~6 seams in `pkg/…` with examples (`examples/` already exists). External apps now embed the **real** core and opt into exactly the modules they want (e.g. a NATS bridge with tools + checkpoint but no TUI).

Each phase is independently shippable and leaves the product fully working.

---

## 6. Risks & trade-offs

- **Refactor scope is large.** Mitigation: the phased, structural-first plan above — behavior-preserving steps, existing tests as the guardrail, `>90%` coverage rule enforced per PR. Do **not** attempt a big-bang rewrite.
- **Over-abstraction (YAGNI).** Six seams is the budget; resist inventing a plugin manager for things that have one implementation. A module seam earns its place only when there are ≥2 implementations or a real external embedder.
- **Interface churn at the ContextStrategy seam.** Compaction/memory/knowledge are genuinely entangled with the loop; expect to iterate on that interface. Land it last (Phase 2 tail) once the cheaper wins are in.
- **Tool-count question is separate.** Whether Rubichan should follow pi and push some of its ~36 tools out to CLIs/skills (progressive disclosure, smaller context tax) is a *product* decision, not required by this refactor. Worth a follow-up, but out of scope here.
- **ACP positioning.** CLAUDE.md frames ACP as the backbone. This proposal keeps ACP first-class but reframes it as **a transport** over the shared core rather than a field baked into the agent — which actually strengthens the "standardized backbone for any client" story.

---

## 7. Answer to the question

> Can we redesign to a minimum core with modules, so we don't bind everything together (like TUI), making it more extendable and easy to bind with other apps?

**Yes.** The intended architecture (ADR-002: shared, UI-free core; features injected via interfaces) is already the stated design, and the good bones are present — event-driven core, UI only at the edge, interface-based providers and tools, a standalone ACP package, and even a public SDK skeleton. What has happened in practice is **drift**: the core grew into a 60-field god object, and the SDK forked into a second, weaker implementation.

The redesign is therefore **consolidation, not invention**: unify the duplicated loop/tool/registry, define ~6 module seams, and move today's 35 baked-in options out to modules — turning "everything compiled together" into "a tiny core plus the modules this deployment chose." That is precisely pi.dev's "small core with programmable edges," and it makes embedding Rubichan in another app a matter of picking modules and a transport instead of inheriting the TUI.

---

### Sources
- Pi coding agent — [pi.dev](https://pi.dev/)
- Mario Zechner, *What I learned building an opinionated and minimal coding agent* — [mariozechner.at](https://mariozechner.at/posts/2025-11-30-pi-coding-agent/)
- Rubichan `spec.md` §3 (System Architecture), ADR-002 (Shared Agent Core Across All Modes)
