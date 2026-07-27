# Provider Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `internal/provider/factory.go`'s switch-statement and `cmd/rubichan/main.go`'s three copy-pasted default-model `if`-blocks with one declarative per-provider registration (`ProviderDef`), so construction, auth, base URL, and default-model resolution for a provider are described once instead of as parallel logic in two files.

**Architecture:** A new `Registry` type in `internal/provider/registry.go` holds `ProviderDef` values (one per provider: anthropic, zai, ollama, openai-compatible), each supplying `Constructor`/`BaseURL`/`Auth` (required) and `DefaultModel`/`ListModels` (optional — nil means unsupported). `factory.go`'s public `NewProvider`/`NewProviderWithDebug` become thin delegates to the registry; `main.go`'s `loadConfig()` calls `Registry.ResolveDefaultModel` once instead of three separate blocks.

**Tech Stack:** Go, existing `internal/provider`/`internal/config` packages, `testify` (assert/require), `testutil.NewServer` for HTTP-backed provider tests.

## Global Constraints

- TDD strictly: one test at a time, Red → Green → Refactor → Commit. Never write implementation before the test.
- Commit prefixes: `[STRUCTURAL]` (no behavior change) or `[BEHAVIORAL]` (new/changed behavior). Never mix both in one commit.
- Run `go build ./...`, `go test ./...`, `gofmt -l .`, `go vet ./...` after every task; all must be clean before moving on.
- No behavior change anywhere is a hard requirement until Task 6 (the cutover). Tasks 1–5 are pure *additions* — nothing in `cmd/rubichan` or the existing `factory.go` switch-statement changes or is removed until Task 6/7. This is what makes every intermediate commit safely shippable.
- Never push to `main`. Work happens on `feature/provider-registry` (already created, spec already committed there).

---

## Prerequisite (read before starting)

PR #329 (branch `fix/ollama-stream-watchdog`, not yet merged as of this plan's writing) changed two files this plan also touches:
- `internal/config/config.go`: `DefaultConfig()` no longer bakes `Provider.Model = "claude-sonnet-4-5"`.
- `cmd/rubichan/main.go`: `loadConfig()` gained three `if cfg.Provider.Model == ""` blocks (zai, ollama, anthropic) that this plan's Task 8 replaces.

**Before starting Task 8 or Task 9**, confirm `main.go`'s `loadConfig()` has these three blocks (run `grep -n "Provider.Zai.Model\|Resolve.*default model" cmd/rubichan/main.go`). If they're missing, PR #329 hasn't merged yet — merge it first, or `git rebase main` after it merges, then re-check. Tasks 1–7 have no dependency on PR #329 and can proceed regardless.

---

## Task 1: Registry core types

**Files:**
- Create: `internal/provider/registry.go`
- Create: `internal/provider/registry_test.go`

**Interfaces:**
- Produces: `provider.Model{ID, Name string}`, `provider.ProviderDef{ID string; Constructor ProviderConstructor; BaseURL func(*config.Config) string; Auth func(*config.Config) (string, map[string]string, error); DefaultModel func(context.Context, *config.Config) (string, error); ListModels func(context.Context, *config.Config) ([]Model, error)}`, `provider.Registry` with `NewRegistry() *Registry`, package var `Default *Registry`, methods `Register(def ProviderDef)`, `RegisterFallback(def ProviderDef)`, `New(cfg *config.Config) (LLMProvider, error)`, `ResolveDefaultModel(ctx, cfg) (string, error)`, `ListModels(ctx, providerID string, cfg) ([]Model, error)`.
- `KeepAliveConfigurer` and `ProviderConstructor` move here from `factory.go` (still exist, same names — Task 7 deletes the now-duplicate copies in `factory.go`; until then both files may declare them, so don't create this file's copy yet if it would collide. To avoid a duplicate-declaration compile error in the interim, this task **moves** them out of `factory.go` now rather than duplicating.)

**Note on `KeepAliveConfigurer`/`ProviderConstructor` relocation:** moving these two type declarations from `factory.go` to `registry.go` in this task is a zero-behavior-change move (same package, same names, same call sites still compile) — do it as part of this task's commit since `registry.go` needs both types and Go doesn't allow duplicate declarations in one package. This is the one place this "pure addition" task also touches existing code, and it's mechanical (cut two type blocks from one file, paste into the new one).

- [ ] **Step 1: Write the failing test for `Registry.New` with a registered provider**

Create `internal/provider/registry_test.go`:

```go
package provider_test

import (
	"context"
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

First, remove these two blocks from `internal/provider/factory.go` (they move to `registry.go`):

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
	// caller must always specify a model explicitly).
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
// the user hasn't specified one. Returns an error if the provider has no
// DefaultModel resolver.
func (r *Registry) ResolveDefaultModel(ctx context.Context, cfg *config.Config) (string, error) {
	def, err := r.lookup(cfg.Provider.Default)
	if err != nil {
		return "", err
	}
	if def.DefaultModel == nil {
		return "", fmt.Errorf("provider %q has no default model; specify one explicitly", cfg.Provider.Default)
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

- [ ] **Step 5: Add the remaining Registry tests (unknown provider, auth error, fallback, KeepAliveConfigurer, ResolveDefaultModel, ListModels)**

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
	assert.Contains(t, err.Error(), "no default model")
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

## Task 2: Migrate Anthropic to a ProviderDef

**Files:**
- Modify: `internal/provider/anthropic/provider.go` (its `init()`, currently lines 17-21)
- Modify: `internal/provider/anthropic/provider_test.go`

**Interfaces:**
- Consumes: `provider.ProviderDef`, `provider.Default.Register` (Task 1).
- Produces: `anthropic.providerDef() provider.ProviderDef` — an unexported function tests call directly, isolated from the shared `provider.Default` registry.

**Note:** this task adds the new registration *alongside* the existing `provider.RegisterProvider("anthropic", ...)` call — both stay active. `factory.go`'s switch-statement still exclusively drives real provider construction until Task 6; this task's new path is exercised only by its own tests, matching Task 1's "pure addition" ethos.

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

- [ ] **Step 3: Add `providerDef()` and register it in `init()`**

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
	provider.RegisterProvider("anthropic", func(baseURL, apiKey string, _ map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey)
	})
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

Add `"github.com/julianshen/rubichan/internal/config"` to `internal/provider/anthropic/provider.go`'s import block (`context` and `fmt` are already imported there).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/anthropic/... -run TestProviderDef -v`
Expected: PASS

- [ ] **Step 5: Run the full anthropic package and provider package (regression check)**

Run: `go test ./internal/provider/anthropic/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS unchanged — the old `RegisterProvider` call and `factory.go`'s switch-statement are untouched, so every existing code path behaves exactly as before.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/provider/anthropic/provider.go internal/provider/anthropic/provider_test.go` and `go vet ./internal/provider/anthropic/...`.

```bash
git add internal/provider/anthropic/provider.go internal/provider/anthropic/provider_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Register Anthropic as a ProviderDef

Adds the new declarative registration alongside the existing
RegisterProvider call, which factory.go still exclusively uses until the
Task 6 cutover. providerDef() is exposed so tests exercise Auth/BaseURL/
DefaultModel directly, isolated from the shared provider.Default registry.
EOF
)"
```

---

## Task 3: Migrate Z.ai to a ProviderDef

**Files:**
- Modify: `internal/provider/zai/provider.go` (its `init()`, currently lines 15-19)
- Modify: `internal/provider/zai/provider_test.go`

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

- [ ] **Step 3: Add `providerDef()` and register it in `init()`**

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
	provider.RegisterProvider("zai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey, "glm-5", extraHeaders)
	})
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/zai/... -run TestProviderDef -v`
Expected: PASS

- [ ] **Step 5: Run the full zai package and provider package (regression check)**

Run: `go test ./internal/provider/zai/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS unchanged.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/provider/zai/provider.go internal/provider/zai/provider_test.go` and `go vet ./internal/provider/zai/...`.

```bash
git add internal/provider/zai/provider.go internal/provider/zai/provider_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Register Z.ai as a ProviderDef

Same pattern as Anthropic (Task 2). DefaultModel replicates the
cfg.Provider.Zai.Model -> "glm-5" fallback exactly.
EOF
)"
```

This completes PR 1 (Tasks 1-3: registry core + Anthropic + Z.ai). Suggest opening/pushing here before continuing to Ollama.

---

## Task 4: Migrate Ollama to a ProviderDef (construction + default model + listing)

**Files:**
- Modify: `internal/provider/ollama/provider.go` (its `init()`, currently lines 17-21)
- Modify: `internal/provider/ollama/provider_test.go`

**Interfaces:**
- Consumes: same as Task 2, plus `ollama.NewClient(baseURL string) *Client` and `Client.ListModels(ctx) ([]ModelInfo, error)` (existing, `internal/provider/ollama/client.go`), `ollama.DefaultBaseURL` (existing constant, `"http://localhost:11434"`).
- Produces: `ollama.providerDef() provider.ProviderDef`, `ollama.resolveDefaultModel(ctx, cfg) (string, error)`, `ollama.listModels(ctx, cfg) ([]provider.Model, error)` — these two replicate `cmd/rubichan/main.go`'s current `resolveOllamaModel` behavior exactly (single model → auto-select; multiple → first-of-list; zero → error `"no models found; run 'rubichan ollama pull <model>' first"`).

**Note:** this task does *not* touch `cmd/rubichan/main.go`'s existing `resolveOllamaModel` function — it stays, still used by the old path, until Task 9 deletes it. For this task's duration, the same model-resolution logic temporarily exists in two places; that's expected and resolved by Task 9, not a bug to fix now.

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

- [ ] **Step 3: Add `resolveDefaultModel`, `listModels`, `providerDef()`, and register it in `init()`**

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
	provider.RegisterProvider("ollama", func(baseURL, _ string, _ map[string]string) provider.LLMProvider {
		return New(baseURL)
	})
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/ollama/... -run 'TestResolveDefaultModel|TestListModels|TestProviderDef' -v`
Expected: PASS

- [ ] **Step 5: Run the full ollama package and provider package (regression check)**

Run: `go test ./internal/provider/ollama/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS unchanged — `cmd/rubichan/main.go`'s `resolveOllamaModel` and the old switch-statement path are both untouched.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/provider/ollama/provider.go internal/provider/ollama/provider_test.go` and `go vet ./internal/provider/ollama/...`.

```bash
git add internal/provider/ollama/provider.go internal/provider/ollama/provider_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Register Ollama as a ProviderDef with model listing

Adds resolveDefaultModel/listModels alongside the existing
cmd/rubichan/main.go resolveOllamaModel (same behavior, temporarily
duplicated — Task 9 removes the old copy once main.go is cut over).
ListModels is the first real use of the ProviderDef.ListModels extension
point; Anthropic/Z.ai don't have a listing API and leave it nil.
EOF
)"
```

This completes PR 2 (Task 4: Ollama). Suggest opening/pushing here before continuing.

---

## Task 5: Migrate the OpenAI-compatible provider to a ProviderDef (fallback registration)

**Files:**
- Modify: `internal/provider/openai/provider.go` (its `init()`, currently lines 14-18)
- Modify: `internal/provider/openai/provider_test.go`

**Interfaces:**
- Consumes: `Registry.RegisterFallback` (Task 1) — this provider handles *any* `cfg.Provider.Default` name that doesn't match an exact registration (openrouter, custom proxies, etc.), matching `factory.go`'s current `switch { ...; default: newOpenAIProvider }`.
- Produces: `openai.providerDef() provider.ProviderDef`, `openai.formatUnknownProviderError(name string, configured []config.OpenAICompatibleConfig) error` (moved here from `factory.go` — Task 7 deletes the `factory.go` copy).

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

- [ ] **Step 3: Add `providerDef()`, `formatUnknownProviderError`, register via `RegisterFallback`**

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
	provider.RegisterProvider("openai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey, extraHeaders)
	})
	provider.Default.RegisterFallback(providerDef())
}

// providerDef describes this provider's construction, auth, and base URL
// for provider.Default. Registered as the fallback (not an exact-ID match)
// because this provider handles any name found in cfg.Provider.OpenAI —
// "openai", "openrouter", a custom proxy name, etc. — not one fixed ID.
// Exposed as a function so tests can exercise it directly.
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

- [ ] **Step 5: Run the full openai package and provider package (regression check)**

Run: `go test ./internal/provider/openai/... ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS unchanged.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/provider/openai/provider.go internal/provider/openai/provider_test.go` and `go vet ./internal/provider/openai/...`.

```bash
git add internal/provider/openai/provider.go internal/provider/openai/provider_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Register the OpenAI-compatible provider as a Registry fallback

Uses RegisterFallback since this provider handles any name found in
cfg.Provider.OpenAI, not one fixed ID — mirrors factory.go's switch
default: case. formatUnknownProviderError moves here from factory.go
(duplicated for now; Task 7 removes the factory.go copy).
EOF
)"
```

---

## Task 6: Cut `factory.go` over to the registry

**Files:**
- Modify: `internal/provider/factory.go`

**Interfaces:**
- Consumes: `provider.Default.New(cfg)` (Task 1), fully populated by Tasks 2-5.
- Produces: `NewProvider`/`NewProviderWithDebug` unchanged signatures, now delegating instead of switching.

This is the actual behavioral cutover — the highest-risk single step in this plan. The old switch-statement (`newAnthropicProvider`, `newOllamaProvider`, `newZaiProvider`, `newOpenAIProvider`, the old `registry` map, `RegisterProvider`) is **not deleted yet** — it becomes unused dead code in this same file, removed cleanly in Task 7 (structural, separated per Tidy First since this task is the behavior change and Task 7 is pure deletion).

- [ ] **Step 1: Run the full existing `factory_test.go` suite first, to have a known-green baseline**

Run: `go test ./internal/provider/... -run TestNewProvider -v`
Expected: all `TestNewProvider*` tests in `internal/provider/factory_test.go` PASS (11 tests — see file for names).

- [ ] **Step 2: Change `NewProviderWithDebug` to delegate to `Default.New`**

In `internal/provider/factory.go`, replace:

```go
// NewProviderWithDebug creates an LLMProvider and optionally enables debug
// logging of HTTP request/response details to stderr via log.Printf.
func NewProviderWithDebug(cfg *config.Config, debug bool) (LLMProvider, error) {
	var p LLMProvider
	var err error

	switch cfg.Provider.Default {
	case "anthropic":
		p, err = newAnthropicProvider(cfg)
	case "ollama":
		return newOllamaProvider(cfg)
	case "zai":
		return newZaiProvider(cfg)
	default:
		p, err = newOpenAIProvider(cfg)
	}

	if err != nil {
		return nil, err
	}

	if debug {
		EnableDebugLogging(p)
	}

	return p, nil
}
```

with:

```go
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

Leave `newAnthropicProvider`, `newOllamaProvider`, `newZaiProvider`, `newOpenAIProvider`, `formatUnknownProviderError`, `anthropicBaseURL`, `registry`, and `RegisterProvider` exactly as they are in the file for now — they're unused by anything except each other after this change, but Task 7 removes them.

- [ ] **Step 3: Run the same tests to verify they still pass, now via the new path**

Run: `go test ./internal/provider/... -run TestNewProvider -v`
Expected: all still PASS — same behavior, now produced by `Registry.New` instead of the switch-statement.

- [ ] **Step 4: Run the full suite (regression check)**

Run: `go test ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS. This is the step that proves the cutover is behavior-preserving — if anything fails here, do not proceed to Task 7; investigate which `ProviderDef` doesn't match its old switch-statement counterpart.

- [ ] **Step 5: Format, vet, commit**

Run: `gofmt -l internal/provider/factory.go` and `go vet ./internal/provider/...`.

```bash
git add internal/provider/factory.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Cut NewProvider/NewProviderWithDebug over to the registry

Delegates to Default.New instead of the switch-statement. The old
switch-statement and per-provider newXxxProvider functions are now dead
code, left in place for this commit and removed in the next
(structural-only) commit so this behavioral change stays isolated and
easy to revert on its own if something regresses.
EOF
)"
```

---

## Task 7: Delete the old switch-statement machinery

**Files:**
- Modify: `internal/provider/factory.go`
- Modify: `internal/provider/anthropic/provider.go`, `internal/provider/zai/provider.go`, `internal/provider/ollama/provider.go`, `internal/provider/openai/provider.go` (remove now-redundant `RegisterProvider` calls)
- Modify: `internal/provider/factory_test.go` (remove/adapt tests that exercised deleted internals — the public-behavior tests calling `NewProvider`/`NewProviderWithDebug` don't need to change at all, since Task 6 proved they still pass)

**Interfaces:** none — pure deletion, no new interfaces produced or consumed. Structural only; run tests before and after per CLAUDE.md's refactoring guideline, revert if anything breaks.

- [ ] **Step 1: Delete the four per-provider constructor functions and `formatUnknownProviderError`/`anthropicBaseURL` from `factory.go`**

Remove from `internal/provider/factory.go`: `newAnthropicProvider`, `newOllamaProvider`, `newZaiProvider`, `newOpenAIProvider`, `formatUnknownProviderError`, the `const anthropicBaseURL = "https://api.anthropic.com"` line, the `registry map[string]ProviderConstructor` var, and `RegisterProvider`. The file should end up containing only: package/imports, `NewProvider`, `NewProviderWithDebug` (from Task 6). Update imports — `strings` is no longer used in this file (it was only for `formatUnknownProviderError`), remove it if `goimports`/`gofmt` doesn't do so automatically.

The resulting `internal/provider/factory.go`:

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

- [ ] **Step 2: Remove the now-redundant `RegisterProvider` calls from each provider's `init()`**

In each of `internal/provider/anthropic/provider.go`, `internal/provider/zai/provider.go`, `internal/provider/ollama/provider.go`, `internal/provider/openai/provider.go`, change `init()` to only call `Default.Register(providerDef())` (or `Default.RegisterFallback(providerDef())` for `openai`), removing the `provider.RegisterProvider(...)` call. Example for anthropic:

```go
func init() {
	provider.Default.Register(providerDef())
}
```

(Same shape for zai/ollama with `Register`, and openai with `RegisterFallback`.)

- [ ] **Step 3: Run the full test suite**

Run: `go test ./internal/provider/... ./cmd/rubichan/...`
Expected: all PASS. `factory_test.go`'s existing `TestNewProvider*` tests should compile and pass unmodified — they call the public `NewProvider`/`NewProviderWithDebug` functions, whose behavior Task 6 already proved is unchanged.

If any test fails: revert this task's changes (`git checkout -- <files>` for uncommitted changes) and investigate before retrying — per CLAUDE.md, a structural change that breaks tests gets reverted, not patched forward.

- [ ] **Step 4: Format, vet, commit**

Run: `gofmt -l internal/provider/factory.go internal/provider/anthropic/provider.go internal/provider/zai/provider.go internal/provider/ollama/provider.go internal/provider/openai/provider.go` and `go vet ./internal/provider/...`.

```bash
git add internal/provider/factory.go internal/provider/anthropic/provider.go internal/provider/zai/provider.go internal/provider/ollama/provider.go internal/provider/openai/provider.go
git commit -m "$(cat <<'EOF'
[STRUCTURAL] Remove the old provider switch-statement and RegisterProvider

Dead since the previous commit cut NewProvider/NewProviderWithDebug over
to the registry. Each provider's init() now registers only its
ProviderDef. No behavior change — verified by the full test suite passing
unmodified.
EOF
)"
```

This completes PR 3's first half (Tasks 5-7). Continue directly to Tasks 8-9, or pause here — Tasks 8-9 are independent of whether this is a separate PR.

---

## Task 8: Cut `main.go`'s `loadConfig()` over to `ResolveDefaultModel`

**Files:**
- Modify: `cmd/rubichan/main.go`

**Interfaces:**
- Consumes: `provider.Default.ResolveDefaultModel(ctx, cfg)` (Task 1, populated by Tasks 2-4; Task 5's OpenAI-compatible provider has no `DefaultModel`, matching today's behavior of never auto-resolving a model for it).

**Reminder:** confirm the Prerequisite section at the top of this plan before starting — this task assumes `loadConfig()` currently has the three `if cfg.Provider.Model == ""` blocks (zai, ollama, anthropic) from PR #329.

- [ ] **Step 1: Run the existing `loadConfig` tests first, to have a known-green baseline**

Run: `go test ./cmd/rubichan/... -run TestLoadConfig -v`
Expected: PASS (`TestLoadConfig_Default`, `TestLoadConfig_WithModelOverride`, `TestLoadConfig_WithProviderOverride`, `TestLoadConfig_ZaiDefaultModel_FallsBackWhenUnset`, `TestLoadConfig_ZaiDefaultModel_UsesConfiguredZaiModel`, `TestLoadConfig_AnthropicDefaultModel`, `TestLoadConfig_WithCustomConfigPath`).

- [ ] **Step 2: Replace the three `if` blocks with one `ResolveDefaultModel` call**

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

	// Resolve Ollama model if provider is ollama and no model specified.
	if cfg.Provider.Default == "ollama" && cfg.Provider.Model == "" {
		model, err := resolveOllamaModel(ollamaURL)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
		fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
	}

	// Resolve Anthropic's default model if it's the (possibly auto-detected)
	// provider and no model was specified. This runs last so it only ever
	// fills in what the provider-specific resolutions above left empty.
	if cfg.Provider.Default == "anthropic" && cfg.Provider.Model == "" {
		cfg.Provider.Model = "claude-sonnet-4-5"
	}
```

with:

```go
	// Resolve the provider's default model if none was specified via
	// --model or config. Each provider's own resolution logic (constant,
	// config-driven fallback, or dynamic lookup) lives in its ProviderDef
	// (internal/provider/{anthropic,zai,ollama}) rather than here.
	if cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
		if cfg.Provider.Default == "ollama" {
			fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
		}
	}
```

`ollamaURL` (computed just above this block) is still used by `autoDetectProvider` a few lines earlier — leave that computation in place. `context` and `provider` are both already imported in `cmd/rubichan/main.go`.

- [ ] **Step 3: Run the same tests to verify they still pass**

Run: `go test ./cmd/rubichan/... -run TestLoadConfig -v`
Expected: all still PASS — same resolved `cfg.Provider.Model` values, now produced by `ResolveDefaultModel` instead of three inline blocks.

- [ ] **Step 4: Run the full suite (regression check)**

Run: `go test ./cmd/rubichan/... ./internal/provider/...`
Expected: all PASS.

- [ ] **Step 5: Format, vet, commit**

Run: `gofmt -l cmd/rubichan/main.go` and `go vet ./cmd/rubichan/...`.

```bash
git add cmd/rubichan/main.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Cut loadConfig()'s default-model resolution over to the registry

Replaces the three provider-specific if-blocks (zai, ollama, anthropic)
with one provider.Default.ResolveDefaultModel call. Same resolved values
for every provider — verified by the existing TestLoadConfig_* suite
passing unmodified.
EOF
)"
```

---

## Task 9: Delete `resolveOllamaModel` from `main.go`

**Files:**
- Modify: `cmd/rubichan/main.go` (delete `resolveOllamaModel`, currently the function right before `loadConfig()`)
- Modify: `cmd/rubichan/main_test.go` (delete `TestResolveOllamaModel_SingleModel`, `TestResolveOllamaModel_NoModels`, `TestResolveOllamaModel_MultipleModels`, `TestResolveOllamaModel_ConnectionError` — their behavior is now covered by Task 4's `TestResolveDefaultModel_*` tests in `internal/provider/ollama/provider_test.go`)

**Interfaces:** none — pure deletion of now-fully-superseded code (Task 4's `ollama.resolveDefaultModel`/`listModels` replicate this function's exact behavior, and Task 8 stopped calling it).

- [ ] **Step 1: Confirm nothing still calls `resolveOllamaModel`**

Run: `grep -rn "resolveOllamaModel" cmd/rubichan/`
Expected: only its own definition and its 4 tests — no call sites (Task 8 removed the only one).

- [ ] **Step 2: Delete the function from `main.go`**

Remove this function from `cmd/rubichan/main.go` (immediately precedes `loadConfig()`):

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

Leave the `ollama` package import in place — `autoDetectProvider` still uses `ollama.NewClient(...).IsRunning(...)`.

- [ ] **Step 3: Delete its tests from `main_test.go`**

Remove `TestResolveOllamaModel_SingleModel`, `TestResolveOllamaModel_NoModels`, `TestResolveOllamaModel_MultipleModels` (all three currently just before `capabilityTestProvider`), and `TestResolveOllamaModel_ConnectionError` (currently just before `// saveFlags saves...`) from `cmd/rubichan/main_test.go`.

- [ ] **Step 4: Run the full suite**

Run: `go test ./cmd/rubichan/... ./internal/provider/...`
Expected: all PASS — Ollama default-model-resolution coverage now lives entirely in `internal/provider/ollama/provider_test.go` (Task 4).

- [ ] **Step 5: Format, vet, commit**

Run: `gofmt -l cmd/rubichan/main.go cmd/rubichan/main_test.go` and `go vet ./cmd/rubichan/...`.

```bash
git add cmd/rubichan/main.go cmd/rubichan/main_test.go
git commit -m "$(cat <<'EOF'
[STRUCTURAL] Remove resolveOllamaModel, now fully superseded

ollama.resolveDefaultModel/listModels (internal/provider/ollama) replicate
its exact behavior and have their own test coverage since Task 4; nothing
has called this copy since Task 8. autoDetectProvider's ollama.NewClient
usage is untouched.
EOF
)"
```

---

## Final verification (after Task 9)

- [ ] Run: `go build ./...` — expect success.
- [ ] Run: `gofmt -l .` — expect no output.
- [ ] Run: `go vet ./...` — expect no issues.
- [ ] Run: `go test ./...` — expect all packages passing, same total test count as before this plan started minus the 4 deleted `TestResolveOllamaModel_*` tests plus every new test this plan added.
- [ ] Run: `go test -race ./internal/provider/... ./cmd/rubichan/...` — expect all passing (matches the verification rigor used in PR #329).
- [ ] Confirm `internal/provider/factory.go` is now ~20 lines (just `NewProvider`/`NewProviderWithDebug`) and every provider construction/default-model concern lives in exactly one place: that provider's own `providerDef()`.
