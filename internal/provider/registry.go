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
