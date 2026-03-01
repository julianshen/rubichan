package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config represents the top-level application configuration.
type Config struct {
	Provider ProviderConfig `toml:"provider"`
	Agent    AgentConfig    `toml:"agent"`
	Skills   SkillsConfig   `toml:"skills"`
	MCP      MCPConfig      `toml:"mcp"`
	Security SecurityConfig `toml:"security"`
}

// SecurityConfig holds settings for the security scanning engine.
type SecurityConfig struct {
	FailOn            string   `toml:"fail_on"`
	EnableLLMAnalysis bool     `toml:"enable_llm_analysis"`
	MaxLLMCalls       int      `toml:"max_llm_calls"`
	ExcludePatterns   []string `toml:"exclude_patterns"`
}

// ProviderConfig holds settings for AI provider selection and configuration.
type ProviderConfig struct {
	Default   string                   `toml:"default"`
	Model     string                   `toml:"model"`
	Anthropic AnthropicProviderConfig  `toml:"anthropic"`
	OpenAI    []OpenAICompatibleConfig `toml:"openai_compatible"`
	Ollama    OllamaProviderConfig     `toml:"ollama"`
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

// OllamaProviderConfig holds Ollama-specific provider settings.
type OllamaProviderConfig struct {
	BaseURL string `toml:"base_url"`
}

// AgentConfig holds settings for the agent behavior.
type AgentConfig struct {
	MaxTurns               int             `toml:"max_turns"`
	ApprovalMode           string          `toml:"approval_mode"`
	ContextBudget          int             `toml:"context_budget"`
	MaxOutputTokens        int             `toml:"max_output_tokens"`
	CompactTrigger         float64         `toml:"compact_trigger"`
	HardBlock              float64         `toml:"hard_block"`
	ResultOffloadThreshold int             `toml:"result_offload_threshold"`
	ToolDeferralThreshold  float64         `toml:"tool_deferral_threshold"`
	TrustRules             []TrustRuleConf `toml:"trust_rules"`
}

// TrustRuleConf defines a trust rule in the configuration file.
// Trust rules enable input-sensitive approval caching: matching tool calls
// are auto-approved without prompting the user.
type TrustRuleConf struct {
	Tool    string `toml:"tool"`    // Tool name to match, or "*" for all tools
	Pattern string `toml:"pattern"` // Regex pattern matched against tool input values
	Action  string `toml:"action"`  // "allow" or "deny"
}

// SkillsConfig holds settings for the skill system.
type SkillsConfig struct {
	RegistryURL         string   `toml:"registry_url"`
	ApprovedSkills      []string `toml:"approved_skills"`
	UserDir             string   `toml:"user_dir"`
	MaxLLMCallsPerTurn  int      `toml:"max_llm_calls_per_turn"`
	MaxShellExecPerTurn int      `toml:"max_shell_exec_per_turn"`
	MaxNetFetchPerTurn  int      `toml:"max_net_fetch_per_turn"`
}

// MCPConfig holds settings for MCP (Model Context Protocol) server connections.
type MCPConfig struct {
	Servers []MCPServerConfig `toml:"servers"`
}

// MCPServerConfig describes a single MCP server connection.
type MCPServerConfig struct {
	Name      string   `toml:"name"`
	Transport string   `toml:"transport"` // "stdio" or "sse"
	Command   string   `toml:"command"`   // for stdio transport
	Args      []string `toml:"args"`      // for stdio transport
	URL       string   `toml:"url"`       // for sse transport
}

// Validate checks that the MCPServerConfig fields are consistent.
func (c MCPServerConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("mcp server: name is required")
	}
	switch c.Transport {
	case "stdio":
		if c.Command == "" {
			return fmt.Errorf("mcp server %q: command is required for stdio transport", c.Name)
		}
	case "sse":
		if c.URL == "" {
			return fmt.Errorf("mcp server %q: url is required for sse transport", c.Name)
		}
	case "":
		return fmt.Errorf("mcp server %q: transport is required (stdio or sse)", c.Name)
	default:
		return fmt.Errorf("mcp server %q: unknown transport %q (must be stdio or sse)", c.Name, c.Transport)
	}
	return nil
}

// Load reads a TOML config file from the given path and returns a Config.
// The returned Config starts with default values and is overridden by values
// found in the file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Validate MCP server configs.
	for _, srv := range cfg.MCP.Servers {
		if err := srv.Validate(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// Save writes the Config to the given path as TOML. It creates parent
// directories if they don't exist.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return nil
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
			MaxTurns:               50,
			ApprovalMode:           "prompt",
			ContextBudget:          100000,
			MaxOutputTokens:        4096,
			CompactTrigger:         0.95,
			HardBlock:              0.98,
			ResultOffloadThreshold: 4096,
			ToolDeferralThreshold:  0.10,
		},
		Skills: SkillsConfig{
			RegistryURL:         "https://registry.rubichan.dev",
			MaxLLMCallsPerTurn:  10,
			MaxShellExecPerTurn: 20,
			MaxNetFetchPerTurn:  10,
		},
		Security: SecurityConfig{
			FailOn:      "high",
			MaxLLMCalls: 10,
		},
	}
}
