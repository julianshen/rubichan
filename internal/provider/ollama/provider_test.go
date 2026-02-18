package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamTextResponse(t *testing.T) {
	ndjsonBody := `{"model":"llama3","message":{"role":"assistant","content":"Hello"},"done":false}
{"model":"llama3","message":{"role":"assistant","content":" world"},"done":false}
{"model":"llama3","message":{"role":"assistant","content":"!"},"done":false}
{"model":"llama3","message":{"role":"assistant","content":""},"done":true}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request basics
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/chat", r.URL.Path)

		// No auth header should be present
		assert.Empty(t, r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ndjsonBody))
	}))
	defer server.Close()

	p := New(server.URL)

	// Verify it satisfies the LLMProvider interface
	var _ provider.LLMProvider = p

	req := provider.CompletionRequest{
		Model:     "llama3",
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

	// Collect text parts and check for stop
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

func TestStreamAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	p := New(server.URL)

	req := provider.CompletionRequest{
		Model:     "nonexistent",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	_, err := p.Stream(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestProviderRegistration(t *testing.T) {
	p := New("http://localhost:11434")

	// Verify it implements LLMProvider
	var _ provider.LLMProvider = p

	assert.NotNil(t, p)
	assert.Equal(t, "http://localhost:11434", p.baseURL)
	assert.NotNil(t, p.client)
}

func TestStreamContextCancellation(t *testing.T) {
	var mu sync.Mutex
	serverReady := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}

		// Send one chunk
		fmt.Fprintf(w, `{"model":"llama3","message":{"role":"assistant","content":"Hello"},"done":false}`+"\n")
		flusher.Flush()

		// Signal ready
		mu.Lock()
		close(serverReady)
		mu.Unlock()

		// Hang until client disconnects
		<-r.Context().Done()
	}))
	defer server.Close()

	p := New(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	req := provider.CompletionRequest{
		Model:     "llama3",
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

func TestStreamToolCallResponse(t *testing.T) {
	ndjsonBody := `{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"/tmp/test.txt"}}}]},"done":false}
{"model":"llama3","message":{"role":"assistant","content":""},"done":true}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ndjsonBody))
	}))
	defer server.Close()

	p := New(server.URL)

	req := provider.CompletionRequest{
		Model:     "llama3",
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
	var toolUseEvents []provider.StreamEvent
	var hasStop bool
	for _, evt := range events {
		switch evt.Type {
		case "tool_use":
			toolUseEvents = append(toolUseEvents, evt)
		case "stop":
			hasStop = true
		}
	}

	// Should have a tool_use event with correct name
	require.Len(t, toolUseEvents, 1)
	require.NotNil(t, toolUseEvents[0].ToolUse)
	assert.Equal(t, "read_file", toolUseEvents[0].ToolUse.Name)
	assert.True(t, strings.HasPrefix(toolUseEvents[0].ToolUse.ID, "ollama_"), "tool call ID should have ollama_ prefix")

	// Verify the arguments contain the expected data
	var args map[string]string
	err = json.Unmarshal(toolUseEvents[0].ToolUse.Input, &args)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/test.txt", args["path"])

	assert.True(t, hasStop, "should have received stop event")
}

func TestBuildRequestBodyWithTools(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"model":"llama3","message":{"role":"assistant","content":""},"done":true}` + "\n"))
	}))
	defer server.Close()

	p := New(server.URL)

	req := provider.CompletionRequest{
		Model:     "llama3",
		System:    "You are a helpful assistant.",
		Messages:  []provider.Message{provider.NewUserMessage("Read a file")},
		MaxTokens: 1024,
		Tools: []provider.ToolDef{
			{
				Name:        "read_file",
				Description: "Reads a file from disk",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
			},
		},
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	// Drain the channel
	for range ch {
	}

	// Parse the captured request body
	var apiReq map[string]any
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	// Verify stream is true
	assert.Equal(t, true, apiReq["stream"])

	// Verify model
	assert.Equal(t, "llama3", apiReq["model"])

	// Verify options
	opts, ok := apiReq["options"].(map[string]any)
	require.True(t, ok, "options should be present")
	assert.Equal(t, float64(1024), opts["num_predict"])

	// Verify messages structure
	msgs, ok := apiReq["messages"].([]any)
	require.True(t, ok)
	// system + user = 2 messages
	require.Len(t, msgs, 2)

	// First should be system message
	systemMsg := msgs[0].(map[string]any)
	assert.Equal(t, "system", systemMsg["role"])
	assert.Equal(t, "You are a helpful assistant.", systemMsg["content"])

	// Second should be user message
	userMsg := msgs[1].(map[string]any)
	assert.Equal(t, "user", userMsg["role"])
	assert.Equal(t, "Read a file", userMsg["content"])

	// Verify tools structure
	tools, ok := apiReq["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool := tools[0].(map[string]any)
	assert.Equal(t, "function", tool["type"])

	fn := tool["function"].(map[string]any)
	assert.Equal(t, "read_file", fn["name"])
	assert.Equal(t, "Reads a file from disk", fn["description"])

	// Verify parameters contain the schema
	params := fn["parameters"].(map[string]any)
	assert.Equal(t, "object", params["type"])
}
