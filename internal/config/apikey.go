package config

import (
	"fmt"
	"os"
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
