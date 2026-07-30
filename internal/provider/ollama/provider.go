package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
)

func init() {
	provider.Default.Register(providerDef())
}

// providerDef describes this provider's construction, auth, default model,
// and model listing for provider.Default. Exposed as a function so tests
// can exercise it directly, isolated from the shared provider.Default
// registry.
func providerDef() provider.ProviderDef {
	return provider.ProviderDef{
		ID: "ollama",
		Constructor: func(baseURL, _ string, _ map[string]string) provider.LLMProvider {
			return New(baseURL)
		},
		BaseURL: func(cfg *config.Config) string {
			return resolveBaseURL(cfg)
		},
		Auth: func(cfg *config.Config) (string, map[string]string, error) {
			return "", nil, nil // local server, no credentials
		},
		DefaultModel: resolveDefaultModel,
		ListModels:   listModels,
	}
}

func resolveBaseURL(cfg *config.Config) string {
	if cfg.Provider.Ollama.BaseURL != "" {
		return cfg.Provider.Ollama.BaseURL
	}
	return DefaultBaseURL
}

// resolveDefaultModel queries Ollama for available models and resolves
// which model to use. With a single model it auto-selects; with multiple,
// it returns the first (a future TUI picker would use listModels/ListModels
// directly instead of this auto-select).
func resolveDefaultModel(ctx context.Context, cfg *config.Config) (string, error) {
	models, err := listModels(ctx, cfg)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no models found; run 'rubichan ollama pull <model>' first")
	}
	return models[0].ID, nil
}

func listModels(ctx context.Context, cfg *config.Config) ([]provider.Model, error) {
	client := NewClient(resolveBaseURL(cfg))
	infos, err := client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing Ollama models: %w", err)
	}
	models := make([]provider.Model, len(infos))
	for i, info := range infos {
		models[i] = provider.Model{ID: info.Name, Name: info.Name}
	}
	return models, nil
}

// Provider implements the LLMProvider interface for Ollama (local LLM server).
type Provider struct {
	baseURL     string
	client      *http.Client
	nextToolID  atomic.Int64
	keepAlive   string
	debugLogger provider.DebugLogger
	watchdogCfg provider.WatchdogConfig
}

// SetDebugLogger enables debug logging for API requests and responses.
func (p *Provider) SetDebugLogger(logger provider.DebugLogger) {
	p.debugLogger = logger
}

// New creates a new Ollama provider.
func New(baseURL string) *Provider {
	return &Provider{
		baseURL: baseURL,
		client:  provider.NewHTTPClient(),
	}
}

// SetHTTPClient replaces the default HTTP client. This is intended for
// testing with custom transports (e.g. in-memory mem:// servers).
func (p *Provider) SetHTTPClient(c *http.Client) {
	p.client = c
}

// SetKeepAlive configures the keep_alive duration sent with each request.
// An empty string means the provider default ("5m") will be used.
func (p *Provider) SetKeepAlive(d string) {
	p.keepAlive = d
}

// SetWatchdogConfig overrides the stream idle-watchdog thresholds. Intended
// for tests; production code relies on the zero value (provider.WatchBody's
// 45s/90s defaults).
func (p *Provider) SetWatchdogConfig(cfg provider.WatchdogConfig) {
	p.watchdogCfg = cfg
}

// KeepAlive returns the configured keep_alive duration, or empty if unset.
func (p *Provider) KeepAlive() string {
	return p.keepAlive
}

// apiRequest is the request body sent to the Ollama API.
type apiRequest struct {
	Model     string       `json:"model"`
	Messages  []apiMessage `json:"messages"`
	Tools     []apiTool    `json:"tools,omitempty"`
	Stream    bool         `json:"stream"`
	Options   *apiOptions  `json:"options,omitempty"`
	KeepAlive string       `json:"keep_alive,omitempty"`
}

type apiOptions struct {
	NumPredict  int      `json:"num_predict,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type apiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
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
	Function apiCallFunc `json:"function"`
}

type apiCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// streamChunk represents a single line of NDJSON from the Ollama streaming response.
type streamChunk struct {
	Model   string       `json:"model"`
	Message chunkMessage `json:"message"`
	Done    bool         `json:"done"`
}

type chunkMessage struct {
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolCalls []chunkToolCall `json:"tool_calls,omitempty"`
}

type chunkToolCall struct {
	Function chunkToolFunc `json:"function"`
}

type chunkToolFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Stream sends a completion request to the Ollama API and returns a channel
// of StreamEvents parsed from the NDJSON response.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	provider.LogRequest(p.debugLogger, httpReq, body)

	resp, err := provider.DoWithRetry(ctx, p.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		provider.LogResponse(p.debugLogger, resp.StatusCode, resp.Header, respBody)
		return nil, provider.ClassifyAPIErrorWithResponse(resp.StatusCode, respBody, httpReq, "ollama", resp.Header)
	}

	if p.debugLogger != nil {
		p.debugLogger("[DEBUG] <<< HTTP Response: %d %s (streaming)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	ch := make(chan provider.StreamEvent)
	go p.processStream(ctx, resp.Body, ch)

	return ch, nil
}

func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	keepAlive := p.keepAlive
	if keepAlive == "" {
		keepAlive = "5m"
	}

	apiReq := apiRequest{
		Model:     req.Model,
		Stream:    true,
		KeepAlive: keepAlive,
	}

	// Set options if max tokens or temperature are specified
	if req.MaxTokens > 0 || req.Temperature != nil {
		opts := &apiOptions{}
		if req.MaxTokens > 0 {
			opts.NumPredict = req.MaxTokens
		}
		if req.Temperature != nil {
			temp := *req.Temperature
			opts.Temperature = &temp
		}
		apiReq.Options = opts
	}

	// Add system message if present
	if req.System != "" {
		apiReq.Messages = append(apiReq.Messages, apiMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert messages
	for _, msg := range req.Messages {
		apiReq.Messages = append(apiReq.Messages, p.convertMessages(msg)...)
	}

	// Convert tools
	for _, tool := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, apiTool{
			Type: "function",
			Function: apiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	return json.Marshal(apiReq)
}

// convertMessages converts a single provider.Message to one or more apiMessages.
func (p *Provider) convertMessages(msg provider.Message) []apiMessage {
	switch msg.Role {
	case "assistant":
		return []apiMessage{p.convertAssistantMessage(msg)}
	case "user":
		return p.convertUserMessages(msg)
	default:
		var texts []string
		for _, block := range msg.Content {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		return []apiMessage{{
			Role:    msg.Role,
			Content: strings.Join(texts, ""),
		}}
	}
}

func (p *Provider) convertAssistantMessage(msg provider.Message) apiMessage {
	var text string
	var toolCalls []apiToolCall

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				text += block.Text
			}
		case "tool_use":
			toolCalls = append(toolCalls, apiToolCall{
				Function: apiCallFunc{
					Name:      block.Name,
					Arguments: block.Input,
				},
			})
		}
	}

	apiMsg := apiMessage{
		Role:    "assistant",
		Content: text,
	}
	if len(toolCalls) > 0 {
		apiMsg.ToolCalls = toolCalls
	}

	return apiMsg
}

// convertUserMessages handles user messages that may contain tool_result blocks.
func (p *Provider) convertUserMessages(msg provider.Message) []apiMessage {
	var toolResults []apiMessage
	var texts []string

	for _, block := range msg.Content {
		switch block.Type {
		case "tool_result":
			toolResults = append(toolResults, apiMessage{
				Role:       "tool",
				Content:    block.Text,
				ToolCallID: block.ToolUseID,
			})
		case "text":
			if block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
	}

	if len(toolResults) > 0 {
		// Preserve any text blocks alongside tool results.
		if len(texts) > 0 {
			msgs := make([]apiMessage, 0, len(toolResults)+1)
			msgs = append(msgs, toolResults...)
			msgs = append(msgs, apiMessage{
				Role:    "user",
				Content: strings.Join(texts, ""),
			})
			return msgs
		}
		return toolResults
	}

	return []apiMessage{{
		Role:    "user",
		Content: strings.Join(texts, ""),
	}}
}

// processStream reads NDJSON lines from the response body and sends StreamEvents.
// The body is wrapped in an idle watchdog: Ollama can stall mid-stream
// (stuck model load, keep_alive swap, GPU busy) without closing the
// connection, and the shared HTTP client has no top-level timeout for
// exactly this reason — long streams are expected to run for minutes.
// Without the watchdog, a stall here blocks forever with no way out short
// of the caller cancelling ctx, which foreground turns don't do.
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)

	onWarn := func() {
		if p.debugLogger != nil {
			p.debugLogger("[DEBUG] ollama: stream idle for 45s, still waiting")
		}
	}
	onKill := func() {
		if p.debugLogger != nil {
			p.debugLogger("[DEBUG] ollama: stream killed after idle timeout")
		}
	}
	watched := provider.WatchBody(body, p.watchdogCfg, onWarn, onKill)
	defer watched.Close()

	gotDone := false

	scanner := bufio.NewScanner(watched)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}

		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing chunk: %w", err)}:
			case <-ctx.Done():
			}
			continue
		}

		// Handle tool calls
		for _, tc := range chunk.Message.ToolCalls {
			argsJSON := json.RawMessage(tc.Function.Arguments)
			if argsJSON == nil {
				argsJSON = json.RawMessage(`{}`)
			}

			select {
			case ch <- provider.StreamEvent{
				Type: "tool_use",
				ToolUse: &provider.ToolUseBlock{
					ID:    fmt.Sprintf("ollama_call_%d", p.nextToolID.Add(1)),
					Name:  tc.Function.Name,
					Input: json.RawMessage(argsJSON),
				},
			}:
			case <-ctx.Done():
				return
			}
		}

		// Handle text content
		if chunk.Message.Content != "" {
			select {
			case ch <- provider.StreamEvent{Type: "text_delta", Text: chunk.Message.Content}:
			case <-ctx.Done():
				return
			}
		}

		// Handle done signal
		if chunk.Done {
			gotDone = true
			select {
			case ch <- provider.StreamEvent{Type: "stop"}:
			case <-ctx.Done():
			}
			break
		}
	}

	if gotDone {
		return
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
		}
		return
	}

	// Stream ended without a done: true chunk — connection was dropped.
	if !gotDone {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: fmt.Errorf("stream ended without done signal")}:
		case <-ctx.Done():
		}
	}
}
