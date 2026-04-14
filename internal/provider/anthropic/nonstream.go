package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// NonStream sends req with stream=false and converts the full JSON response
// into a slice of StreamEvents equivalent to what streaming would have produced.
// Intended as a fallback for proxy environments that corrupt SSE streams
// (HTTP 200 with non-SSE body, or mid-stream truncation).
func (p *Provider) NonStream(ctx context.Context, req provider.CompletionRequest) ([]provider.StreamEvent, error) {
	body, err := p.transformer.ToProviderJSON(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	// Inject stream:false. The transformer sets stream:true by default.
	body, err = overrideStreamFlag(body, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	requestID := uuid.New().String()
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-client-request-id", requestID)

	provider.LogRequest(p.debugLogger, httpReq, body)

	resp, err := provider.DoWithRetry(ctx, p.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	provider.LogResponse(p.debugLogger, resp.StatusCode, resp.Header, respBody)

	if resp.StatusCode != http.StatusOK {
		classified := provider.ClassifyAPIErrorWithResponse(resp.StatusCode, respBody, httpReq, "anthropic", resp.Header)
		if classified != nil {
			classified.RequestID = requestID
		}
		return nil, classified
	}

	events, err := parseNonStreamResponse(respBody)
	if err != nil {
		return nil, err
	}
	return events, nil
}

// overrideStreamFlag rewrites the stream field in a request body JSON object.
// The transformer emits stream:true by default; this flips it for NonStream.
func overrideStreamFlag(body []byte, stream bool) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("patching stream flag: %w", err)
	}
	if stream {
		raw["stream"] = json.RawMessage(`true`)
	} else {
		raw["stream"] = json.RawMessage(`false`)
	}
	return json.Marshal(raw)
}

// parseNonStreamResponse converts a full Anthropic message JSON response
// into a StreamEvent slice mirroring what processStream would have emitted.
// Event order: message_start, then one text_delta per text block OR
// tool_use+content_block_stop per tool block, then a final stop event.
func parseNonStreamResponse(data []byte) ([]provider.StreamEvent, error) {
	var msg struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("parsing non-stream response: %w", err)
	}

	events := make([]provider.StreamEvent, 0, 2+2*len(msg.Content))
	events = append(events, provider.StreamEvent{
		Type:                "message_start",
		MessageID:           msg.ID,
		Model:               msg.Model,
		InputTokens:         msg.Usage.InputTokens,
		OutputTokens:        msg.Usage.OutputTokens,
		CacheCreationTokens: msg.Usage.CacheCreationInputTokens,
		CacheReadTokens:     msg.Usage.CacheReadInputTokens,
	})

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				events = append(events, provider.StreamEvent{Type: "text_delta", Text: block.Text})
			}
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			events = append(events, provider.StreamEvent{
				Type: agentsdk.EventToolUse,
				ToolUse: &provider.ToolUseBlock{
					ID:    block.ID,
					Name:  block.Name,
					Input: input,
				},
			})
			events = append(events, provider.StreamEvent{Type: agentsdk.EventContentBlockStop})
		}
	}

	events = append(events, provider.StreamEvent{
		Type:       agentsdk.EventStop,
		StopReason: msg.StopReason,
	})
	return events, nil
}
