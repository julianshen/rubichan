package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

func TestStreamAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	_, err := p.Stream(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestMessageConversion(t *testing.T) {
	// Test that assistant messages with tool_use and tool_result messages are
	// properly converted in the request body.
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)

	// Build a conversation with tool use
	messages := []provider.Message{
		provider.NewUserMessage("Read /tmp/test.txt"),
		{
			Role: "assistant",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "Let me read that file."},
				{
					Type:  "tool_use",
					ID:    "call_1",
					Name:  "read_file",
					Input: json.RawMessage(`{"path":"/tmp/test.txt"}`),
				},
			},
		},
		provider.NewToolResultMessage("call_1", "file contents here", false),
	}

	req := provider.CompletionRequest{
		Model:       "gpt-4",
		System:      "You are helpful.",
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.5,
		Tools: []provider.ToolDef{
			{
				Name:        "read_file",
				Description: "Reads a file",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		},
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain the channel
	for range ch {
	}

	// Parse the captured request body
	var apiReq map[string]interface{}
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	// Verify stream is true
	assert.Equal(t, true, apiReq["stream"])

	// Verify model
	assert.Equal(t, "gpt-4", apiReq["model"])

	// Verify temperature
	assert.Equal(t, 0.5, apiReq["temperature"])

	// Verify messages structure
	msgs, ok := apiReq["messages"].([]interface{})
	require.True(t, ok)
	// system + user + assistant + tool = 4 messages
	require.Len(t, msgs, 4)

	// First should be system message
	systemMsg := msgs[0].(map[string]interface{})
	assert.Equal(t, "system", systemMsg["role"])
	assert.Equal(t, "You are helpful.", systemMsg["content"])

	// Second should be user message
	userMsg := msgs[1].(map[string]interface{})
	assert.Equal(t, "user", userMsg["role"])
	assert.Equal(t, "Read /tmp/test.txt", userMsg["content"])

	// Third should be assistant message with tool_calls
	assistantMsg := msgs[2].(map[string]interface{})
	assert.Equal(t, "assistant", assistantMsg["role"])
	toolCalls, ok := assistantMsg["tool_calls"].([]interface{})
	require.True(t, ok)
	require.Len(t, toolCalls, 1)

	tc := toolCalls[0].(map[string]interface{})
	assert.Equal(t, "call_1", tc["id"])

	// Fourth should be tool result message
	toolMsg := msgs[3].(map[string]interface{})
	assert.Equal(t, "tool", toolMsg["role"])
	assert.Equal(t, "call_1", toolMsg["tool_call_id"])
	assert.Equal(t, "file contents here", toolMsg["content"])

	// Verify tools structure
	tools, ok := apiReq["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool := tools[0].(map[string]interface{})
	assert.Equal(t, "function", tool["type"])
	fn := tool["function"].(map[string]interface{})
	assert.Equal(t, "read_file", fn["name"])
	assert.Equal(t, "Reads a file", fn["description"])
}

func TestStreamContextCancellation(t *testing.T) {
	var mu sync.Mutex
	serverReady := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}

		// Send partial data
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Signal ready
		mu.Lock()
		close(serverReady)
		mu.Unlock()

		// Hang until client disconnects
		<-r.Context().Done()
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)
	ctx, cancel := context.WithCancel(context.Background())

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(ctx, req)
	require.NoError(t, err)

	// Wait for server to send partial data
	<-serverReady
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Drain the channel - should close
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				goto done
			}
		case <-timeout:
			t.Fatal("timed out waiting for channel to close")
		}
	}
done:
}

func TestStreamMalformedChunk(t *testing.T) {
	sseBody := "data: {invalid json}\n\ndata: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var hasError bool
	var hasStop bool
	for evt := range ch {
		if evt.Type == "error" {
			hasError = true
		}
		if evt.Type == "stop" {
			hasStop = true
		}
	}

	assert.True(t, hasError, "should have received error event for malformed JSON")
	assert.True(t, hasStop, "should have received stop event after error")
}

func TestStreamEmptyChoices(t *testing.T) {
	sseBody := `data: {"id":"chatcmpl-1","choices":[]}

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
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should only have the stop event; empty choices are skipped
	require.Len(t, events, 1)
	assert.Equal(t, "stop", events[0].Type)
}

func TestConvertMessageDefaultRole(t *testing.T) {
	// Test the default case in convertMessage for a non-standard role
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)

	// Use a non-standard role to hit the default case in convertMessage
	messages := []provider.Message{
		{
			Role: "developer",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "First part."},
				{Type: "text", Text: " Second part."},
			},
		},
	}

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		Messages:  messages,
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	for range ch {
	}

	var apiReq map[string]interface{}
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	msgs, ok := apiReq["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, msgs, 1)

	msg := msgs[0].(map[string]interface{})
	assert.Equal(t, "developer", msg["role"])
	assert.Equal(t, "First part. Second part.", msg["content"])
}

func TestStreamContextCancelledDuringProcessing(t *testing.T) {
	requestReceived := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}

		// Write many events
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chunk%d\"},\"finish_reason\":null}]}\n\n", i)
			flusher.Flush()
		}

		close(requestReceived)

		// Keep connection open
		<-r.Context().Done()
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key", nil)
	ctx, cancel := context.WithCancel(context.Background())

	req := provider.CompletionRequest{
		Model:     "gpt-4",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(ctx, req)
	require.NoError(t, err)

	// Wait for events to be written then cancel
	<-requestReceived
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first event")
	}

	cancel()

	// Drain the channel - should close
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				goto done
			}
		case <-timeout:
			t.Fatal("timed out waiting for channel to close")
		}
	}
done:
}
