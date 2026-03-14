package config

import (
	"fmt"
	"os"
	"strings"
)

// ResolveAPIKey resolves an API key based on the given source.
// Supported sources: "env" (from environment variable), "config" (from config value),
// "keyring" (currently falls back to env).
func ResolveAPIKey(source, configValue, envVar string) (string, error) {
	switch source {
	case "keyring":
		return resolveFromEnv(envVar)
	case "env":
		return resolveFromEnv(envVar)
	case "config":
		if configValue == "" {
			return "", fmt.Errorf("api_key_source is 'config' but no api_key value provided")
		}
		return configValue, nil
	default:
		return "", fmt.Errorf("unknown api_key_source: %q", source)
	}
}

// OpenAICompatibleEnvVar returns the canonical environment variable name for
// an OpenAI-compatible provider entry (for example, "openrouter" ->
// "OPENROUTER_API_KEY").
func OpenAICompatibleEnvVar(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ToUpper(name) + "_API_KEY"
}

// ResolveOpenAICompatibleAPIKey resolves credentials for an OpenAI-compatible
// provider entry using the same rules as provider construction.
func ResolveOpenAICompatibleAPIKey(oc OpenAICompatibleConfig) (string, error) {
	return ResolveAPIKey(oc.APIKeySource, oc.APIKey, OpenAICompatibleEnvVar(oc.Name))
}

// HasUsableCredentialsForProvider reports whether the named provider is
// configured well enough to be used right now.
func HasUsableCredentialsForProvider(cfg *Config, providerName string) bool {
	if cfg == nil {
		return false
	}

	switch providerName {
	case "ollama":
		return true
	case "anthropic":
		_, err := ResolveAPIKey(
			cfg.Provider.Anthropic.APIKeySource,
			cfg.Provider.Anthropic.APIKey,
			"ANTHROPIC_API_KEY",
		)
		return err == nil
	default:
		for _, oc := range cfg.Provider.OpenAI {
			if oc.Name != providerName {
				continue
			}
			_, err := ResolveOpenAICompatibleAPIKey(oc)
			return err == nil
		}
		return false
	}
}

// HasUsableCredentialsForDefaultProvider reports whether the config's default
// provider can be used immediately.
func HasUsableCredentialsForDefaultProvider(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return HasUsableCredentialsForProvider(cfg, cfg.Provider.Default)
}

func resolveFromEnv(envVar string) (string, error) {
	if envVar == "" {
		return "", fmt.Errorf("no environment variable name specified")
	}
	val := os.Getenv(envVar)
	if val == "" {
		return "", fmt.Errorf("environment variable %s is not set", envVar)
	}
	return val, nil
}
