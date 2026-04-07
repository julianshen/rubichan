package zai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
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

// Type aliases to OpenAI-compatible request/response types
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

// Chunk types for parsing SSE responses
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

func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	// Use request model if provided, otherwise use provider default
	model := req.Model
	if model == "" {
		model = p.model
	}

	apiReq := apiRequest{
		Model:     model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}

	if req.Temperature != nil {
		temp := *req.Temperature
		apiReq.Temperature = &temp
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

	// Sort tools alphabetically for deterministic serialization
	sort.Slice(apiReq.Tools, func(i, j int) bool {
		return apiReq.Tools[i].Function.Name < apiReq.Tools[j].Function.Name
	})

	return json.Marshal(apiReq)
}

func (p *Provider) convertMessages(msg provider.Message) []apiMessage {
	switch msg.Role {
	case "assistant":
		return []apiMessage{p.convertAssistantMessage(msg)}
	case "user":
		return p.convertUserMessages(msg)
	default:
		// Fallback: concatenate text blocks
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
			text += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, apiToolCall{
				ID:   block.ID,
				Type: "function",
				Function: apiCallFunc{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	apiMsg := apiMessage{
		Role: "assistant",
	}
	if text != "" || len(toolCalls) > 0 {
		apiMsg.Content = text
	}
	if len(toolCalls) > 0 {
		apiMsg.ToolCalls = toolCalls
	}

	return apiMsg
}

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
			texts = append(texts, block.Text)
		}
	}

	if len(toolResults) > 0 {
		return toolResults
	}

	return []apiMessage{{
		Role:    "user",
		Content: strings.Join(texts, ""),
	}}
}

// Stream sends a completion request to the Z.ai API and returns a channel of StreamEvents.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.buildRequestBody(req)
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
// Z.ai interleaves argument fragments across multiple tool calls in the
// same response, so we must accumulate per-index and flush complete calls.
type toolCallAccumulator struct {
	calls []struct {
		id   string
		name string
		args strings.Builder
	}
}

// update processes a streamed tool call chunk.
func (a *toolCallAccumulator) update(tc chunkToolCall) {
	// Grow the slice to fit the index.
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
		// Emit a text_delta with the full arguments for the agent loop's
		// toolInputBuf accumulation.
		select {
		case ch <- provider.StreamEvent{Type: "text_delta", Text: args}:
		case <-ctx.Done():
			return
		}
	}
}

// processStream reads SSE lines from the response body and sends StreamEvents.
// Tool call arguments are accumulated per-index to handle Z.ai's interleaved
// streaming format, then flushed as complete events at stream end.
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

		// Skip empty lines and non-data lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for end of stream
		if data == "[DONE]" {
			// Flush accumulated tool calls before the stop event.
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

		// Handle text content
		if delta.Content != nil && *delta.Content != "" {
			select {
			case ch <- provider.StreamEvent{Type: "text_delta", Text: *delta.Content}:
			case <-ctx.Done():
				return
			}
		}

		// Accumulate tool call fragments by index.
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
