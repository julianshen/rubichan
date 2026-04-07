package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/provider/ssecompat"
)

func init() {
	provider.RegisterProvider("openai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey, extraHeaders)
	})
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

// apiRequest is the request body sent to the OpenAI API.
type apiRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type apiMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type apiTool struct {
	Type     string      `json:"type"`
	Function apiFunction `json:"function"`
}

type apiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type apiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function apiCallFunc `json:"function"`
}

type apiCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	go ssecompat.ProcessSSE(ctx, resp.Body, ch)

	return ch, nil
}
