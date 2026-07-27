package zai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/provider/openai"
	"github.com/julianshen/rubichan/internal/provider/ssecompat"
)

func init() {
	provider.Default.Register(providerDef())
}

// providerDef describes this provider's construction, auth, and default
// model for provider.Default. Exposed as a function so tests can exercise
// it directly, isolated from the shared provider.Default registry.
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "zai",
		Constructor: func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
			return New(baseURL, apiKey, "glm-5", extraHeaders)
		},
		BaseURL: func(cfg *config.Config) string {
			if cfg.Provider.Zai.BaseURL != "" {
				return cfg.Provider.Zai.BaseURL
			}
			return "https://api.z.ai/api/coding/paas/v4"
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			apiKey, err := config.ResolveAPIKey(
				cfg.Provider.Zai.APIKeySource,
				cfg.Provider.Zai.APIKey,
				"Z_AI_API_KEY",
			)
			if err != nil {
				return "", nil, fmt.Errorf("resolving Z.ai API key: %w", err)
			}
			return apiKey, nil, nil
		},
		DefaultModel: func(_ context.Context, cfg *config.Config) (string, error) {
			if cfg.Provider.Zai.Model != "" {
				return cfg.Provider.Zai.Model, nil
			}
			return "glm-5", nil
		},
	}
}

// Provider implements the LLMProvider interface for Z.ai API.
type Provider struct {
	baseURL      string
	apiKey       string
	model        string
	extraHeaders map[string]string
	client       *http.Client
	transformer  openai.Transformer
	debugLogger  provider.DebugLogger
}

// SetDebugLogger enables debug logging for API requests and responses.
func (p *Provider) SetDebugLogger(logger provider.DebugLogger) {
	p.debugLogger = logger
}

// New creates a new Z.ai provider.
func New(baseURL, apiKey, model string, extraHeaders map[string]string) *Provider {
	if extraHeaders == nil {
		extraHeaders = make(map[string]string)
	}
	if model == "" {
		model = "glm-5"
	}
	return &Provider{
		baseURL:      baseURL,
		apiKey:       apiKey,
		model:        model,
		extraHeaders: extraHeaders,
		client:       provider.NewHTTPClient(),
	}
}

// SetHTTPClient replaces the default HTTP client. This is intended for
// testing with custom transports (e.g. in-memory mem:// servers).
func (p *Provider) SetHTTPClient(c *http.Client) {
	p.client = c
}

// Stream sends a completion request to the Z.ai API and returns a channel of StreamEvents.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	// Resolve default model if not specified in the request.
	if req.Model == "" {
		req.Model = p.model
	}

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
		return nil, provider.ClassifyAPIErrorWithResponse(resp.StatusCode, respBody, httpReq, "zai", resp.Header)
	}

	if p.debugLogger != nil {
		p.debugLogger("[DEBUG] <<< HTTP Response: %d %s (streaming)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	ch := make(chan provider.StreamEvent)
	go ssecompat.ProcessSSE(ctx, resp.Body, ch, "zai")

	return ch, nil
}
