package provider

import (
	"fmt"

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
		return nil, fmt.Errorf("creating provider: %w", err)
	}

	if debug {
		EnableDebugLogging(p)
	}

	return p, nil
}
