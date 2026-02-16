package anthropic

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
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	// Verify it satisfies the LLMProvider interface
	var _ provider.LLMProvider = p

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
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

func TestStreamToolUseResponse(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":20,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me read that file."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_abc123","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/tmp/test.txt\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":30}}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
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

	// Should have the text part
	assert.Equal(t, []string{"Let me read that file.", `{"path":`, `"/tmp/test.txt"}`}, textParts)

	// Should have tool_use event with correct ID and name
	require.Len(t, toolUseEvents, 1)
	require.NotNil(t, toolUseEvents[0].ToolUse)
	assert.Equal(t, "toolu_abc123", toolUseEvents[0].ToolUse.ID)
	assert.Equal(t, "read_file", toolUseEvents[0].ToolUse.Name)

	assert.True(t, hasStop, "should have received stop event")
}

func TestStreamAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	_, err := p.Stream(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestStreamContextCancellation(t *testing.T) {
	var mu sync.Mutex
	serverReady := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Write partial response
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}

		fmt.Fprintf(w, "event: message_start\n")
		fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_3\",\"type\":\"message\",\"role\":\"assistant\"}}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "event: content_block_start\n")
		fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "event: content_block_delta\n")
		fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		flusher.Flush()

		// Signal that we've sent partial data
		mu.Lock()
		close(serverReady)
		mu.Unlock()

		// Hang here - context cancellation should unblock the client
		<-r.Context().Done()
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(ctx, req)
	require.NoError(t, err)

	// Wait for server to send partial data
	<-serverReady

	// Give a brief moment for events to be processed
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Drain the channel - it should close eventually
	var gotError bool
	timeout := time.After(5 * time.Second)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				// Channel closed
				goto done
			}
			if evt.Type == "error" && evt.Error != nil {
				gotError = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for channel to close")
		}
	}
done:
	// The channel should have been closed. We may or may not see an error event
	// depending on timing, but the channel must close.
	_ = gotError
}

func TestStreamMalformedContentBlockStart(t *testing.T) {
	sseBody := `event: content_block_start
data: {invalid json}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var hasError bool
	for evt := range ch {
		if evt.Type == "error" {
			hasError = true
		}
	}

	assert.True(t, hasError, "should have received error event for malformed JSON")
}

func TestStreamMalformedContentBlockDelta(t *testing.T) {
	sseBody := `event: content_block_delta
data: {invalid json}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var hasError bool
	for evt := range ch {
		if evt.Type == "error" {
			hasError = true
		}
	}

	assert.True(t, hasError, "should have received error event for malformed delta JSON")
}

func TestStreamUnknownDeltaType(t *testing.T) {
	sseBody := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"unknown_delta","text":"test"}}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should only have the stop event, unknown delta type returns nil
	require.Len(t, events, 1)
	assert.Equal(t, "stop", events[0].Type)
}

func TestStreamContentBlockStartTextType(t *testing.T) {
	// content_block_start with "text" type should return nil (no event emitted)
	sseBody := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Only stop event should be emitted; text content_block_start returns nil
	require.Len(t, events, 1)
	assert.Equal(t, "stop", events[0].Type)
}

func TestStreamContextCancelledDuringEventIteration(t *testing.T) {
	// Build a large SSE body with many events so there's time to cancel
	// during iteration. Anthropic parseSSEEvents reads the full body first,
	// then iterates events, so the body must be complete.
	var sseBuilder strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sseBuilder, "event: content_block_delta\n")
		fmt.Fprintf(&sseBuilder, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"chunk%d\"}}\n\n", i)
	}
	fmt.Fprintf(&sseBuilder, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	sseBody := sseBuilder.String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	ctx, cancel := context.WithCancel(context.Background())

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(ctx, req)
	require.NoError(t, err)

	// Read first event then cancel
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first event")
	}

	cancel()

	// Drain remaining events - channel should close
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

func TestStreamRequestBody(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")

	req := provider.CompletionRequest{
		Model:       "claude-sonnet-4-5",
		System:      "You are helpful.",
		Messages:    []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens:   2048,
		Temperature: 0.7,
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

	for range ch {
	}

	// Parse the captured request body
	var apiReq map[string]interface{}
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	assert.Equal(t, true, apiReq["stream"])
	assert.Equal(t, "claude-sonnet-4-5", apiReq["model"])
	assert.Equal(t, "You are helpful.", apiReq["system"])
	assert.Equal(t, float64(2048), apiReq["max_tokens"])
	assert.Equal(t, 0.7, apiReq["temperature"])

	// Verify tools are included
	tools, ok := apiReq["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool := tools[0].(map[string]interface{})
	assert.Equal(t, "read_file", tool["name"])
}
