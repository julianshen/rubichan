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

// pendingToolBlock tracks an in-flight tool_use content block. Anthropic
// emits tool_use as a content_block_start with empty Input, followed by
// input_json_delta fragments inside content_block_delta, then a
// content_block_stop. The provider accumulates fragments per block index
// and emits a single tool_use StreamEvent with the fully-joined Input
// at content_block_stop time — that way the agent loop sees tool_use
// events with complete Input and can dispatch immediately instead of
// waiting for a subsequent content block (which may never arrive for
// single-tool responses).
type pendingToolBlock struct {
	id   string
	name string
	args strings.Builder
}

// streamState carries per-request state across SSE events. It lives
// inside processStream's goroutine so there is no cross-request sharing.
type streamState struct {
	pendingTools map[int]*pendingToolBlock
}

func newStreamState() *streamState {
	return &streamState{pendingTools: map[int]*pendingToolBlock{}}
}

// processStream reads SSE events from the response body and sends StreamEvents
// to the channel as they arrive. It closes both the body and the channel when done.
func (p *Provider) processStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	state := newStreamState()

	scanner := newSSEScanner(body)
	for scanner.Next() {
		if ctx.Err() != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}

		for _, streamEvt := range p.convertSSEEvent(state, scanner.Event()) {
			select {
			case ch <- streamEvt:
			case <-ctx.Done():
				select {
				case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
				default:
				}
				return
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

// convertSSEEvent converts a raw SSE event into zero or more
// StreamEvents, using state to accumulate per-block data (notably
// tool_use Input fragments). Most events return a single result; the
// content_block_stop path may emit a tool_use followed by a
// content_block_stop marker so the agent loop can finalize immediately.
func (p *Provider) convertSSEEvent(state *streamState, evt sseEvent) []provider.StreamEvent {
	switch evt.Event {
	case "message_start":
		return singleEvent(p.handleMessageStart(evt.Data))
	case "content_block_start":
		return singleEvent(p.handleContentBlockStart(state, evt.Data))
	case "content_block_delta":
		return singleEvent(p.handleContentBlockDelta(state, evt.Data))
	case "content_block_stop":
		return p.handleContentBlockStop(state, evt.Data)
	case "message_stop":
		return []provider.StreamEvent{{Type: "stop"}}
	default:
		return nil
	}
}

func singleEvent(e *provider.StreamEvent) []provider.StreamEvent {
	if e == nil {
		return nil
	}
	return []provider.StreamEvent{*e}
}

func (p *Provider) handleMessageStart(data string) *provider.StreamEvent {
	var parsed struct {
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}

	if err := json.NewDecoder(strings.NewReader(data)).Decode(&parsed); err != nil {
		return &provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing message_start: %w", err)}
	}

	return &provider.StreamEvent{
		Type:         "message_start",
		Model:        parsed.Message.Model,
		MessageID:    parsed.Message.ID,
		InputTokens:  parsed.Message.Usage.InputTokens,
		OutputTokens: parsed.Message.Usage.OutputTokens,
	}
}

func (p *Provider) handleContentBlockStart(state *streamState, data string) *provider.StreamEvent {
	var parsed struct {
		Index        int `json:"index"`
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
		// Start accumulating fragments for this block; emit nothing yet.
		// The tool_use StreamEvent is emitted at content_block_stop with
		// the joined Input so the agent loop can dispatch immediately
		// with complete arguments.
		state.pendingTools[parsed.Index] = &pendingToolBlock{
			id:   parsed.ContentBlock.ID,
			name: parsed.ContentBlock.Name,
		}
		return nil
	case "thinking":
		// Thinking content arrives via content_block_delta events; no event needed at start.
		return nil
	default:
		return nil
	}
}

func (p *Provider) handleContentBlockDelta(state *streamState, data string) *provider.StreamEvent {
	var parsed struct {
		Index int `json:"index"`
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
		// Accumulate into the pending tool block rather than emit a
		// separate event; the agent loop doesn't track input_json_delta.
		if pending, ok := state.pendingTools[parsed.Index]; ok {
			pending.args.WriteString(parsed.Delta.PartialJSON)
			return nil
		}
		// No pending block — fall back to emitting the fragment so
		// upstream observers can still see it for debugging.
		return &provider.StreamEvent{
			Type: "input_json_delta",
			Text: parsed.Delta.PartialJSON,
		}
	default:
		return nil
	}
}

func (p *Provider) handleContentBlockStop(state *streamState, data string) []provider.StreamEvent {
	var parsed struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(strings.NewReader(data)).Decode(&parsed); err != nil {
		return []provider.StreamEvent{{Type: "error", Error: fmt.Errorf("parsing content_block_stop: %w", err)}}
	}

	pending, ok := state.pendingTools[parsed.Index]
	if !ok {
		// Not a tool_use block — still emit content_block_stop so the
		// agent loop can finalize any text_delta-accumulated tool that
		// was started earlier via a legacy pathway.
		return []provider.StreamEvent{{Type: "content_block_stop"}}
	}
	delete(state.pendingTools, parsed.Index)

	args := pending.args.String()
	if args == "" {
		// Anthropic emits an empty-args tool_use for no-argument tools;
		// the JSON must still be valid so downstream handlers can parse it.
		args = "{}"
	}
	// Emit the tool_use first (so currentTool is populated with full
	// Input), then a content_block_stop marker so the agent loop
	// finalizes (and streaming-dispatches) immediately.
	return []provider.StreamEvent{
		{
			Type: "tool_use",
			ToolUse: &provider.ToolUseBlock{
				ID:    pending.id,
				Name:  pending.name,
				Input: json.RawMessage(args),
			},
		},
		{Type: "content_block_stop"},
	}
}
