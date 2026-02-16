package config

// Config represents the top-level application configuration.
type Config struct {
	Provider ProviderConfig `toml:"provider"`
	Agent    AgentConfig    `toml:"agent"`
}

// ProviderConfig holds settings for AI provider selection and configuration.
type ProviderConfig struct {
	Default   string                   `toml:"default"`
	Model     string                   `toml:"model"`
	Anthropic AnthropicProviderConfig  `toml:"anthropic"`
	OpenAI    []OpenAICompatibleConfig `toml:"openai_compatible"`
}

// AnthropicProviderConfig holds Anthropic-specific provider settings.
type AnthropicProviderConfig struct {
	APIKeySource string `toml:"api_key_source"`
	APIKey       string `toml:"api_key"`
}

// OpenAICompatibleConfig holds settings for an OpenAI-compatible provider.
type OpenAICompatibleConfig struct {
	Name         string            `toml:"name"`
	BaseURL      string            `toml:"base_url"`
	APIKeySource string            `toml:"api_key_source"`
	APIKey       string            `toml:"api_key"`
	ExtraHeaders map[string]string `toml:"extra_headers"`
}

// AgentConfig holds settings for the agent behavior.
type AgentConfig struct {
	MaxTurns      int    `toml:"max_turns"`
	ApprovalMode  string `toml:"approval_mode"`
	ContextBudget int    `toml:"context_budget"`
}

// DefaultConfig returns a Config populated with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Provider: ProviderConfig{
			Default: "anthropic",
			Model:   "claude-sonnet-4-5",
			Anthropic: AnthropicProviderConfig{
				APIKeySource: "env",
			},
		},
		Agent: AgentConfig{
			MaxTurns:      50,
			ApprovalMode:  "prompt",
			ContextBudget: 100000,
		},
	}
}
