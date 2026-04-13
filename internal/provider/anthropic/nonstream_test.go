package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonStream_TextResponse(t *testing.T) {
	responseJSON := `{
        "id": "msg_01",
        "type": "message",
        "role": "assistant",
        "model": "claude-sonnet-4-5",
        "content": [{"type": "text", "text": "Hello world!"}],
        "stop_reason": "end_turn",
        "usage": {"input_tokens": 10, "output_tokens": 5, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
    }`

	var capturedStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]interface{}
		_ = json.Unmarshal(body, &parsed)
		if s, ok := parsed["stream"].(bool); ok {
			capturedStream = s
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseJSON)
	}))
	defer srv.Close()

	p := New(srv.URL, "test-key")
	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 100,
		Messages:  []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}}},
	}
	events, err := p.NonStream(context.Background(), req)
	require.NoError(t, err)

	assert.False(t, capturedStream, "stream flag must be false")

	// Expect: message_start, text_delta, stop
	require.Len(t, events, 3)
	assert.Equal(t, "message_start", events[0].Type)
	assert.Equal(t, "msg_01", events[0].MessageID)
	assert.Equal(t, 10, events[0].InputTokens)
	assert.Equal(t, 5, events[0].OutputTokens)

	assert.Equal(t, "text_delta", events[1].Type)
	assert.Equal(t, "Hello world!", events[1].Text)

	assert.Equal(t, agentsdk.EventStop, events[2].Type)
	assert.Equal(t, "end_turn", events[2].StopReason)
}

func TestNonStream_ToolUseResponse(t *testing.T) {
	responseJSON := `{
        "id": "msg_02",
        "type": "message",
        "role": "assistant",
        "model": "claude-sonnet-4-5",
        "content": [
            {"type": "text", "text": "Let me check that."},
            {"type": "tool_use", "id": "tool_abc", "name": "read_file", "input": {"path": "/tmp/f"}}
        ],
        "stop_reason": "tool_use",
        "usage": {"input_tokens": 20, "output_tokens": 15}
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseJSON)
	}))
	defer srv.Close()

	p := New(srv.URL, "test-key")
	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 100,
		Messages:  []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}}},
	}
	events, err := p.NonStream(context.Background(), req)
	require.NoError(t, err)

	// Expect: message_start, text_delta, tool_use, content_block_stop, stop
	require.Len(t, events, 5)
	assert.Equal(t, "message_start", events[0].Type)
	assert.Equal(t, "text_delta", events[1].Type)
	assert.Equal(t, "Let me check that.", events[1].Text)
	assert.Equal(t, agentsdk.EventToolUse, events[2].Type)
	require.NotNil(t, events[2].ToolUse)
	assert.Equal(t, "tool_abc", events[2].ToolUse.ID)
	assert.Equal(t, "read_file", events[2].ToolUse.Name)
	assert.Equal(t, `{"path":"/tmp/f"}`, strings.ReplaceAll(string(events[2].ToolUse.Input), " ", ""))
	assert.Equal(t, agentsdk.EventContentBlockStop, events[3].Type)
	assert.Equal(t, agentsdk.EventStop, events[4].Type)
	assert.Equal(t, "tool_use", events[4].StopReason)
}

func TestNonStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "5")
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	p := New(srv.URL, "test-key")
	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 100,
		Messages:  []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}}},
	}
	_, err := p.NonStream(context.Background(), req)
	require.Error(t, err)

	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrRateLimited, pe.Kind)
	assert.NotEmpty(t, pe.RequestID, "RequestID must be populated on HTTP errors")
}

func TestParseNonStreamResponse_EmptyInputToolUse(t *testing.T) {
	data := []byte(`{
        "id": "msg_03",
        "model": "m",
        "stop_reason": "tool_use",
        "content": [{"type": "tool_use", "id": "t1", "name": "noop"}],
        "usage": {"input_tokens": 1, "output_tokens": 1}
    }`)
	events, err := parseNonStreamResponse(data)
	require.NoError(t, err)
	// message_start, tool_use, content_block_stop, stop
	require.Len(t, events, 4)
	require.NotNil(t, events[1].ToolUse)
	assert.JSONEq(t, `{}`, string(events[1].ToolUse.Input), "empty input must default to {}")
}
