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
	baseURL     string
	apiKey      string
	client      *http.Client
	transformer Transformer
	debugLogger provider.DebugLogger
}

// SetDebugLogger enables debug logging for API requests and responses.
func (p *Provider) SetDebugLogger(logger provider.DebugLogger) {
	p.debugLogger = logger
}

// New creates a new Anthropic provider.
func New(baseURL, apiKey string) *Provider {
	return &Provider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  provider.NewHTTPClient(),
	}
}

// SetHTTPClient replaces the default HTTP client. This is intended for
// testing with custom transports (e.g. in-memory mem:// servers).
func (p *Provider) SetHTTPClient(c *http.Client) {
	p.client = c
}

// Stream sends a completion request to the Anthropic API and returns a channel
// of StreamEvents parsed from the SSE response.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.transformer.ToProviderJSON(req)
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

	provider.LogRequest(p.debugLogger, httpReq, body)

	resp, err := provider.DoWithRetry(ctx, p.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		provider.LogResponse(p.debugLogger, resp.StatusCode, resp.Header, respBody)
		return nil, provider.ClassifyAPIErrorWithResponse(resp.StatusCode, respBody, httpReq, "anthropic", resp.Header)
	}

	if p.debugLogger != nil {
		p.debugLogger("[DEBUG] <<< HTTP Response: %d %s (streaming)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	ch := make(chan provider.StreamEvent)
	go p.processStream(ctx, resp.Body, ch)

	return ch, nil
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

	switch parsed.ContentBlock.Type {
	case "tool_use":
		return &provider.StreamEvent{
			Type: "tool_use",
			ToolUse: &provider.ToolUseBlock{
				ID:   parsed.ContentBlock.ID,
				Name: parsed.ContentBlock.Name,
			},
		}
	case "thinking":
		// Thinking content arrives via content_block_delta events; no event needed at start.
		return nil
	default:
		return nil
	}
}

func (p *Provider) handleContentBlockDelta(data string) *provider.StreamEvent {
	var parsed struct {
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
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
	case "thinking_delta":
		return &provider.StreamEvent{
			Type: "thinking_delta",
			Text: parsed.Delta.Thinking,
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
