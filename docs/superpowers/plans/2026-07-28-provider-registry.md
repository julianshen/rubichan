# Provider Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `internal/provider/factory.go`'s switch-statement and `cmd/rubichan/main.go`'s three copy-pasted default-model `if`-blocks with one declarative per-provider registration (`ProviderDef`), so construction, auth, base URL, and default-model resolution for a provider are described once instead of as parallel logic in two files.

**Architecture:** A new `Registry` type in `internal/provider/registry.go` holds `ProviderDef` values (one per provider: anthropic, zai, ollama, openai-compatible), each supplying `Constructor`/`BaseURL`/`Auth` (required) and `DefaultModel`/`ListModels` (optional — nil means unsupported). Each provider is migrated **fully atomically, one provider per task**: `factory.go`'s switch-case and `main.go`'s corresponding `if`-block are cut over and the old code deleted in the *same* commit as that provider's new `ProviderDef` — no task leaves duplicate logic or dead code for a later task to clean up.

**Tech Stack:** Go, existing `internal/provider`/`internal/config` packages, `testify` (assert/require), `testutil.NewServer` for HTTP-backed provider tests.

## Global Constraints

- TDD strictly: one test at a time, Red → Green → Refactor → Commit. Never write implementation before the test.
- Commit prefixes: `[STRUCTURAL]` (no behavior change) or `[BEHAVIORAL]` (new/changed behavior). Never mix both in one commit.
- Run `go build ./...`, `go test ./...`, `gofmt -l .`, `go vet ./...` after every task; all must be clean before moving on.
- **Zero duplication, zero dead-code windows.** Every task that adds new provider logic must, in the same commit, delete whichever old logic it supersedes (old switch-case body, old `if`-block, old standalone function). No task may leave the same logic implemented in two places, or leave code nothing calls, for a later task to remove.
- No behavior change anywhere is a hard requirement, verified at every task boundary — except the one explicitly-flagged, intentional fix in Task 5 (see that task).
- Never push to `main`. Work happens on `feature/provider-registry` (already created; design spec and a prior draft of this plan are already committed there — this version supersedes that draft, see note below).
- **Correctness note this plan corrects vs. an earlier draft:** a fully-generic `if cfg.Provider.Model == "" { ResolveDefaultModel(...) }` call is only safe once *every* provider is registered in the `Registry` (including the OpenAI-compatible fallback) — calling it while some providers are still unmigrated would hard-error for those. It is also only safe if a provider with no `DefaultModel` resolver (OpenAI-compatible, by design) is treated as "leave the model as-is," not as an error — otherwise users of custom OpenAI-compatible endpoints who don't pass `--model` would get a new hard failure at startup where previously the CLI proceeded (and would fail later, if at all, from the provider's own response). `Registry.ResolveDefaultModel` returns the sentinel `ErrNoDefaultModel` for this case specifically so callers can distinguish it from a real failure.

---

## Prerequisite (read before starting)

PR #329 (branch `fix/ollama-stream-watchdog`, not yet merged as of this plan's writing) changed two files this plan also touches:
- `internal/config/config.go`: `DefaultConfig()` no longer bakes `Provider.Model = "claude-sonnet-4-5"`.
- `cmd/rubichan/main.go`: `loadConfig()` gained three `if cfg.Provider.Model == ""` blocks (zai, ollama, anthropic) that Tasks 2-5 each replace one at a time.

**Before starting Task 2**, confirm `main.go`'s `loadConfig()` has these three blocks (run `grep -n "Provider.Zai.Model\|Resolve.*default model" cmd/rubichan/main.go`). If they're missing, PR #329 hasn't merged yet — merge it first, or `git rebase main` after it merges, then re-check. Task 1 has no dependency on PR #329.

---

## Task 1: Registry core types

**Files:**
- Create: `internal/provider/registry.go`
- Create: `internal/provider/registry_test.go`
- Modify: `internal/provider/factory.go` (move two type declarations out — see below)

**Interfaces:**
- Produces: `provider.Model{ID, Name string}`, `provider.ProviderDef{ID string; Constructor ProviderConstructor; BaseURL func(*config.Config) string; Auth func(*config.Config) (string, map[string]string, error); DefaultModel func(context.Context, *config.Config) (string, error); ListModels func(context.Context, *config.Config) ([]Model, error)}`, `provider.ErrNoDefaultModel` (sentinel error), `provider.Registry` with `NewRegistry() *Registry`, package var `Default *Registry`, methods `Register(def ProviderDef)`, `RegisterFallback(def ProviderDef)`, `New(cfg *config.Config) (LLMProvider, error)`, `ResolveDefaultModel(ctx, cfg) (string, error)`, `ListModels(ctx, providerID string, cfg) ([]Model, error)`.

This task is pure addition — nothing in `cmd/rubichan` or `factory.go`'s switch-statement is touched except moving `KeepAliveConfigurer`/`ProviderConstructor` (verbatim, same names, same package — a mechanical Move, not a duplication, since Go disallows declaring them twice in one package and `registry.go` needs both).

- [ ] **Step 1: Write the failing test for `Registry.New` with a registered provider**

Create `internal/provider/registry_test.go`:

```go
package provider_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	baseURL, apiKey string
	headers         map[string]string
}

func (f *fakeProvider) Stream(context.Context, provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	return nil, nil
}

func fakeDef(id string) provider.ProviderDef {
	return provider.ProviderDef{
		ID: id,
		Constructor: func(baseURL, apiKey string, headers map[string]string) provider.LLMProvider {
			return &fakeProvider{baseURL: baseURL, apiKey: apiKey, headers: headers}
		},
		BaseURL: func(cfg *config.Config) string { return "https://" + id + ".example.com" },
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			return "key-" + id, map[string]string{"X-Test": id}, nil
		},
	}
}

func TestRegistry_New_BuildsRegisteredProvider(t *testing.T) {
	r := provider.NewRegistry()
	r.Register(fakeDef("acme"))

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "acme"

	p, err := r.New(cfg)
	require.NoError(t, err)
	fp, ok := p.(*fakeProvider)
	require.True(t, ok)
	assert.Equal(t, "https://acme.example.com", fp.baseURL)
	assert.Equal(t, "key-acme", fp.apiKey)
	assert.Equal(t, map[string]string{"X-Test": "acme"}, fp.headers)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/... -run TestRegistry_New_BuildsRegisteredProvider -v`
Expected: FAIL — `undefined: provider.NewRegistry` (or similar compile error; `registry.go` doesn't exist yet).

- [ ] **Step 3: Write `registry.go` with `Model`, `ProviderDef`, `Registry`, `Register`, `New`**

First, remove these two blocks from `internal/provider/factory.go` (they move to `registry.go` — leave everything else in `factory.go` untouched):

```go
// ProviderConstructor is a function that creates a new LLMProvider.
type ProviderConstructor func(baseURL, apiKey string, extraHeaders map[string]string) LLMProvider

// KeepAliveConfigurer is implemented by providers that support configurable
// model keep-alive duration (e.g., Ollama). Defined here so the factory can
// type-assert without importing provider sub-packages.
type KeepAliveConfigurer interface {
	SetKeepAlive(duration string)
	KeepAlive() string
}
```

Create `internal/provider/registry.go`:

```go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/julianshen/rubichan/internal/config"
)

// ProviderConstructor is a function that creates a new LLMProvider.
type ProviderConstructor func(baseURL, apiKey string, extraHeaders map[string]string) LLMProvider

// KeepAliveConfigurer is implemented by providers that support configurable
// model keep-alive duration (e.g., Ollama). Registry.New type-asserts
// against it after construction, for any provider that happens to
// implement it — no provider-specific wiring needed here.
type KeepAliveConfigurer interface {
	SetKeepAlive(duration string)
	KeepAlive() string
}

// ErrNoDefaultModel is returned by Registry.ResolveDefaultModel when the
// resolved provider has no DefaultModel resolver (e.g. a custom
// OpenAI-compatible endpoint). Callers should treat this as "leave the
// model unset," not as a failure — the provider was found, it simply
// doesn't offer default-model resolution.
var ErrNoDefaultModel = errors.New("provider has no default model")

// Model describes one entry in a provider's model catalog, whether static
// or fetched dynamically via ProviderDef.ListModels.
type Model struct {
	ID   string
	Name string
}

// ProviderDef is a declarative registration for one provider: how to build
// it, how to authenticate it, and (optionally) how to resolve or list its
// models. Registering a ProviderDef replaces registering a bare
// ProviderConstructor — construction and default-model resolution are
// described once, together, instead of as parallel switch-statements in
// factory.go and cmd/rubichan/main.go.
type ProviderDef struct {
	ID          string
	Constructor ProviderConstructor
	BaseURL     func(cfg *config.Config) string
	Auth        func(cfg *config.Config) (apiKey string, headers map[string]string, err error)

	// DefaultModel resolves the model to use when the user hasn't specified
	// one. nil means the provider has no default-model resolution (the
	// caller must always specify a model explicitly) — ResolveDefaultModel
	// returns ErrNoDefaultModel in that case.
	DefaultModel func(ctx context.Context, cfg *config.Config) (string, error)

	// ListModels returns the provider's available models. nil means the
	// provider doesn't support dynamic listing.
	ListModels func(ctx context.Context, cfg *config.Config) ([]Model, error)
}

// Registry holds ProviderDefs, registered by each provider package's init().
type Registry struct {
	defs        map[string]ProviderDef
	fallback    ProviderDef
	hasFallback bool
}

// NewRegistry returns an empty Registry. Most callers use Default; tests
// that want isolation from global registration state use NewRegistry.
func NewRegistry() *Registry {
	return &Registry{defs: make(map[string]ProviderDef)}
}

// Default is the registry every provider package registers against via
// init(). cmd/rubichan and factory.go use this one.
var Default = NewRegistry()

// Register adds or replaces a provider definition, looked up by ID.
func (r *Registry) Register(def ProviderDef) {
	r.defs[def.ID] = def
}

// RegisterFallback registers a definition used when cfg.Provider.Default
// doesn't match any exact-ID registration — mirrors the OpenAI-compatible
// provider's role of handling any provider name found in
// cfg.Provider.OpenAI rather than one fixed ID.
func (r *Registry) RegisterFallback(def ProviderDef) {
	r.fallback = def
	r.hasFallback = true
}

func (r *Registry) lookup(id string) (ProviderDef, error) {
	if def, ok := r.defs[id]; ok {
		return def, nil
	}
	if r.hasFallback {
		return r.fallback, nil
	}
	return ProviderDef{}, fmt.Errorf("provider %q not registered", id)
}

// New builds the LLMProvider for cfg.Provider.Default.
func (r *Registry) New(cfg *config.Config) (LLMProvider, error) {
	def, err := r.lookup(cfg.Provider.Default)
	if err != nil {
		return nil, err
	}
	apiKey, headers, err := def.Auth(cfg)
	if err != nil {
		return nil, err
	}
	p := def.Constructor(def.BaseURL(cfg), apiKey, headers)
	if ka := cfg.Agent.Cache.OllamaKeepAlive; ka != "" {
		if kac, ok := p.(KeepAliveConfigurer); ok {
			kac.SetKeepAlive(ka)
		}
	}
	return p, nil
}

// ResolveDefaultModel returns the model to use for cfg.Provider.Default when
// the user hasn't specified one. Returns ErrNoDefaultModel if the provider
// has no DefaultModel resolver (distinct from any other error, which means
// resolution was attempted and failed).
func (r *Registry) ResolveDefaultModel(ctx context.Context, cfg *config.Config) (string, error) {
	def, err := r.lookup(cfg.Provider.Default)
	if err != nil {
		return "", err
	}
	if def.DefaultModel == nil {
		return "", ErrNoDefaultModel
	}
	return def.DefaultModel(ctx, cfg)
}

// ListModels returns the available models for providerID. Returns an error
// if the provider has no ListModels resolver.
func (r *Registry) ListModels(ctx context.Context, providerID string, cfg *config.Config) ([]Model, error) {
	def, err := r.lookup(providerID)
	if err != nil {
		return nil, err
	}
	if def.ListModels == nil {
		return nil, fmt.Errorf("provider %q does not support model listing", providerID)
	}
	return def.ListModels(ctx, cfg)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/... -run TestRegistry_New_BuildsRegisteredProvider -v`
Expected: PASS

- [ ] **Step 5: Add the remaining Registry tests**

Append to `internal/provider/registry_test.go`:

```go
func TestRegistry_New_UnknownProvider(t *testing.T) {
	r := provider.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "nope"

	_, err := r.New(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"nope"`)
}

func TestRegistry_New_AuthError(t *testing.T) {
	r := provider.NewRegistry()
	def := fakeDef("acme")
	def.Auth = func(cfg *config.Config) (string, map[string]string, error) {
		return "", nil, fmt.Errorf("no key configured")
	}
	r.Register(def)

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "acme"

	_, err := r.New(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no key configured")
}

func TestRegistry_New_FallsBackWhenNoExactMatch(t *testing.T) {
	r := provider.NewRegistry()
	r.RegisterFallback(fakeDef("compat"))

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "my-custom-endpoint"

	p, err := r.New(cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
}

type keepAliveFakeProvider struct {
	fakeProvider
	keepAlive string
}

func (k *keepAliveFakeProvider) SetKeepAlive(d string) { k.keepAlive = d }
func (k *keepAliveFakeProvider) KeepAlive() string     { return k.keepAlive }

func TestRegistry_New_AppliesKeepAliveConfigurer(t *testing.T) {
	r := provider.NewRegistry()
	r.Register(provider.ProviderDef{
		ID: "ka",
		Constructor: func(baseURL, apiKey string, headers map[string]string) provider.LLMProvider {
			return &keepAliveFakeProvider{}
		},
		BaseURL: func(cfg *config.Config) string { return "" },
		Auth:    func(cfg *config.Config) (string, map[string]string, error) { return "", nil, nil },
	})

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ka"
	cfg.Agent.Cache.OllamaKeepAlive = "10m"

	p, err := r.New(cfg)
	require.NoError(t, err)
	kap, ok := p.(*keepAliveFakeProvider)
	require.True(t, ok)
	assert.Equal(t, "10m", kap.keepAlive)
}

func TestRegistry_ResolveDefaultModel(t *testing.T) {
	r := provider.NewRegistry()
	def := fakeDef("acme")
	def.DefaultModel = func(ctx context.Context, cfg *config.Config) (string, error) {
		return "acme-model-1", nil
	}
	r.Register(def)

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "acme"

	model, err := r.ResolveDefaultModel(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "acme-model-1", model)
}

func TestRegistry_ResolveDefaultModel_NoResolver(t *testing.T) {
	r := provider.NewRegistry()
	r.Register(fakeDef("acme")) // DefaultModel left nil

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "acme"

	_, err := r.ResolveDefaultModel(context.Background(), cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, provider.ErrNoDefaultModel))
}

func TestRegistry_ListModels(t *testing.T) {
	r := provider.NewRegistry()
	def := fakeDef("acme")
	def.ListModels = func(ctx context.Context, cfg *config.Config) ([]provider.Model, error) {
		return []provider.Model{{ID: "m1", Name: "Model One"}}, nil
	}
	r.Register(def)

	models, err := r.ListModels(context.Background(), "acme", config.DefaultConfig())
	require.NoError(t, err)
	assert.Equal(t, []provider.Model{{ID: "m1", Name: "Model One"}}, models)
}

func TestRegistry_ListModels_NoResolver(t *testing.T) {
	r := provider.NewRegistry()
	r.Register(fakeDef("acme")) // ListModels left nil

	_, err := r.ListModels(context.Background(), "acme", config.DefaultConfig())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support model listing")
}
```

- [ ] **Step 6: Run the full registry test file and the whole provider package**

Run: `go test ./internal/provider/... -run TestRegistry -v`
Expected: all `TestRegistry_*` PASS.

Run: `go test ./internal/provider/... ./cmd/rubichan/...`
Expected: all existing tests still PASS unchanged (this task didn't touch any provider construction logic, only moved two type declarations verbatim).

- [ ] **Step 7: Format, vet, commit**

Run: `gofmt -l internal/provider/registry.go internal/provider/registry_test.go internal/provider/factory.go` (expect no output) and `go vet ./internal/provider/...` (expect no issues).

```bash
git add internal/provider/registry.go internal/provider/registry_test.go internal/provider/factory.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Add the provider Registry/ProviderDef/Model types

New declarative registration mechanism for providers: construction, auth,
base URL, and optional default-model/model-listing resolvers described
once per provider instead of as parallel switch-statement logic. Nothing
uses it yet — factory.go's existing switch-statement is untouched.
Moves KeepAliveConfigurer/ProviderConstructor out of factory.go since
registry.go needs them and Go disallows duplicate declarations.
EOF
)"
```

---

## Task 2: Migrate Anthropic (fully atomic — construction + default model)

**Files:**
- Modify: `internal/provider/anthropic/provider.go` (its `init()`, currently lines 17-21)
- Modify: `internal/provider/anthropic/provider_test.go`
- Modify: `internal/provider/factory.go` (the `"anthropic"` switch-case; delete `newAnthropicProvider` and `anthropicBaseURL`)
- Modify: `cmd/rubichan/main.go` (the anthropic `if` block in `loadConfig()`)

**Interfaces:**
- Consumes: `provider.ProviderDef`, `provider.Default.Register`, `provider.Default.New`, `provider.Default.ResolveDefaultModel` (Task 1).
- Produces: `anthropic.providerDef() provider.ProviderDef` — an unexported function tests call directly, isolated from the shared `provider.Default` registry's global state.

**Why this task also touches `factory.go` and `main.go`:** per this plan's zero-duplication constraint, the new `ProviderDef` and the code it replaces cannot coexist past this one commit. `newAnthropicProvider` (factory.go) and the anthropic `if` block (main.go) are deleted in this same task, the moment the registry-based path takes over their job — not in a later cleanup task.

- [ ] **Step 1: Write the failing test for the new ProviderDef**

Add to `internal/provider/anthropic/provider_test.go` (package `anthropic`, already imports `provider`, `testify/assert`, `testify/require` — add `"github.com/julianshen/rubichan/internal/config"`):

```go
func TestProviderDef_BaseURLAndAuth(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")

	def := providerDef()
	cfg := config.DefaultConfig()

	assert.Equal(t, "https://api.anthropic.com", def.BaseURL(cfg))

	apiKey, headers, err := def.Auth(cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-anthropic-key", apiKey)
	assert.Nil(t, headers)
}

func TestProviderDef_AuthMissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	def := providerDef()
	cfg := config.DefaultConfig()

	_, _, err := def.Auth(cfg)
	require.Error(t, err)
}

func TestProviderDef_DefaultModel(t *testing.T) {
	def := providerDef()

	model, err := def.DefaultModel(context.Background(), config.DefaultConfig())
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-5", model)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/anthropic/... -run TestProviderDef -v`
Expected: FAIL — `undefined: providerDef` (and `undefined: config` until the import is added — add `"github.com/julianshen/rubichan/internal/config"` to the test file's import block now).

- [ ] **Step 3: Add `providerDef()`, replace `init()`'s registration**

In `internal/provider/anthropic/provider.go`, replace:

```go
func init() {
	provider.RegisterProvider("anthropic", func(baseURL, apiKey string, _ map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey)
	})
}
```

with:

```go
const anthropicBaseURL = "https://api.anthropic.com"

func init() {
	provider.Default.Register(providerDef())
}

// providerDef describes this provider's construction, auth, and default
// model for provider.Default. Exposed as a function (not inlined in init)
// so tests can exercise it directly without depending on the shared
// provider.Default registry's global state.
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "anthropic",
		Constructor: func(baseURL, apiKey string, _ map[string]string) provider.LLMProvider {
			return New(baseURL, apiKey)
		},
		BaseURL: func(cfg *config.Config) string {
			return anthropicBaseURL
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			apiKey, err := config.ResolveAPIKey(
				cfg.Provider.Anthropic.APIKeySource,
				cfg.Provider.Anthropic.APIKey,
				"ANTHROPIC_API_KEY",
			)
			if err != nil {
				return "", nil, fmt.Errorf("resolving Anthropic API key: %w", err)
			}
			return apiKey, nil, nil
		},
		DefaultModel: func(_ context.Context, _ *config.Config) (string, error) {
			return "claude-sonnet-4-5", nil
		},
	}
}
```

Add `"github.com/julianshen/rubichan/internal/config"` to `internal/provider/anthropic/provider.go`'s import block (`context` and `fmt` are already imported).

- [ ] **Step 4: Cut `factory.go`'s `"anthropic"` switch-case over, delete the old function**

In `internal/provider/factory.go`, change the switch case (inside `NewProviderWithDebug`) from:

```go
	switch cfg.Provider.Default {
	case "anthropic":
		p, err = newAnthropicProvider(cfg)
```

to:

```go
	switch cfg.Provider.Default {
	case "anthropic":
		p, err = Default.New(cfg)
```

(This preserves the existing fall-through-to-debug-wrapping behavior for anthropic exactly — the original code already fell through for this case, so nothing about debug-logging changes here.)

Delete the now-unused `newAnthropicProvider` function and the `const anthropicBaseURL = "https://api.anthropic.com"` line from `factory.go` (the constant now lives in `anthropic/provider.go`, added in Step 3). Leave `newOllamaProvider`, `newZaiProvider`, `newOpenAIProvider`, `formatUnknownProviderError`, `registry`, and `RegisterProvider` untouched — they're still used by the other, not-yet-migrated switch cases.

- [ ] **Step 5: Cut `main.go`'s anthropic `if` block over**

In `cmd/rubichan/main.go`'s `loadConfig()`, replace:

```go
	// Resolve Anthropic's default model if it's the (possibly auto-detected)
	// provider and no model was specified. This runs last so it only ever
	// fills in what the provider-specific resolutions above left empty.
	if cfg.Provider.Default == "anthropic" && cfg.Provider.Model == "" {
		cfg.Provider.Model = "claude-sonnet-4-5"
	}
```

with:

```go
	// Resolve Anthropic's default model if it's the (possibly auto-detected)
	// provider and no model was specified. This runs last so it only ever
	// fills in what the provider-specific resolutions above left empty.
	// (Zai/Ollama below still resolve their own default inline until Tasks
	// 3-4 migrate them the same way; Task 5 collapses all three into one
	// generic call once every provider is registered.)
	if cfg.Provider.Default == "anthropic" && cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
	}
```

`context` and `provider` are both already imported in `cmd/rubichan/main.go`. Leave the zai and ollama `if` blocks below this one exactly as they are — Tasks 3 and 4 migrate those.

- [ ] **Step 6: Run the full anthropic package, provider package, and cmd/rubichan (regression check)**

Run: `go test ./internal/provider/anthropic/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS unchanged, including `internal/provider/factory_test.go`'s `TestNewProviderAnthropic`/`TestNewProviderAnthropicMissingKey` (unmodified — they test the public `NewProvider` behavior, which hasn't changed) and `cmd/rubichan/coverage_test.go`'s `TestLoadConfig_AnthropicDefaultModel` (unmodified — same resolved value, same gating).

- [ ] **Step 7: Format, vet, commit**

Run: `gofmt -l internal/provider/anthropic/provider.go internal/provider/anthropic/provider_test.go internal/provider/factory.go cmd/rubichan/main.go` and `go vet ./internal/provider/anthropic/... ./internal/provider/... ./cmd/rubichan/...`.

```bash
git add internal/provider/anthropic/provider.go internal/provider/anthropic/provider_test.go internal/provider/factory.go cmd/rubichan/main.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Migrate Anthropic to a ProviderDef

Fully atomic: registers the new ProviderDef, cuts factory.go's
"anthropic" switch-case and main.go's anthropic default-model block over
to it, and deletes newAnthropicProvider/anthropicBaseURL in the same
commit — no window where the old and new logic coexist.
EOF
)"
```

This completes a coherent, independently mergeable unit. Suggest a PR checkpoint here (Task 1 + Task 2) before continuing.

---

## Task 3: Migrate Z.ai (fully atomic)

**Files:**
- Modify: `internal/provider/zai/provider.go` (its `init()`, currently lines 15-19)
- Modify: `internal/provider/zai/provider_test.go`
- Modify: `internal/provider/factory.go` (the `"zai"` switch-case; delete `newZaiProvider`)
- Modify: `cmd/rubichan/main.go` (the zai `if` block in `loadConfig()`)

**Interfaces:**
- Consumes: same as Task 2.
- Produces: `zai.providerDef() provider.ProviderDef`.

- [ ] **Step 1: Write the failing test**

Add to `internal/provider/zai/provider_test.go` (package `zai`; add `"github.com/julianshen/rubichan/internal/config"` to imports):

```go
func TestProviderDef_BaseURLAndAuth(t *testing.T) {
	t.Setenv("Z_AI_API_KEY", "test-zai-key")

	def := providerDef()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"

	assert.Equal(t, "https://api.z.ai/api/coding/paas/v4", def.BaseURL(cfg))

	apiKey, headers, err := def.Auth(cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-zai-key", apiKey)
	assert.Nil(t, headers)
}

func TestProviderDef_BaseURLOverride(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()
	cfg.Provider.Zai.BaseURL = "https://custom.zai.example.com"

	assert.Equal(t, "https://custom.zai.example.com", def.BaseURL(cfg))
}

func TestProviderDef_DefaultModel_FallsBackToGlm5(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()

	model, err := def.DefaultModel(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "glm-5", model)
}

func TestProviderDef_DefaultModel_UsesConfiguredModel(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()
	cfg.Provider.Zai.Model = "custom-glm"

	model, err := def.DefaultModel(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "custom-glm", model)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/zai/... -run TestProviderDef -v`
Expected: FAIL — `undefined: providerDef`.

- [ ] **Step 3: Add `providerDef()`, replace `init()`'s registration**

In `internal/provider/zai/provider.go`, replace:

```go
func init() {
	provider.RegisterProvider("zai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey, "glm-5", extraHeaders)
	})
}
```

with:

```go
func init() {
	provider.Default.Register(providerDef())
}

// providerDef describes this provider's construction, auth, and default
// model for provider.Default. Exposed as a function so tests can exercise
// it directly, isolated from the shared provider.Default registry.
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "zai",
		Constructor: func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
			return New(baseURL, apiKey, "glm-5", extraHeaders)
		},
		BaseURL: func(cfg *config.Config) string {
			if cfg.Provider.Zai.BaseURL != "" {
				return cfg.Provider.Zai.BaseURL
			}
			return "https://api.z.ai/api/coding/paas/v4"
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			apiKey, err := config.ResolveAPIKey(
				cfg.Provider.Zai.APIKeySource,
				cfg.Provider.Zai.APIKey,
				"Z_AI_API_KEY",
			)
			if err != nil {
				return "", nil, fmt.Errorf("resolving Z.ai API key: %w", err)
			}
			return apiKey, nil, nil
		},
		DefaultModel: func(_ context.Context, cfg *config.Config) (string, error) {
			if cfg.Provider.Zai.Model != "" {
				return cfg.Provider.Zai.Model, nil
			}
			return "glm-5", nil
		},
	}
}
```

Add `"context"` and `"github.com/julianshen/rubichan/internal/config"` to `internal/provider/zai/provider.go`'s import block (`fmt` is already imported).

- [ ] **Step 4: Cut `factory.go`'s `"zai"` switch-case over, delete the old function**

In `internal/provider/factory.go`, change:

```go
	case "zai":
		return newZaiProvider(cfg)
```

to:

```go
	case "zai":
		return Default.New(cfg)
```

Keep the early `return` (not falling through to the `if debug { EnableDebugLogging(p) }` line below) — this preserves the existing behavior exactly, including its latent quirk that Z.ai (like Ollama) never receives debug-logging even when `--debug` is passed, because the original code also returns early for this case. Task 5 removes the whole switch-statement and fixes this quirk for every provider at once, explicitly flagged there — don't fix it here, to keep this commit's behavior change scoped to "same construction, now via the registry."

Delete the now-unused `newZaiProvider` function from `factory.go`.

- [ ] **Step 5: Cut `main.go`'s zai `if` block over**

In `cmd/rubichan/main.go`'s `loadConfig()`, replace:

```go
	// Resolve Z.ai's default model if provider is zai and no model specified
	// via --model. [provider.zai].model wins over the built-in fallback.
	if cfg.Provider.Default == "zai" && cfg.Provider.Model == "" {
		if cfg.Provider.Zai.Model != "" {
			cfg.Provider.Model = cfg.Provider.Zai.Model
		} else {
			cfg.Provider.Model = "glm-5"
		}
	}
```

with:

```go
	// Resolve Z.ai's default model if provider is zai and no model specified.
	if cfg.Provider.Default == "zai" && cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
	}
```

- [ ] **Step 6: Run the full zai package, provider package, and cmd/rubichan (regression check)**

Run: `go test ./internal/provider/zai/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS unchanged, including `cmd/rubichan/coverage_test.go`'s `TestLoadConfig_ZaiDefaultModel_FallsBackWhenUnset`/`TestLoadConfig_ZaiDefaultModel_UsesConfiguredZaiModel`.

- [ ] **Step 7: Format, vet, commit**

Run: `gofmt -l internal/provider/zai/provider.go internal/provider/zai/provider_test.go internal/provider/factory.go cmd/rubichan/main.go` and `go vet ./internal/provider/zai/... ./internal/provider/... ./cmd/rubichan/...`.

```bash
git add internal/provider/zai/provider.go internal/provider/zai/provider_test.go internal/provider/factory.go cmd/rubichan/main.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Migrate Z.ai to a ProviderDef

Same fully-atomic treatment as Anthropic (Task 2): DefaultModel
replicates the cfg.Provider.Zai.Model -> "glm-5" fallback exactly, and
newZaiProvider/the old main.go if-block are deleted in this same commit.
EOF
)"
```

---

## Task 4: Migrate Ollama (fully atomic — construction + default model + listing)

**Files:**
- Modify: `internal/provider/ollama/provider.go` (its `init()`, currently lines 17-21)
- Modify: `internal/provider/ollama/provider_test.go`
- Modify: `internal/provider/factory.go` (the `"ollama"` switch-case; delete `newOllamaProvider`)
- Modify: `cmd/rubichan/main.go` (the ollama `if` block in `loadConfig()`; delete `resolveOllamaModel`)
- Modify: `cmd/rubichan/main_test.go` (delete `TestResolveOllamaModel_SingleModel`, `TestResolveOllamaModel_NoModels`, `TestResolveOllamaModel_MultipleModels`, `TestResolveOllamaModel_ConnectionError` — moved to `internal/provider/ollama/provider_test.go` in this same task, not left behind for later cleanup)

**Interfaces:**
- Consumes: same as Task 2, plus `ollama.NewClient(baseURL string) *Client` and `Client.ListModels(ctx) ([]ModelInfo, error)` (existing, `internal/provider/ollama/client.go`), `ollama.DefaultBaseURL` (existing constant, `"http://localhost:11434"`).
- Produces: `ollama.providerDef() provider.ProviderDef`, `ollama.resolveDefaultModel(ctx, cfg) (string, error)`, `ollama.listModels(ctx, cfg) ([]provider.Model, error)` — these two replicate `cmd/rubichan/main.go`'s `resolveOllamaModel` behavior exactly (single model → auto-select; multiple → first-of-list; zero → error `"no models found; run 'rubichan ollama pull <model>' first"`), and this task deletes `resolveOllamaModel` in the same commit — the old and new implementations never coexist.

- [ ] **Step 1: Write the failing tests for `resolveDefaultModel`/`listModels`**

Add to `internal/provider/ollama/provider_test.go` (package `ollama`; add `"github.com/julianshen/rubichan/internal/config"` to imports — `context`, `net/http`, `testutil` already imported):

```go
func TestResolveDefaultModel_SingleModel(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [{"name": "llama3.2:latest", "size": 4294967296}]}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = srv.URL

	model, err := resolveDefaultModel(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "llama3.2:latest", model)
}

func TestResolveDefaultModel_NoModels(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = srv.URL

	_, err := resolveDefaultModel(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

func TestResolveDefaultModel_MultipleModels(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [
			{"name": "llama3.2:latest", "size": 4294967296},
			{"name": "codellama:7b", "size": 3758096384}
		]}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = srv.URL

	model, err := resolveDefaultModel(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "llama3.2:latest", model) // returns first model
}

func TestResolveDefaultModel_ConnectionError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = "http://localhost:1"

	_, err := resolveDefaultModel(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing Ollama models")
}

func TestListModels(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [{"name": "llama3.2:latest", "size": 1}]}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = srv.URL

	models, err := listModels(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, []provider.Model{{ID: "llama3.2:latest", Name: "llama3.2:latest"}}, models)
}

func TestProviderDef_BaseURLDefault(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()

	assert.Equal(t, DefaultBaseURL, def.BaseURL(cfg))
}

func TestProviderDef_BaseURLOverride(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = "http://custom-ollama:1234"

	assert.Equal(t, "http://custom-ollama:1234", def.BaseURL(cfg))
}

func TestProviderDef_AuthIsKeyless(t *testing.T) {
	def := providerDef()

	apiKey, headers, err := def.Auth(config.DefaultConfig())
	require.NoError(t, err)
	assert.Empty(t, apiKey)
	assert.Nil(t, headers)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/ollama/... -run 'TestResolveDefaultModel|TestListModels|TestProviderDef' -v`
Expected: FAIL — `undefined: resolveDefaultModel` (and friends).

- [ ] **Step 3: Add `resolveDefaultModel`, `listModels`, `providerDef()`, replace `init()`'s registration**

In `internal/provider/ollama/provider.go`, replace:

```go
func init() {
	provider.RegisterProvider("ollama", func(baseURL, _ string, _ map[string]string) provider.LLMProvider {
		return New(baseURL)
	})
}
```

with:

```go
func init() {
	provider.Default.Register(providerDef())
}

// providerDef describes this provider's construction, auth, default model,
// and model listing for provider.Default. Exposed as a function so tests
// can exercise it directly, isolated from the shared provider.Default
// registry.
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "ollama",
		Constructor: func(baseURL, _ string, _ map[string]string) provider.LLMProvider {
			return New(baseURL)
		},
		BaseURL: func(cfg *config.Config) string {
			return resolveBaseURL(cfg)
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			return "", nil, nil // local server, no credentials
		},
		DefaultModel: resolveDefaultModel,
		ListModels:   listModels,
	}
}

func resolveBaseURL(cfg *config.Config) string {
	if cfg.Provider.Ollama.BaseURL != "" {
		return cfg.Provider.Ollama.BaseURL
	}
	return DefaultBaseURL
}

// resolveDefaultModel queries Ollama for available models and resolves
// which model to use. With a single model it auto-selects; with multiple,
// it returns the first (a future TUI picker would use listModels/ListModels
// directly instead of this auto-select).
func resolveDefaultModel(ctx context.Context, cfg *config.Config) (string, error) {
	models, err := listModels(ctx, cfg)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no models found; run 'rubichan ollama pull <model>' first")
	}
	return models[0].ID, nil
}

func listModels(ctx context.Context, cfg *config.Config) ([]provider.Model, error) {
	client := NewClient(resolveBaseURL(cfg))
	infos, err := client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing Ollama models: %w", err)
	}
	models := make([]provider.Model, len(infos))
	for i, info := range infos {
		models[i] = provider.Model{ID: info.Name, Name: info.Name}
	}
	return models, nil
}
```

Add `"github.com/julianshen/rubichan/internal/config"` to `internal/provider/ollama/provider.go`'s import block (`context` and `fmt` are already imported).

- [ ] **Step 4: Cut `factory.go`'s `"ollama"` switch-case over, delete the old function**

In `internal/provider/factory.go`, change:

```go
	case "ollama":
		return newOllamaProvider(cfg)
```

to:

```go
	case "ollama":
		return Default.New(cfg)
```

Keep the early `return`, matching Task 3's reasoning exactly — preserves the existing debug-logging quirk unchanged; Task 5 fixes it for every provider at once, explicitly.

Delete the now-unused `newOllamaProvider` function from `factory.go`.

- [ ] **Step 5: Cut `main.go`'s ollama `if` block over, delete `resolveOllamaModel`**

In `cmd/rubichan/main.go`'s `loadConfig()`, replace:

```go
	// Resolve Ollama model if provider is ollama and no model specified.
	if cfg.Provider.Default == "ollama" && cfg.Provider.Model == "" {
		model, err := resolveOllamaModel(ollamaURL)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
		fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
	}
```

with:

```go
	// Resolve Ollama model if provider is ollama and no model specified.
	if cfg.Provider.Default == "ollama" && cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
		fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
	}
```

`ollamaURL` (computed a few lines above, still used by `autoDetectProvider` just before this block) is no longer read by this block, but leave its computation and the `autoDetectProvider` call untouched — provider auto-*detection* is a separate concern from model resolution, per the design spec.

Delete this function from `cmd/rubichan/main.go` entirely (immediately precedes `loadConfig()`):

```go
// resolveOllamaModel queries Ollama for available models and resolves which
// model to use. With a single model it auto-selects; with zero models it
// returns an error. The ollamaURL parameter allows testing with httptest.
func resolveOllamaModel(ollamaURL string) (string, error) {
	client := ollama.NewClient(ollamaURL)
	models, err := client.ListModels(context.Background())
	if err != nil {
		return "", fmt.Errorf("listing Ollama models: %w", err)
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no models found; run 'rubichan ollama pull <model>' first")
	}

	if len(models) == 1 {
		return models[0].Name, nil
	}

	// Multiple models — in interactive mode, we'd show a picker.
	// For now, return the first model. The TUI picker integration
	// requires running a Bubble Tea program which is complex to wire here.
	// TODO: integrate tui.ModelPicker when running interactively.
	return models[0].Name, nil
}
```

Leave the `ollama` package import in `main.go` in place — `autoDetectProvider` still uses `ollama.NewClient(...).IsRunning(...)`.

- [ ] **Step 6: Delete `resolveOllamaModel`'s tests from `main_test.go`**

Remove `TestResolveOllamaModel_SingleModel`, `TestResolveOllamaModel_NoModels`, `TestResolveOllamaModel_MultipleModels` (currently just before `capabilityTestProvider`), and `TestResolveOllamaModel_ConnectionError` (currently just before `// saveFlags saves...`) from `cmd/rubichan/main_test.go`. Their behavior is now covered by Step 1's tests in `internal/provider/ollama/provider_test.go`.

- [ ] **Step 7: Run the full ollama package, provider package, and cmd/rubichan (regression check)**

Run: `go test ./internal/provider/ollama/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS. `grep -rn "resolveOllamaModel" cmd/rubichan/` should return no matches at all (function and all its call sites/tests gone).

- [ ] **Step 8: Format, vet, commit**

Run: `gofmt -l internal/provider/ollama/provider.go internal/provider/ollama/provider_test.go internal/provider/factory.go cmd/rubichan/main.go cmd/rubichan/main_test.go` and `go vet ./internal/provider/ollama/... ./internal/provider/... ./cmd/rubichan/...`.

```bash
git add internal/provider/ollama/provider.go internal/provider/ollama/provider_test.go internal/provider/factory.go cmd/rubichan/main.go cmd/rubichan/main_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Migrate Ollama to a ProviderDef with model listing

Fully atomic: providerDef's DefaultModel/ListModels replicate
resolveOllamaModel's exact behavior, and that function (plus its 4 tests
in main_test.go) is deleted in this same commit — never coexists with
its replacement. ListModels is the first real use of the
ProviderDef.ListModels extension point; Anthropic/Z.ai don't have a
listing API and leave it nil.
EOF
)"
```

---

## Task 5: Migrate the OpenAI-compatible provider and remove the switch-statement scaffolding

**Files:**
- Modify: `internal/provider/openai/provider.go` (its `init()`, currently lines 14-18)
- Modify: `internal/provider/openai/provider_test.go`
- Modify: `internal/provider/factory.go` (replace the entire switch-statement with a direct `Default.New` call; delete `newOpenAIProvider`, `formatUnknownProviderError`, `registry`, `RegisterProvider`)
- Modify: `cmd/rubichan/main.go` (collapse the anthropic/zai/ollama `if` blocks into one generic call)

**Interfaces:**
- Consumes: `Registry.RegisterFallback` (Task 1) — this provider handles *any* `cfg.Provider.Default` name that doesn't match an exact registration (openrouter, custom proxies, etc.), matching `factory.go`'s current `switch { ...; default: newOpenAIProvider }`.
- Produces: `openai.providerDef() provider.ProviderDef`, `openai.formatUnknownProviderError(name string, configured []config.OpenAICompatibleConfig) error` (moved here from `factory.go` in this same commit).

**This is the last provider migration, so it's also where the shared scaffolding — the switch-statement itself, the old `registry` map, `RegisterProvider` — becomes fully unused and is deleted, and where `main.go`'s three `if` blocks collapse into one. Both are safe only now, for two reasons documented in this plan's Global Constraints: (1) `Registry.lookup` would hard-error for any not-yet-registered provider name, so the generic single-dispatch form can't land until every provider (including this fallback) is registered; (2) `ErrNoDefaultModel` must exist so an OpenAI-compatible provider's absence of default-model resolution is treated as "leave unset," not a startup failure.**

- [ ] **Step 1: Write the failing tests**

Add to `internal/provider/openai/provider_test.go` (package `openai`; add `"github.com/julianshen/rubichan/internal/config"` to imports):

```go
func TestProviderDef_BaseURLAndAuth(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openrouter"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKeySource: "config", APIKey: "test-key", ExtraHeaders: map[string]string{"X-Test": "1"}},
	}

	assert.Equal(t, "https://openrouter.ai/api/v1", def.BaseURL(cfg))

	apiKey, headers, err := def.Auth(cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-key", apiKey)
	assert.Equal(t, map[string]string{"X-Test": "1"}, headers)
}

func TestProviderDef_AuthUnknownProvider(t *testing.T) {
	def := providerDef()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "does-not-exist"

	_, _, err := def.Auth(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"does-not-exist"`)
}

func TestProviderDef_DefaultModelIsNil(t *testing.T) {
	def := providerDef()
	assert.Nil(t, def.DefaultModel)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/openai/... -run TestProviderDef -v`
Expected: FAIL — `undefined: providerDef`.

- [ ] **Step 3: Add `providerDef()`, `formatUnknownProviderError`, replace `init()`'s registration**

In `internal/provider/openai/provider.go`, replace:

```go
func init() {
	provider.RegisterProvider("openai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey, extraHeaders)
	})
}
```

with:

```go
func init() {
	provider.Default.RegisterFallback(providerDef())
}

// providerDef describes this provider's construction, auth, and base URL
// for provider.Default. Registered as the fallback (not an exact-ID match)
// because this provider handles any name found in cfg.Provider.OpenAI —
// "openai", "openrouter", a custom proxy name, etc. — not one fixed ID.
// DefaultModel is left nil: this provider has never had default-model
// resolution (users must pass --model), and Registry.ResolveDefaultModel's
// ErrNoDefaultModel preserves that — see main.go's loadConfig().
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "openai",
		Constructor: func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
			return New(baseURL, apiKey, extraHeaders)
		},
		BaseURL: func(cfg *config.Config) string {
			oc, _ := lookupCompatEntry(cfg)
			return oc.BaseURL
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			oc, ok := lookupCompatEntry(cfg)
			if !ok {
				return "", nil, formatUnknownProviderError(cfg.Provider.Default, cfg.Provider.OpenAI)
			}
			apiKey, err := config.ResolveOpenAICompatibleAPIKey(oc)
			if err != nil {
				return "", nil, fmt.Errorf("resolving %s API key: %w", cfg.Provider.Default, err)
			}
			return apiKey, oc.ExtraHeaders, nil
		},
	}
}

func lookupCompatEntry(cfg *config.Config) (config.OpenAICompatibleConfig, bool) {
	for _, oc := range cfg.Provider.OpenAI {
		if oc.Name == cfg.Provider.Default {
			return oc, true
		}
	}
	return config.OpenAICompatibleConfig{}, false
}

// formatUnknownProviderError builds a helpful error message when the
// requested provider name doesn't match any configured
// [[provider.openai_compatible]] entry. It lists what IS configured and
// shows example config / CLI usage.
func formatUnknownProviderError(name string, configured []config.OpenAICompatibleConfig) error {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown provider: %q\n\n", name)

	if len(configured) > 0 {
		b.WriteString("Configured OpenAI-compatible providers:\n")
		for _, oc := range configured {
			fmt.Fprintf(&b, "  - %s (%s)\n", oc.Name, oc.BaseURL)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("No OpenAI-compatible providers are configured.\n\n")
	}

	b.WriteString("Quick fix — use CLI flags:\n")
	fmt.Fprintf(&b, "  rubichan --provider %s --api-base http://localhost:1234/v1 --model my-model\n\n", name)

	b.WriteString("Or add to ~/.config/rubichan/config.toml:\n")
	fmt.Fprintf(&b, "  [provider]\n")
	fmt.Fprintf(&b, "  default = %q\n", name)
	fmt.Fprintf(&b, "  model   = \"my-model\"\n\n")
	fmt.Fprintf(&b, "  [[provider.openai_compatible]]\n")
	fmt.Fprintf(&b, "  name     = %q\n", name)
	fmt.Fprintf(&b, "  base_url = \"http://localhost:1234/v1\"\n")
	fmt.Fprintf(&b, "  api_key  = \"none\"")

	return fmt.Errorf("%s", b.String())
}
```

Add `"strings"` and `"github.com/julianshen/rubichan/internal/config"` to `internal/provider/openai/provider.go`'s import block (`fmt` is already imported).

**Note:** `Registry.New` calls `def.Auth(cfg)` before `def.BaseURL(cfg)` (see Task 1's `New` implementation) — this ordering is why `BaseURL` above can safely ignore the not-found case (`oc, _ := lookupCompatEntry(cfg)`) rather than duplicating the error: by the time `BaseURL` runs, `Auth` has already succeeded, which only happens when the entry exists.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/openai/... -run TestProviderDef -v`
Expected: PASS

- [ ] **Step 5: Replace `factory.go`'s entire switch-statement, delete now-dead code**

`internal/provider/factory.go` should end up containing only `NewProvider` and `NewProviderWithDebug`, plus the imports they need:

```go
package provider

import (
	"github.com/julianshen/rubichan/internal/config"
)

// NewProvider creates an LLMProvider based on the given configuration.
// It routes to the appropriate provider based on the default provider name.
// Use NewProviderWithDebug to enable debug logging of API requests/responses.
func NewProvider(cfg *config.Config) (LLMProvider, error) {
	return NewProviderWithDebug(cfg, false)
}

// NewProviderWithDebug creates an LLMProvider and optionally enables debug
// logging of HTTP request/response details to stderr via log.Printf.
func NewProviderWithDebug(cfg *config.Config, debug bool) (LLMProvider, error) {
	p, err := Default.New(cfg)
	if err != nil {
		return nil, err
	}

	if debug {
		EnableDebugLogging(p)
	}

	return p, nil
}
```

Delete `newOpenAIProvider`, `formatUnknownProviderError` (moved to `openai/provider.go` in Step 3), the `registry map[string]ProviderConstructor` var, and `RegisterProvider` — every provider now registers via `Default.Register`/`RegisterFallback` in its own `init()`, so nothing calls `RegisterProvider` anymore. Remove the `strings` import if nothing else in the file uses it (it was only for `formatUnknownProviderError`).

**Explicitly flagged behavior change:** the original switch-statement had `case "ollama": return newOllamaProvider(cfg)` and `case "zai": return newZaiProvider(cfg)` — early returns that skipped the `if debug { EnableDebugLogging(p) }` line entirely, so `--debug` never applied to Ollama/Z.ai even when passed (Anthropic and OpenAI-compatible always fell through to the debug check and worked correctly). The unified `Default.New(cfg)` call above has no per-provider branching, so it applies debug-logging uniformly to every provider — this closes that latent gap for Ollama and Z.ai. This is a real, positive behavior change, not incidental noise to hide: call it out in the commit message and PR description.

- [ ] **Step 6: Collapse `main.go`'s three `if` blocks into one generic call**

In `cmd/rubichan/main.go`'s `loadConfig()`, replace all three of these blocks (anthropic, zai, ollama — by now each looks like `if cfg.Provider.Default == "X" && cfg.Provider.Model == "" { model, err := provider.Default.ResolveDefaultModel(...); ...}`):

```go
	if cfg.Provider.Default == "anthropic" && cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
	}

	if cfg.Provider.Default == "zai" && cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
	}

	if cfg.Provider.Default == "ollama" && cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
		fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
	}
```

with:

```go
	// Resolve the provider's default model if none was specified via
	// --model or config. Each provider's own resolution logic (constant,
	// config-driven fallback, or dynamic lookup) lives in its ProviderDef
	// (internal/provider/{anthropic,zai,ollama}). Providers with no
	// DefaultModel resolver (e.g. custom OpenAI-compatible endpoints) leave
	// cfg.Provider.Model unset, matching prior behavior.
	if cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		switch {
		case err == nil:
			cfg.Provider.Model = model
			if cfg.Provider.Default == "ollama" {
				fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
			}
		case errors.Is(err, provider.ErrNoDefaultModel):
			// No default-model resolution for this provider — leave Model
			// empty, same as before this provider had a Registry entry.
		default:
			return nil, err
		}
	}
```

`"errors"` is already imported in `cmd/rubichan/main.go` (line 8) — no import changes needed for this step.

- [ ] **Step 7: Run the full suite (regression check)**

Run: `go test ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS, including every `TestLoadConfig_*` and `TestNewProvider*` test, unmodified. `grep -rn "newOpenAIProvider\|RegisterProvider\b" internal/provider/` should return no matches (fully removed).

- [ ] **Step 8: Format, vet, commit**

Run: `gofmt -l internal/provider/openai/provider.go internal/provider/openai/provider_test.go internal/provider/factory.go cmd/rubichan/main.go` and `go vet ./internal/provider/... ./cmd/rubichan/...`.

```bash
git add internal/provider/openai/provider.go internal/provider/openai/provider_test.go internal/provider/factory.go cmd/rubichan/main.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Migrate OpenAI-compatible, remove the switch-statement scaffolding

Last provider migration: registers via RegisterFallback (handles any
provider name in cfg.Provider.OpenAI, not one fixed ID), then removes
factory.go's now-fully-unused switch/registry/RegisterProvider and
collapses main.go's three provider-gated default-model blocks into one
generic call, safe now that every provider (including this fallback) is
registered and ErrNoDefaultModel distinguishes "no resolver" from a real
failure.

Flagging one incidental behavior fix: the old switch's early-return
branches for Ollama/Z.ai skipped debug-log wiring even with --debug set;
the unified dispatch applies it uniformly to every provider now.
EOF
)"
```

---

## Final verification (after Task 5)

- [ ] Run: `go build ./...` — expect success.
- [ ] Run: `gofmt -l .` — expect no output.
- [ ] Run: `go vet ./...` — expect no issues.
- [ ] Run: `go test ./...` — expect all packages passing.
- [ ] Run: `go test -race ./internal/provider/... ./cmd/rubichan/...` — expect all passing (matches the verification rigor used in PR #329).
- [ ] Run: `grep -rn "RegisterProvider\b\|newAnthropicProvider\|newZaiProvider\|newOllamaProvider\|newOpenAIProvider\|resolveOllamaModel" internal/ cmd/` — expect zero matches; confirms nothing from the old mechanism survives.
- [ ] Confirm `internal/provider/factory.go` is ~20 lines (just `NewProvider`/`NewProviderWithDebug`) and every provider construction/default-model concern lives in exactly one place: that provider's own `providerDef()`.
