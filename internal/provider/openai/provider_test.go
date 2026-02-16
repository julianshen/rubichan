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
