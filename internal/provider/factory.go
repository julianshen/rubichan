package provider

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/config"
)

const anthropicBaseURL = "https://api.anthropic.com"

// ProviderConstructor is a function that creates a new LLMProvider.
type ProviderConstructor func(baseURL, apiKey string, extraHeaders map[string]string) LLMProvider

// KeepAliveConfigurer is implemented by providers that support configurable
// model keep-alive duration (e.g., Ollama). Defined here so the factory can
// type-assert without importing provider sub-packages.
type KeepAliveConfigurer interface {
	SetKeepAlive(duration string)
	KeepAlive() string
}

// registry holds registered provider constructors.
// This map is only written to during init() functions and read thereafter,
// so it is safe for concurrent reads without a mutex.
var registry = map[string]ProviderConstructor{}

// RegisterProvider registers a provider constructor by name.
func RegisterProvider(name string, constructor ProviderConstructor) {
	registry[name] = constructor
}

// NewProvider creates an LLMProvider based on the given configuration.
// It routes to the appropriate provider constructor based on the default provider name.
func NewProvider(cfg *config.Config) (LLMProvider, error) {
	switch cfg.Provider.Default {
	case "anthropic":
		return newAnthropicProvider(cfg)
	case "ollama":
		return newOllamaProvider(cfg)
	default:
		return newOpenAIProvider(cfg)
	}
}

func newAnthropicProvider(cfg *config.Config) (LLMProvider, error) {
	constructor, ok := registry["anthropic"]
	if !ok {
		return nil, fmt.Errorf("anthropic provider not registered")
	}

	apiKey, err := config.ResolveAPIKey(
		cfg.Provider.Anthropic.APIKeySource,
		cfg.Provider.Anthropic.APIKey,
		"ANTHROPIC_API_KEY",
	)
	if err != nil {
		return nil, fmt.Errorf("resolving Anthropic API key: %w", err)
	}

	return constructor(anthropicBaseURL, apiKey, nil), nil
}

func newOllamaProvider(cfg *config.Config) (LLMProvider, error) {
	constructor, ok := registry["ollama"]
	if !ok {
		return nil, fmt.Errorf("ollama provider not registered")
	}

	baseURL := cfg.Provider.Ollama.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434" // matches ollama.DefaultBaseURL (can't import due to cycle)
	}

	p := constructor(baseURL, "", nil)
	if ka := cfg.Agent.Cache.OllamaKeepAlive; ka != "" {
		if kac, ok := p.(KeepAliveConfigurer); ok {
			kac.SetKeepAlive(ka)
		}
	}
	return p, nil
}

func newOpenAIProvider(cfg *config.Config) (LLMProvider, error) {
	name := cfg.Provider.Default

	constructor, ok := registry["openai"]
	if !ok {
		return nil, fmt.Errorf("openai provider not registered")
	}

	for _, oc := range cfg.Provider.OpenAI {
		if oc.Name == name {
			apiKey, err := config.ResolveOpenAICompatibleAPIKey(oc)
			if err != nil {
				return nil, fmt.Errorf("resolving %s API key: %w", name, err)
			}

			return constructor(oc.BaseURL, apiKey, oc.ExtraHeaders), nil
		}
	}

	return nil, fmt.Errorf("unknown provider: %q", name)
}
