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

func init() {
	provider.RegisterProvider("anthropic", func(baseURL, apiKey string, _ map[string]string) provider.LLMProvider {
		return New(baseURL, apiKey)
	})
}

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
	System      any          `json:"system,omitempty"` // string or []apiSystemBlock
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
}

// apiSystemBlock represents a structured system prompt block with optional cache control.
type apiSystemBlock struct {
	Type         string           `json:"type"`
	Text         string           `json:"text"`
	CacheControl *apiCacheControl `json:"cache_control,omitempty"`
}

// apiCacheControl marks a block for prompt caching.
type apiCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type apiMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type apiTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// apiContentBlock is an Anthropic-specific content block for serialization.
// The Anthropic API uses "content" (not "text") for tool_result blocks.
type apiContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
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
	}

	// Build system prompt with optional cache breakpoints.
	if len(req.CacheBreakpoints) > 0 && req.System != "" {
		apiReq.System = buildCachedSystemBlocks(req.System, req.CacheBreakpoints)
	} else {
		apiReq.System = req.System
	}

	if req.Temperature != nil {
		temp := *req.Temperature
		apiReq.Temperature = &temp
	}

	// Convert messages, remapping fields for the Anthropic API
	for _, msg := range req.Messages {
		apiReq.Messages = append(apiReq.Messages, apiMessage{
			Role:    msg.Role,
			Content: convertContentBlocks(msg.Content),
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

// buildCachedSystemBlocks splits the system prompt at breakpoint byte offsets
// and marks pre-breakpoint blocks with cache_control.
func buildCachedSystemBlocks(system string, breakpoints []int) []apiSystemBlock {
	var blocks []apiSystemBlock
	prev := 0
	for _, bp := range breakpoints {
		if bp > len(system) {
			bp = len(system)
		}
		if bp <= prev {
			continue
		}
		blocks = append(blocks, apiSystemBlock{
			Type:         "text",
			Text:         system[prev:bp],
			CacheControl: &apiCacheControl{Type: "ephemeral"},
		})
		prev = bp
	}
	if prev < len(system) {
		blocks = append(blocks, apiSystemBlock{
			Type: "text",
			Text: system[prev:],
		})
	}
	return blocks
}

// convertContentBlocks maps provider.ContentBlock to Anthropic-specific
// apiContentBlock. For tool_result blocks, the text is placed in the "content"
// field (which is what the Anthropic API expects) instead of "text".
func convertContentBlocks(blocks []provider.ContentBlock) []apiContentBlock {
	out := make([]apiContentBlock, len(blocks))
	for i, b := range blocks {
		out[i] = apiContentBlock{
			Type:      b.Type,
			ID:        b.ID,
			Name:      b.Name,
			Input:     b.Input,
			ToolUseID: b.ToolUseID,
			IsError:   b.IsError,
		}
		if b.Type == "tool_result" {
			out[i].Content = b.Text
		} else {
			out[i].Text = b.Text
		}
	}
	return out
}

// processStream reads SSE events from the response body and sends StreamEvents
// to the channel as they arrive. It closes both the body and the channel when done.
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	scanner := newSSEScanner(body)
	for scanner.Next() {
		if ctx.Err() != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}

		streamEvt := p.convertSSEEvent(scanner.Event())
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

	if err := scanner.Err(); err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
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
