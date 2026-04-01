package provider

import (
	"fmt"
	"strings"

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
// Use NewProviderWithDebug to enable debug logging of API requests/responses.
func NewProvider(cfg *config.Config) (LLMProvider, error) {
	return NewProviderWithDebug(cfg, false)
}

// NewProviderWithDebug creates an LLMProvider and optionally enables debug
// logging of HTTP request/response details to stderr via log.Printf.
func NewProviderWithDebug(cfg *config.Config, debug bool) (LLMProvider, error) {
	var p LLMProvider
	var err error

	switch cfg.Provider.Default {
	case "anthropic":
		p, err = newAnthropicProvider(cfg)
	case "ollama":
		p, err = newOllamaProvider(cfg)
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

	return nil, formatUnknownProviderError(name, cfg.Provider.OpenAI)
}

// formatUnknownProviderError builds a helpful error message when the requested
// provider name doesn't match any configured [[provider.openai_compatible]]
// entry. It lists what IS configured and shows example config / CLI usage.
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
