package zai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/provider/openai"
)

func init() {
	provider.RegisterProvider("zai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey, "glm-5", extraHeaders)
	})
}

// Provider implements the LLMProvider interface for Z.ai API.
type Provider struct {
	baseURL      string
	apiKey       string
	model        string
	extraHeaders map[string]string
	client       *http.Client
	transformer  openai.Transformer
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

// SSE chunk types for parsing streaming responses.
type chatChunk struct {
	Choices []chunkChoice `json:"choices"`
}

type chunkChoice struct {
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type chunkDelta struct {
	Content   *string         `json:"content"`
	ToolCalls []chunkToolCall `json:"tool_calls"`
}

type chunkToolCall struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function chunkToolFunc `json:"function,omitempty"`
}

type chunkToolFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
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

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, provider.ClassifyAPIErrorWithResponse(resp.StatusCode, respBody, httpReq, "zai", resp.Header)
	}

	ch := make(chan provider.StreamEvent)
	go p.processStream(ctx, resp.Body, ch)

	return ch, nil
}

// toolCallAccumulator tracks in-flight tool calls by their streamed index.
type toolCallAccumulator struct {
	calls []struct {
		id   string
		name string
		args strings.Builder
	}
}

// update processes a streamed tool call chunk.
func (a *toolCallAccumulator) update(tc chunkToolCall) {
	for len(a.calls) <= tc.Index {
		a.calls = append(a.calls, struct {
			id   string
			name string
			args strings.Builder
		}{})
	}
	if tc.ID != "" {
		a.calls[tc.Index].id = tc.ID
	}
	if tc.Function.Name != "" {
		a.calls[tc.Index].name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		a.calls[tc.Index].args.WriteString(tc.Function.Arguments)
	}
}

// flush emits all accumulated tool calls as complete StreamEvents.
func (a *toolCallAccumulator) flush(ctx context.Context, ch chan<- provider.StreamEvent) {
	for _, call := range a.calls {
		if call.id == "" {
			continue
		}
		args := call.args.String()
		if args == "" {
			args = "{}"
		}
		select {
		case ch <- provider.StreamEvent{
			Type: "tool_use",
			ToolUse: &provider.ToolUseBlock{
				ID:    call.id,
				Name:  call.name,
				Input: json.RawMessage(args),
			},
		}:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- provider.StreamEvent{Type: "text_delta", Text: args}:
		case <-ctx.Done():
			return
		}
	}
}

// processStream reads SSE lines from the response body and sends StreamEvents.
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	var toolAcc toolCallAccumulator

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			toolAcc.flush(ctx, ch)
			select {
			case ch <- provider.StreamEvent{Type: "stop"}:
			case <-ctx.Done():
			}
			return
		}

		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing chunk: %w", err)}:
			case <-ctx.Done():
			}
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		if delta.Content != nil && *delta.Content != "" {
			select {
			case ch <- provider.StreamEvent{Type: "text_delta", Text: *delta.Content}:
			case <-ctx.Done():
				return
			}
		}

		for _, tc := range delta.ToolCalls {
			toolAcc.update(tc)
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
		}
	}
}
