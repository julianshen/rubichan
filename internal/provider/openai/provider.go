package openai

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
		client:       &http.Client{},
	}
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
	Content    interface{}   `json:"content,omitempty"`
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

// Stream sends a completion request to the OpenAI-compatible API and returns a
// channel of StreamEvents.
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
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan provider.StreamEvent)
	go p.processStream(ctx, resp.Body, ch)

	return ch, nil
}

func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	apiReq := apiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}

	if req.Temperature != 0 {
		temp := req.Temperature
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
		apiReq.Messages = append(apiReq.Messages, p.convertMessage(msg))
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

func (p *Provider) convertMessage(msg provider.Message) apiMessage {
	switch msg.Role {
	case "assistant":
		return p.convertAssistantMessage(msg)
	case "user":
		return p.convertUserMessage(msg)
	default:
		// Fallback: concatenate text blocks
		var texts []string
		for _, block := range msg.Content {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		return apiMessage{
			Role:    msg.Role,
			Content: strings.Join(texts, ""),
		}
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
	if text != "" {
		apiMsg.Content = text
	}
	if len(toolCalls) > 0 {
		apiMsg.ToolCalls = toolCalls
	}

	return apiMsg
}

func (p *Provider) convertUserMessage(msg provider.Message) apiMessage {
	// Check if this is a tool result message
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return apiMessage{
				Role:       "tool",
				Content:    block.Text,
				ToolCallID: block.ToolUseID,
			}
		}
	}

	// Regular user message: concatenate text blocks
	var texts []string
	for _, block := range msg.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return apiMessage{
		Role:    "user",
		Content: strings.Join(texts, ""),
	}
}

// processStream reads SSE lines from the response body and sends StreamEvents.
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

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

		// Handle tool calls
		for _, tc := range delta.ToolCalls {
			if tc.ID != "" {
				// New tool call started
				select {
				case ch <- provider.StreamEvent{
					Type: "tool_use",
					ToolUse: &provider.ToolUseBlock{
						ID:   tc.ID,
						Name: tc.Function.Name,
					},
				}:
				case <-ctx.Done():
					return
				}
			}
			if tc.Function.Arguments != "" {
				// Emit argument fragments as text_delta for accumulation
				select {
				case ch <- provider.StreamEvent{Type: "text_delta", Text: tc.Function.Arguments}:
				case <-ctx.Done():
					return
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
		}
	}
}
