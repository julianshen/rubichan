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

	"github.com/julianshen/rubichan/internal/provider"
)

func init() {
	provider.RegisterProvider("ollama", func(baseURL, _ string, _ map[string]string) provider.LLMProvider {
		return New(baseURL)
	})
}

// Provider implements the LLMProvider interface for Ollama (local LLM server).
type Provider struct {
	baseURL    string
	client     *http.Client
	nextToolID atomic.Int64
}

// New creates a new Ollama provider.
func New(baseURL string) *Provider {
	return &Provider{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// apiRequest is the request body sent to the Ollama API.
type apiRequest struct {
	Model    string       `json:"model"`
	Messages []apiMessage `json:"messages"`
	Tools    []apiTool    `json:"tools,omitempty"`
	Stream   bool         `json:"stream"`
	Options  *apiOptions  `json:"options,omitempty"`
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

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan provider.StreamEvent)
	go p.processStream(ctx, resp.Body, ch)

	return ch, nil
}

func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	apiReq := apiRequest{
		Model:  req.Model,
		Stream: true,
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
			text += block.Text
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
			texts = append(texts, block.Text)
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
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
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
			select {
			case ch <- provider.StreamEvent{Type: "stop"}:
			case <-ctx.Done():
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
		}
	}
}
