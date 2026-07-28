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

// TestRegisterRejectsIncompleteDef pins the registration-time contract.
// New and ResolveDefaultModel call Constructor, BaseURL and Auth without
// nil checks, so a def missing one of them panics on the first request
// that reaches it — far from the init() that registered it, with a bare
// nil-function-call and no provider name. Failing at registration keeps
// the blame at the offending provider.
func TestRegisterRejectsIncompleteDef(t *testing.T) {
	t.Parallel()

	complete := func() provider.ProviderDef {
		return provider.ProviderDef{
			ID:          "test",
			Constructor: func(string, string, map[string]string) provider.LLMProvider { return nil },
			BaseURL:     func(*config.Config) string { return "" },
			Auth:        func(*config.Config) (string, map[string]string, error) { return "", nil, nil },
		}
	}

	tests := []struct {
		name    string
		mutate  func(*provider.ProviderDef)
		missing string
	}{
		{"missing Constructor", func(d *provider.ProviderDef) { d.Constructor = nil }, "Constructor"},
		{"missing BaseURL", func(d *provider.ProviderDef) { d.BaseURL = nil }, "BaseURL"},
		{"missing Auth", func(d *provider.ProviderDef) { d.Auth = nil }, "Auth"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			def := complete()
			tt.mutate(&def)

			r := provider.NewRegistry()
			require.PanicsWithError(t, `provider "test": `+tt.missing+" is required",
				func() { r.Register(def) })
			require.PanicsWithError(t, `provider "test": `+tt.missing+" is required",
				func() { r.RegisterFallback(def) })
		})
	}
}

// TestRegisterAcceptsCompleteDef guards against the validation rejecting
// a valid registration — the optional resolvers stay optional.
func TestRegisterAcceptsCompleteDef(t *testing.T) {
	t.Parallel()

	def := provider.ProviderDef{
		ID:          "test",
		Constructor: func(string, string, map[string]string) provider.LLMProvider { return nil },
		BaseURL:     func(*config.Config) string { return "" },
		Auth:        func(*config.Config) (string, map[string]string, error) { return "", nil, nil },
	}

	r := provider.NewRegistry()
	require.NotPanics(t, func() { r.Register(def) })
	require.NotPanics(t, func() { r.RegisterFallback(def) })
}

// TestNewCallsAuthBeforeBaseURL pins an ordering the OpenAI-compatible
// provider depends on for correctness.
//
// Its BaseURL discards the lookup's ok flag and returns the zero entry's
// empty URL for an unknown provider, while its Auth returns a descriptive
// error for that same case. Because New authenticates first, the error
// surfaces and BaseURL's silent empty string never reaches a constructor.
// Reversing the order — or evaluating BaseURL into a variable before the
// Auth call — would turn a clear "unknown provider" error into a provider
// built against an empty base URL, failing later and less legibly.
func TestNewCallsAuthBeforeBaseURL(t *testing.T) {
	t.Parallel()

	var calls []string
	def := provider.ProviderDef{
		ID: "test",
		Constructor: func(string, string, map[string]string) provider.LLMProvider {
			calls = append(calls, "Constructor")
			return &fakeProvider{}
		},
		BaseURL: func(*config.Config) string {
			calls = append(calls, "BaseURL")
			return ""
		},
		Auth: func(*config.Config) (string, map[string]string, error) {
			calls = append(calls, "Auth")
			return "", nil, nil
		},
	}

	r := provider.NewRegistry()
	r.Register(def)

	cfg := &config.Config{}
	cfg.Provider.Default = "test"
	_, err := r.New(cfg)
	require.NoError(t, err)

	require.Equal(t, []string{"Auth", "BaseURL", "Constructor"}, calls,
		"New must authenticate before resolving the base URL")
}

// TestNewSkipsBaseURLWhenAuthFails is the consequence of that ordering:
// a failed Auth short-circuits, so BaseURL never runs on a config it
// cannot resolve.
func TestNewSkipsBaseURLWhenAuthFails(t *testing.T) {
	t.Parallel()

	baseURLCalled := false
	def := provider.ProviderDef{
		ID:          "test",
		Constructor: func(string, string, map[string]string) provider.LLMProvider { return &fakeProvider{} },
		BaseURL: func(*config.Config) string {
			baseURLCalled = true
			return ""
		},
		Auth: func(*config.Config) (string, map[string]string, error) {
			return "", nil, errors.New("no credentials")
		},
	}

	r := provider.NewRegistry()
	r.Register(def)

	cfg := &config.Config{}
	cfg.Provider.Default = "test"
	_, err := r.New(cfg)
	require.Error(t, err)
	require.False(t, baseURLCalled, "BaseURL must not run once Auth has failed")
}
