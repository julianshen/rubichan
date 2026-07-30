package openai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/provider/ssecompat"
)

func init() {
	provider.Default.RegisterFallback(providerDef())
}

// providerDef describes this provider's construction, auth, and base URL
// for provider.Default. Registered as the fallback (not an exact-ID match)
// because this provider handles any name found in cfg.Provider.OpenAI —
// "openai", "openrouter", a custom proxy name, etc. — not one fixed ID.
// DefaultModel is left nil: this provider has never had default-model
// resolution (users must pass --model), and Registry.ResolveDefaultModel's
// ErrNoDefaultModel preserves that — see main.go's loadConfig().
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "openai",
		Constructor: func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
			return New(baseURL, apiKey, extraHeaders)
		},
		BaseURL: func(cfg *config.Config) string {
			oc, _ := lookupCompatEntry(cfg)
			return oc.BaseURL
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			oc, ok := lookupCompatEntry(cfg)
			if !ok {
				return "", nil, formatUnknownProviderError(cfg.Provider.Default, cfg.Provider.OpenAI)
			}
			apiKey, err := config.ResolveOpenAICompatibleAPIKey(oc)
			if err != nil {
				return "", nil, fmt.Errorf("resolving %s API key: %w", cfg.Provider.Default, err)
			}
			return apiKey, oc.ExtraHeaders, nil
		},
	}
}

func lookupCompatEntry(cfg *config.Config) (config.OpenAICompatibleConfig, bool) {
	for _, oc := range cfg.Provider.OpenAI {
		if oc.Name == cfg.Provider.Default {
			return oc, true
		}
	}
	return config.OpenAICompatibleConfig{}, false
}

// formatUnknownProviderError builds a helpful error message when the
// requested provider name doesn't match any configured
// [[provider.openai_compatible]] entry. It lists what IS configured and
// shows example config / CLI usage.
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

// Provider implements the LLMProvider interface for OpenAI-compatible APIs.
type Provider struct {
	baseURL      string
	apiKey       string
	extraHeaders map[string]string
	client       *http.Client
	transformer  Transformer
	debugLogger  provider.DebugLogger
}

// SetDebugLogger enables debug logging for API requests and responses.
func (p *Provider) SetDebugLogger(logger provider.DebugLogger) {
	p.debugLogger = logger
}

// New creates a new OpenAI-compatible provider.
func New(baseURL, apiKey string, extraHeaders map[string]string) *Provider {
	if extraHeaders == nil {
		extraHeaders = make(map[string]string)
	}
	return &Provider{
		baseURL:      baseURL,
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
		client:       provider.NewHTTPClient(),
	}
}

// SetHTTPClient replaces the default HTTP client. This is intended for
// testing with custom transports (e.g. in-memory mem:// servers).
func (p *Provider) SetHTTPClient(c *http.Client) {
	p.client = c
}

// Stream sends a completion request to the OpenAI-compatible API and returns a
// channel of StreamEvents.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.transformer.ToProviderJSON(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	for k, v := range p.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	provider.LogRequest(p.debugLogger, httpReq, body)

	resp, err := provider.DoWithRetry(ctx, p.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		provider.LogResponse(p.debugLogger, resp.StatusCode, resp.Header, respBody)
		return nil, provider.ClassifyAPIErrorWithResponse(resp.StatusCode, respBody, httpReq, "openai", resp.Header)
	}

	if p.debugLogger != nil {
		p.debugLogger("[DEBUG] <<< HTTP Response: %d %s (streaming)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	ch := make(chan provider.StreamEvent)
	go ssecompat.ProcessSSE(ctx, resp.Body, ch, "openai")

	return ch, nil
}
