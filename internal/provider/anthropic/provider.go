package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// Provider implements the LLMProvider interface for the Anthropic API.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates a new Anthropic provider.
func New(baseURL, apiKey string) *Provider {
	return &Provider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// apiRequest is the request body sent to the Anthropic API.
type apiRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	Stream      bool         `json:"stream"`
	System      string       `json:"system,omitempty"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
}

type apiMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type apiTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Stream sends a completion request to the Anthropic API and returns a channel
// of StreamEvents parsed from the SSE response.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
		System:    req.System,
	}

	if req.Temperature != 0 {
		temp := req.Temperature
		apiReq.Temperature = &temp
	}

	// Convert messages
	for _, msg := range req.Messages {
		apiReq.Messages = append(apiReq.Messages, apiMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Convert tools
	for _, tool := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, apiTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	return json.Marshal(apiReq)
}

// processStream reads SSE events from the response body and sends StreamEvents
// to the channel. It closes both the body and the channel when done.
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	events, err := parseSSEEvents(body)
	if err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
		}
		return
	}

	for _, evt := range events {
		if ctx.Err() != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}

		streamEvt := p.convertSSEEvent(evt)
		if streamEvt == nil {
			continue
		}

		select {
		case ch <- *streamEvt:
		case <-ctx.Done():
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}
	}
}

// convertSSEEvent converts a raw SSE event into a StreamEvent.
func (p *Provider) convertSSEEvent(evt sseEvent) *provider.StreamEvent {
	switch evt.Event {
	case "content_block_start":
		return p.handleContentBlockStart(evt.Data)
	case "content_block_delta":
		return p.handleContentBlockDelta(evt.Data)
	case "message_stop":
		return &provider.StreamEvent{Type: "stop"}
	default:
		return nil
	}
}

func (p *Provider) handleContentBlockStart(data string) *provider.StreamEvent {
	var parsed struct {
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
	}

	if err := json.NewDecoder(strings.NewReader(data)).Decode(&parsed); err != nil {
		return &provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing content_block_start: %w", err)}
	}

	if parsed.ContentBlock.Type == "tool_use" {
		return &provider.StreamEvent{
			Type: "tool_use",
			ToolUse: &provider.ToolUseBlock{
				ID:   parsed.ContentBlock.ID,
				Name: parsed.ContentBlock.Name,
			},
		}
	}

	return nil
}

func (p *Provider) handleContentBlockDelta(data string) *provider.StreamEvent {
	var parsed struct {
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}

	if err := json.NewDecoder(strings.NewReader(data)).Decode(&parsed); err != nil {
		return &provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing content_block_delta: %w", err)}
	}

	switch parsed.Delta.Type {
	case "text_delta":
		return &provider.StreamEvent{
			Type: "text_delta",
			Text: parsed.Delta.Text,
		}
	case "input_json_delta":
		// Emit JSON input deltas as text_delta - the agent layer accumulates the JSON
		return &provider.StreamEvent{
			Type: "text_delta",
			Text: parsed.Delta.PartialJSON,
		}
	default:
		return nil
	}
}
