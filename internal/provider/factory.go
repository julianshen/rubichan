package provider

import (
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/config"
)

const anthropicBaseURL = "https://api.anthropic.com"

// ProviderConstructor is a function that creates a new LLMProvider.
type ProviderConstructor func(baseURL, apiKey string, extraHeaders map[string]string) LLMProvider

// registry holds registered provider constructors.
var registry = map[string]ProviderConstructor{}

// RegisterProvider registers a provider constructor by name.
func RegisterProvider(name string, constructor ProviderConstructor) {
	registry[name] = constructor
}

// NewProvider creates an LLMProvider based on the given configuration.
// If the default provider is "anthropic", it creates an Anthropic provider.
// Otherwise, it searches the OpenAI-compatible configurations for a matching name.
func NewProvider(cfg *config.Config) (LLMProvider, error) {
	if cfg.Provider.Default == "anthropic" {
		return newAnthropicProvider(cfg)
	}

	return newOpenAIProvider(cfg)
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

func newOpenAIProvider(cfg *config.Config) (LLMProvider, error) {
	name := cfg.Provider.Default

	constructor, ok := registry["openai"]
	if !ok {
		return nil, fmt.Errorf("openai provider not registered")
	}

	for _, oc := range cfg.Provider.OpenAI {
		if oc.Name == name {
			envVar := strings.ToUpper(name) + "_API_KEY"
			apiKey, err := config.ResolveAPIKey(oc.APIKeySource, oc.APIKey, envVar)
			if err != nil {
				return nil, fmt.Errorf("resolving %s API key: %w", name, err)
			}

			return constructor(oc.BaseURL, apiKey, oc.ExtraHeaders), nil
		}
	}

	return nil, fmt.Errorf("unknown provider: %q", name)
}
