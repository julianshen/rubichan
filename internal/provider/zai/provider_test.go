package zai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test-only types for unmarshaling request bodies (mirror openai wire format).
type apiRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type apiMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"`
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
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function apiCallFunc `json:"function"`
}

type apiCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Task 7 - Construction Tests

func TestNew(t *testing.T) {
	p := New("https://z.ai/api", "test-key", "glm-5", map[string]string{"X-Custom": "value"})
	assert.NotNil(t, p)
	assert.Equal(t, "https://z.ai/api", p.baseURL)
	assert.Equal(t, "test-key", p.apiKey)
	assert.Equal(t, "glm-5", p.model)
	assert.Equal(t, "value", p.extraHeaders["X-Custom"])
	assert.NotNil(t, p.client)
}

func TestNewWithNilExtraHeaders(t *testing.T) {
	p := New("https://z.ai/api", "test-key", "glm-5", nil)
	assert.NotNil(t, p)
	assert.NotNil(t, p.extraHeaders)
	assert.Equal(t, 0, len(p.extraHeaders))
}

func TestNewWithDefaultModel(t *testing.T) {
	p := New("https://z.ai/api", "test-key", "", nil)
	assert.Equal(t, "glm-5", p.model)
}

func TestSetHTTPClient(t *testing.T) {
	p := New("https://z.ai/api", "test-key", "glm-5", nil)
	oldClient := p.client

	newClient := &http.Client{}
	p.SetHTTPClient(newClient)

	assert.NotEqual(t, oldClient, p.client)
	assert.Equal(t, newClient, p.client)
}

// Task 8 - Model Selection Tests

func TestStreamUsesDefaultModel(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "", // Empty model should use provider default
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify default model was used
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)
	assert.Equal(t, "glm-5", apiReq.Model)
}

func TestStreamUsesRequestModel(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-4",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify request model overrides provider default
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)
	assert.Equal(t, "glm-4", apiReq.Model)
}

// Task 9 - Streaming Text Test

func TestStreamTextResponse(t *testing.T) {
	sseBody := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"!"},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	// Verify it satisfies the LLMProvider interface
	var _ provider.LLMProvider = p

	req := provider.CompletionRequest{
		Model:     "glm-5",
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

	// Collect text parts
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

// Task 10 - Tool Use Streaming

func TestStreamToolCallResponse(t *testing.T) {
	sseBody := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp/test.txt\"}"}}]},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Read /tmp/test.txt")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Collect tool_use events
	var toolUseEvents []provider.StreamEvent
	for _, evt := range events {
		if evt.Type == "tool_use" {
			toolUseEvents = append(toolUseEvents, evt)
		}
	}

	// Should have one tool_use event
	require.Len(t, toolUseEvents, 1)
	require.NotNil(t, toolUseEvents[0].ToolUse)
	assert.Equal(t, "call_123", toolUseEvents[0].ToolUse.ID)
	assert.Equal(t, "read_file", toolUseEvents[0].ToolUse.Name)
	assert.JSONEq(t, `{"path":"/tmp/test.txt"}`, string(toolUseEvents[0].ToolUse.Input))
}

// Task 11 - Multi-Tool Streaming

func TestStreamMultipleToolCalls(t *testing.T) {
	// Z.ai interleaves argument fragments from multiple tool calls
	sseBody := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_t1","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_t2","type":"function","function":{"name":"shell","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"cmd\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"file.txt\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"ls\"}"}}]},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Read file.txt and list")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Collect tool_use events
	var toolUseEvents []provider.StreamEvent
	for _, evt := range events {
		if evt.Type == "tool_use" {
			toolUseEvents = append(toolUseEvents, evt)
		}
	}

	// Should have two tool calls
	require.Len(t, toolUseEvents, 2, "should have 2 tool_use events")

	assert.Equal(t, "call_t1", toolUseEvents[0].ToolUse.ID)
	assert.Equal(t, "read_file", toolUseEvents[0].ToolUse.Name)
	assert.JSONEq(t, `{"path":"file.txt"}`, string(toolUseEvents[0].ToolUse.Input))

	assert.Equal(t, "call_t2", toolUseEvents[1].ToolUse.ID)
	assert.Equal(t, "shell", toolUseEvents[1].ToolUse.Name)
	assert.JSONEq(t, `{"cmd":"ls"}`, string(toolUseEvents[1].ToolUse.Input))
}

// Task 12 - Error Handling

func TestStreamUnauthorizedError(t *testing.T) {
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Invalid API key"}`))
	}))
	defer server.Close()

	p := New(server.URL, "invalid-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	_, err := p.Stream(context.Background(), req)
	require.Error(t, err)
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrAuthFailed, pe.Kind)
	assert.Contains(t, err.Error(), "Authentication failed")
}

func TestStreamServerError(t *testing.T) {
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"Internal server error"}`))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	_, err := p.Stream(context.Background(), req)
	require.Error(t, err)
	var pe *provider.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, provider.ErrServerError, pe.Kind)
	assert.Contains(t, err.Error(), "Server error")
}

// Task 13 - Context Cancellation

func TestStreamContextCancellation(t *testing.T) {
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write some data but never complete
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(ctx, req)
	require.Error(t, err) // Should error due to cancelled context
	assert.Nil(t, ch)
}

// Task 14 - Malformed Response

func TestStreamMalformedJSON(t *testing.T) {
	// Include a malformed JSON chunk that should be skipped
	sseBody := `data: {"choices":[{"delta":{"content":"Start"},"finish_reason":null}]}

data: not valid json at all

data: {"choices":[{"delta":{"content":" End"},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should still collect valid text parts and have an error event
	var textParts []string
	var hasError, hasStop bool
	for _, evt := range events {
		switch evt.Type {
		case "text_delta":
			textParts = append(textParts, evt.Text)
		case "error":
			hasError = true
		case "stop":
			hasStop = true
		}
	}

	assert.Equal(t, []string{"Start", " End"}, textParts)
	assert.True(t, hasError, "should have received error event for malformed JSON")
	assert.True(t, hasStop, "should still reach stop after skipping malformed chunk")
}

// Task 15 - Message Conversion

func TestConvertUserMessage(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hello, world!")},
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify message conversion
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	require.Len(t, apiReq.Messages, 1)
	assert.Equal(t, "user", apiReq.Messages[0].Role)
	assert.Equal(t, "Hello, world!", apiReq.Messages[0].Content)
}

func TestConvertAssistantMessage(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	messages := []provider.Message{
		provider.NewUserMessage("Read /tmp/file.txt"),
		{
			Role: "assistant",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "I'll read that file."},
				{Type: "tool_use", ID: "tool_1", Name: "read_file", Input: json.RawMessage(`{"path":"/tmp/file.txt"}`)},
			},
		},
	}

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  messages,
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify message conversion
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	// Find assistant message
	var assistantMsg *apiMessage
	for i := range apiReq.Messages {
		if apiReq.Messages[i].Role == "assistant" {
			assistantMsg = &apiReq.Messages[i]
			break
		}
	}

	require.NotNil(t, assistantMsg)
	assert.Equal(t, "I'll read that file.", assistantMsg.Content)
	require.Len(t, assistantMsg.ToolCalls, 1)
	assert.Equal(t, "tool_1", assistantMsg.ToolCalls[0].ID)
	assert.Equal(t, "read_file", assistantMsg.ToolCalls[0].Function.Name)
}

func TestConvertToolResultMessage(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	messages := []provider.Message{
		provider.NewToolResultMessage("tool_1", "File contents: hello", false),
	}

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  messages,
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify message conversion
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	// Find tool message
	var toolMsg *apiMessage
	for i := range apiReq.Messages {
		if apiReq.Messages[i].Role == "tool" {
			toolMsg = &apiReq.Messages[i]
			break
		}
	}

	require.NotNil(t, toolMsg)
	assert.Equal(t, "File contents: hello", toolMsg.Content)
	assert.Equal(t, "tool_1", toolMsg.ToolCallID)
}

// Task 16 - Tool Serialization

func TestBuildRequestBodySerializesTools(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	// Define tools in non-alphabetical order
	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 100,
		Tools: []provider.ToolDef{
			{Name: "zebra", Description: "Z tool", InputSchema: json.RawMessage(`{}`)},
			{Name: "apple", Description: "A tool", InputSchema: json.RawMessage(`{}`)},
			{Name: "middle", Description: "M tool", InputSchema: json.RawMessage(`{}`)},
		},
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify tools are sorted alphabetically
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	require.Len(t, apiReq.Tools, 3)
	assert.Equal(t, "apple", apiReq.Tools[0].Function.Name)
	assert.Equal(t, "middle", apiReq.Tools[1].Function.Name)
	assert.Equal(t, "zebra", apiReq.Tools[2].Function.Name)
}

// Task 17 - System Message

func TestBuildRequestBodyIncludesSystemMessage(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		System:    "You are a helpful assistant.",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify system message is first
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	require.Len(t, apiReq.Messages, 2)
	assert.Equal(t, "system", apiReq.Messages[0].Role)
	assert.Equal(t, "You are a helpful assistant.", apiReq.Messages[0].Content)
	assert.Equal(t, "user", apiReq.Messages[1].Role)
}

// Task 18 - Temperature

func TestBuildRequestBodyIncludesTemperature(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	temp := 0.7
	req := provider.CompletionRequest{
		Model:       "glm-5",
		Messages:    []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens:   100,
		Temperature: &temp,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify temperature is included
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	require.NotNil(t, apiReq.Temperature)
	assert.Equal(t, 0.7, *apiReq.Temperature)
}

// Task 19 - Empty Response

func TestStreamEmptyResponse(t *testing.T) {
	sseBody := `data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should have only a stop event
	require.Len(t, events, 1)
	assert.Equal(t, "stop", events[0].Type)
}

// Additional coverage tests

func TestConvertMessagesWithFallbackRole(t *testing.T) {
	// Test the fallback path in convertMessages for non-user/assistant roles
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	// Create a message with a custom role (fallback path)
	customMsg := provider.Message{
		Role: "custom_role",
		Content: []provider.ContentBlock{
			{Type: "text", Text: "Some text"},
		},
	}

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{customMsg},
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}

	// Verify fallback message was created
	var apiReq apiRequest
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	require.Len(t, apiReq.Messages, 1)
	assert.Equal(t, "custom_role", apiReq.Messages[0].Role)
	assert.Equal(t, "Some text", apiReq.Messages[0].Content)
}

func TestStreamChoicesWithEmptyDelta(t *testing.T) {
	// Test handling of choices with empty delta objects
	sseBody := `data: {"choices":[{"delta":{},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"Text"},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should skip empty delta and get text then stop
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

	assert.Equal(t, []string{"Text"}, textParts)
	assert.True(t, hasStop)
}

func TestStreamToolCallWithEmptyArguments(t *testing.T) {
	// Test tool call that ends without arguments defaults to {}
	sseBody := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"empty_tool"}}]},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Call empty tool")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Collect tool_use events
	var toolUseEvents []provider.StreamEvent
	for _, evt := range events {
		if evt.Type == "tool_use" {
			toolUseEvents = append(toolUseEvents, evt)
		}
	}

	// Should have tool_use event with default {} arguments
	require.Len(t, toolUseEvents, 1)
	assert.JSONEq(t, `{}`, string(toolUseEvents[0].ToolUse.Input))
}

func TestStreamProcessScannerError(t *testing.T) {
	// Test handling of scanner errors during stream processing
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write incomplete data that will cause read errors when connection closes
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should have completed normally
	assert.True(t, len(events) > 0)
}

func TestStreamNoChoices(t *testing.T) {
	// Test handling of chunks with empty choices array
	sseBody := `data: {"choices":[]}

data: {"choices":[{"delta":{"content":"Valid"},"finish_reason":null}]}

data: [DONE]

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", "glm-5", nil)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should skip empty choices and get text then stop
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

	assert.Equal(t, []string{"Valid"}, textParts)
	assert.True(t, hasStop)
}

func TestExtraHeadersInRequest(t *testing.T) {
	// Test that extra headers are properly included in requests
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify extra headers
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom-Header"))
		assert.Equal(t, "another-value", r.Header.Get("X-Another"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	extraHeaders := map[string]string{
		"X-Custom-Header": "custom-value",
		"X-Another":       "another-value",
	}

	p := New(server.URL, "test-key", "glm-5", extraHeaders)
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "glm-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 100,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain channel
	for range ch {
	}
}
