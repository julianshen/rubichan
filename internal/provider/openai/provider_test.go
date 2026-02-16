package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamTextResponse(t *testing.T) {
	sseBody := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)

	// Verify it satisfies the LLMProvider interface
	var _ provider.LLMProvider = p

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		System:    "You are helpful.",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should have text_delta events and a stop event
	var textParts []string
	var hasStop bool
	for _, evt := range events {
		switch evt.Type {
		case "text_delta":
			textParts = append(textParts, evt.Text)
		case "stop":
			hasStop = true
		}
	}

	assert.Equal(t, []string{"Hello", " world", "!"}, textParts)
	assert.True(t, hasStop, "should have received stop event")
}

func TestStreamToolCallResponse(t *testing.T) {
	sseBody := `data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_abc123","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp/test.txt\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		Messages:  []provider.Message{provider.NewUserMessage("Read /tmp/test.txt")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Collect events by type
	var textParts []string
	var toolUseEvents []provider.StreamEvent
	var hasStop bool

	for _, evt := range events {
		switch evt.Type {
		case "text_delta":
			textParts = append(textParts, evt.Text)
		case "tool_use":
			toolUseEvents = append(toolUseEvents, evt)
		case "stop":
			hasStop = true
		}
	}

	// Should have tool_use event with correct ID and name
	require.Len(t, toolUseEvents, 1)
	require.NotNil(t, toolUseEvents[0].ToolUse)
	assert.Equal(t, "call_abc123", toolUseEvents[0].ToolUse.ID)
	assert.Equal(t, "read_file", toolUseEvents[0].ToolUse.Name)

	// Should have argument fragments as text_delta
	assert.Equal(t, []string{`{"path":`, `"/tmp/test.txt"}`}, textParts)

	assert.True(t, hasStop, "should have received stop event")
}

func TestExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify extra headers are sent
		assert.Equal(t, "https://myapp.com", r.Header.Get("HTTP-Referer"))
		assert.Equal(t, "My App", r.Header.Get("X-Title"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	extraHeaders := map[string]string{
		"HTTP-Referer": "https://myapp.com",
		"X-Title":      "My App",
	}

	p := New(server.URL, "test-api-key", extraHeaders)

	req := provider.CompletionRequest{
		Model:     "openrouter/auto",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain the channel
	for range ch {
	}
}
