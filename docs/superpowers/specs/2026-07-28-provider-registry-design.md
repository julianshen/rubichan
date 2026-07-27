# Provider Registry Design

**Status:** Approved for implementation planning
**Date:** 2026-07-28
**Reference:** [earendil-works/pi packages/ai](https://github.com/earendil-works/pi/tree/main/packages/ai) — design inspiration only, not ported wholesale.

## Problem

`internal/provider/factory.go` constructs providers via a hardcoded switch-statement (`NewProviderWithDebug`), and default-model resolution lives as three separate copy-pasted `if` blocks in `cmd/rubichan/main.go`'s `loadConfig()` (zai, ollama, anthropic). These are two views of the same concern — "how do I use provider X" — kept in sync only by developer discipline. That discipline failed once already: PR #329 fixed a bug where `config.DefaultConfig()` baked in an Anthropic-specific model default, which silently defeated Ollama's and Z.ai's own default-model resolution because nobody had a single place enforcing that every provider's defaulting logic actually gets wired up.

pi/ai (a large, mature TypeScript LLM SDK covering 30+ providers) solves the equivalent problem with `createProvider()`: a single declarative registration — identity, auth, base URL, model catalog, wire-protocol dispatch — instead of parallel switch-statements. This spec adopts that *pattern*, not pi/ai's scope (no OAuth, no image generation, no cost tracking, no 30-provider catalog, no changes to the existing `Context`/`Message`/`StreamEvent` data model).

## Scope

**In scope:**
- A declarative per-provider registration (`ProviderDef`) covering construction, auth, base URL, and model defaulting/listing.
- A `Registry` type that both provider construction (`factory.go`) and default-model resolution (`main.go`'s `loadConfig()`) delegate to, so a provider is described once.
- Migrating all 4 existing providers (anthropic, openai-compatible, ollama, zai) onto this registration.
- A `ListModels` extension point for dynamic model catalogs (only Ollama implements it today — Anthropic/OpenAI-compatible/Z.ai don't have a listing API in scope here).

**Out of scope:**
- New public `pkg/` surface — this stays inside `internal/provider` (private), no new subpackage.
- Any change to `CompletionRequest`/`StreamEvent`/`Message`/content-block types, or the streaming event taxonomy. Those already work and aren't part of this problem.
- `capabilities.go`'s heuristic model-capability detection (unrelated concern, untouched).
- Adding new providers beyond the existing 4.
- OAuth, image generation, cost/context-window catalogs, cross-provider handoff, context serialization — all present in pi/ai, all irrelevant to what's broken here.

## Architecture

Everything lives inside the existing `internal/provider` package — this is an evolution of `factory.go`'s existing registry concept (a `map[string]ProviderConstructor` populated by each provider's `init()`), not a new module boundary. The 4 provider sub-packages already import `provider` and self-register via `init()`; that mechanism is unchanged, just registering a richer value.

```
internal/provider/
  registry.go       <- NEW: ProviderDef, Model, Registry types
  factory.go         <- CHANGED: NewProvider/NewProviderWithDebug delegate to Default.New(cfg)
  anthropic/provider.go  <- CHANGED: init() registers a ProviderDef
  openai/provider.go     <- CHANGED: init() registers a ProviderDef
  ollama/provider.go     <- CHANGED: init() registers a ProviderDef; gains DefaultModel/ListModels
  zai/provider.go        <- CHANGED: init() registers a ProviderDef
```

## Components

```go
// registry.go

type Model struct {
    ID   string
    Name string // display name, optional
}

type ProviderDef struct {
    ID          string
    Constructor ProviderConstructor // unchanged: func(baseURL, apiKey string, extraHeaders map[string]string) LLMProvider

    BaseURL func(cfg *config.Config) string
    Auth    func(cfg *config.Config) (apiKey string, headers map[string]string, err error)

    // Optional. nil means the provider has no default-model resolution
    // (caller must always specify one) or no dynamic listing.
    DefaultModel func(ctx context.Context, cfg *config.Config) (string, error)
    ListModels   func(ctx context.Context, cfg *config.Config) ([]Model, error)
}

type Registry struct { /* unexported map[string]ProviderDef */ }

func NewRegistry() *Registry
var Default = NewRegistry()

func (r *Registry) Register(def ProviderDef)
func (r *Registry) New(cfg *config.Config) (LLMProvider, error)
func (r *Registry) ResolveDefaultModel(ctx context.Context, cfg *config.Config) (string, error)
func (r *Registry) ListModels(ctx context.Context, providerID string, cfg *config.Config) ([]Model, error)
```

`Registry.New` looks up `cfg.Provider.Default`, calls the def's `BaseURL`/`Auth`, then `Constructor`. `ResolveDefaultModel` looks up the same def and calls `DefaultModel` (error if nil — caller decides whether that's fatal). `ListModels` calls `ListModels` (error if nil: "provider does not support model listing").

**Existing behavior explicitly preserved, not redesigned:**
- Ollama's `KeepAliveConfigurer` type-assertion (`SetKeepAlive`) — stays exactly as-is, applied after `Constructor` runs.
- `formatUnknownProviderError`'s helpful message for an unrecognized OpenAI-compatible provider name — becomes the OpenAI-compatible def's `Auth`/`BaseURL` error path.
- Ollama's current auto-select behavior (single model → use it, multiple → first-of-list) moves verbatim from `resolveOllamaModel` in `main.go` into the Ollama `ProviderDef.DefaultModel`.
- Z.ai's `cfg.Provider.Zai.Model` → `"glm-5"` fallback moves verbatim from `main.go`'s `loadConfig()` into the Z.ai `ProviderDef.DefaultModel`.
- Anthropic's `DefaultModel` returns the constant `"claude-sonnet-4-5"`.

## Call-site impact

`NewProvider(cfg)` / `NewProviderWithDebug(cfg, debug)` keep their exact current signatures in `factory.go`, now implemented as thin delegates to `Default.New(cfg)` (debug logging wrapping unchanged). **Zero changes needed** at any of the 6 existing call sites (`cmd/rubichan/serve.go`, `shell.go`, `knowledge.go`, `main.go` ×3, plus the `newProviderWithDebug` function-variable indirection used for test mocking).

`main.go`'s `loadConfig()` replaces its three provider-specific `if cfg.Provider.Model == ""` blocks (zai, ollama, anthropic — added/fixed in PR #329) with one call: `provider.Default.ResolveDefaultModel(ctx, cfg)`, gated the same way. `autoDetectProvider` (which auto-*selects* Ollama when no API key is configured) is unchanged — only model-*selection* moves into the registry; provider auto-detection is a separate concern.

## Migration plan

Each step is independently shippable and TDD'd (characterization test before each provider migrates, so a regression is caught immediately, not at the end):

1. Add `Registry`/`ProviderDef`/`Model` to `internal/provider/registry.go` with full unit test coverage. Pure addition — nothing calls it yet, no behavior change anywhere.
2. Migrate Anthropic and Z.ai (simplest — constant/config-driven defaults, no dynamic listing).
3. Migrate Ollama (the one provider with real behavioral logic to move: `ListModels` + auto-select `DefaultModel`).
4. Migrate the OpenAI-compatible provider (per-entry base URL/key lookup, unknown-provider error message).
5. Switch `factory.go` to delegate to `Default.New`; delete the old switch-statement and per-provider `newXxxProvider` functions.
6. Switch `main.go`'s `loadConfig()` to call `Default.ResolveDefaultModel`; delete the three now-redundant `if` blocks and the model-selection half of `resolveOllamaModel`.

Likely 2-3 PRs: (registry + Anthropic + Z.ai), (Ollama), (OpenAI-compatible + factory.go/main.go cleanup) — each a coherent, reviewable unit, per CLAUDE.md's small-PR preference. Structural/behavioral commits kept separate within each PR per Tidy First.

## Testing

- `Registry.New`/`ResolveDefaultModel`/`ListModels` get direct unit tests against fake `ProviderDef`s — no HTTP involved.
- Each provider's `ProviderDef` gets a test asserting it resolves the same auth/base-URL/default-model as today; most of this is adapting existing `factory_test.go` cases, not writing from scratch.
- No behavior change anywhere is a hard requirement, verified at every step against the existing `cmd/rubichan` and `internal/provider` suites (both 100% green as of PR #329).
