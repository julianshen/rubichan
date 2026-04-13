package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/pkg/agentsdk"
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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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
	var jsonDeltaParts []string
	var toolUseEvents []provider.StreamEvent
	var hasStop bool
	var messageStartEvt *provider.StreamEvent

	for _, evt := range events {
		switch evt.Type {
		case "text_delta":
			textParts = append(textParts, evt.Text)
		case "input_json_delta":
			jsonDeltaParts = append(jsonDeltaParts, evt.Text)
		case "tool_use":
			toolUseEvents = append(toolUseEvents, evt)
		case "message_start":
			e := evt
			messageStartEvt = &e
		case "stop":
			hasStop = true
		}
	}

	// Text part should only contain visible text, not tool JSON.
	assert.Equal(t, []string{"Let me read that file."}, textParts)

	// Tool JSON fragments are now accumulated internally by the provider
	// and joined into the tool_use event's Input at content_block_stop
	// time — they must NOT be emitted as separate input_json_delta events
	// because the agent loop doesn't track them and would lose the input.
	assert.Empty(t, jsonDeltaParts, "input_json_delta fragments must be absorbed into the tool_use event")

	// message_start should have been emitted with model and ID.
	require.NotNil(t, messageStartEvt, "should have received message_start event")
	assert.Equal(t, "claude-sonnet-4-5", messageStartEvt.Model)
	assert.Equal(t, "msg_2", messageStartEvt.MessageID)

	// Should have tool_use event with correct ID, name, and fully-accumulated Input.
	// The Anthropic wire format emits input_json_delta fragments BETWEEN the
	// content_block_start and content_block_stop for a tool_use block; the
	// provider must accumulate those fragments and emit the tool_use event
	// with complete Input at content_block_stop time. jsonDeltaParts is also
	// asserted above as the raw delta sequence — this check proves the
	// provider-side accumulation joined them correctly.
	require.Len(t, toolUseEvents, 1)
	require.NotNil(t, toolUseEvents[0].ToolUse)
	assert.Equal(t, "toolu_abc123", toolUseEvents[0].ToolUse.ID)
	assert.Equal(t, "read_file", toolUseEvents[0].ToolUse.Name)
	assert.JSONEq(t, `{"path":"/tmp/test.txt"}`, string(toolUseEvents[0].ToolUse.Input),
		"tool_use Input must be the accumulated input_json_delta fragments as valid JSON")

	assert.True(t, hasStop, "should have received stop event")
}

func TestStreamAPIError(t *testing.T) {
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	_, err := p.Stream(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rate limited")
	assert.Contains(t, err.Error(), "Rate limit exceeded")
}

func TestStreamContextCancellation(t *testing.T) {
	var mu sync.Mutex
	serverReady := make(chan struct{})

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	p.SetHTTPClient(&http.Client{})

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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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

// TestStreamMultiToolUseResponse verifies per-block isolation in the
// pendingToolBlock accumulator. Two tool_use blocks at indices 1 and 2
// each have their own input_json_delta fragments; the provider must
// join each set independently and emit two tool_use StreamEvents with
// the correct Input values — not cross-contaminated.
func TestStreamMultiToolUseResponse(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_multi","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","usage":{"input_tokens":12,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I'll read both."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/a\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_b","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/b\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Read /a and /b")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var toolUseEvents []provider.StreamEvent
	var blockStopCount int
	for evt := range ch {
		switch evt.Type {
		case "tool_use":
			toolUseEvents = append(toolUseEvents, evt)
		case "content_block_stop":
			blockStopCount++
		}
	}

	require.Len(t, toolUseEvents, 2, "both tool_use blocks must be emitted")
	require.NotNil(t, toolUseEvents[0].ToolUse)
	require.NotNil(t, toolUseEvents[1].ToolUse)
	assert.Equal(t, "toolu_a", toolUseEvents[0].ToolUse.ID)
	assert.JSONEq(t, `{"path":"/a"}`, string(toolUseEvents[0].ToolUse.Input))
	assert.Equal(t, "toolu_b", toolUseEvents[1].ToolUse.ID)
	assert.JSONEq(t, `{"path":"/b"}`, string(toolUseEvents[1].ToolUse.Input))
	// Two content_block_stop markers, one per tool block. The text
	// block's content_block_stop must NOT emit a marker (non-tool
	// block path returns nil, nil).
	assert.Equal(t, 2, blockStopCount, "content_block_stop emits exactly once per tool block")
}

// TestStreamUnknownContentBlockTypeLogs verifies the debugLogger
// branch in handleContentBlockStart's default case: Anthropic may
// introduce new content_block types (image, redacted_thinking, etc.)
// and the provider must log via debugLogger without emitting an
// event so the stream continues cleanly.
func TestStreamUnknownContentBlockTypeLogs(t *testing.T) {
	sseBody := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"st_1","name":"web_search"}}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})
	var logged []string
	p.SetDebugLogger(func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var types []string
	for evt := range ch {
		types = append(types, evt.Type)
	}

	// No tool_use or error event for the unknown block; stop still fires.
	for _, typ := range types {
		if typ == "tool_use" || typ == "error" {
			t.Errorf("unknown block type should not produce %q event", typ)
		}
	}
	var sawLog bool
	for _, msg := range logged {
		if strings.Contains(msg, "server_tool_use") && strings.Contains(msg, "unknown content_block type") {
			sawLog = true
		}
	}
	assert.True(t, sawLog, "debugLogger must record the unknown content_block type; logged=%v", logged)
}

// TestStreamInputJsonDeltaWithoutBlockStart covers the defensive
// fallback in handleContentBlockDelta where an input_json_delta fragment
// arrives for a block index that was never opened with a tool_use
// content_block_start. Under a valid Anthropic wire stream this is
// unreachable; the provider surfaces it as an error event so a
// protocol regression fails loudly instead of silently dropping tool
// input.
func TestStreamInputJsonDeltaWithoutBlockStart(t *testing.T) {
	sseBody := `event: content_block_delta
data: {"type":"content_block_delta","index":7,"delta":{"type":"input_json_delta","partial_json":"{\"orphan\":true}"}}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var sawError bool
	for evt := range ch {
		if evt.Type == "error" && evt.Error != nil &&
			strings.Contains(evt.Error.Error(), "unknown content block index") {
			sawError = true
		}
	}
	assert.True(t, sawError, "orphan input_json_delta must produce an error StreamEvent")
}

// TestStreamMalformedContentBlockStop covers the error path in
// handleContentBlockStop for malformed JSON payloads.
func TestStreamMalformedContentBlockStop(t *testing.T) {
	sseBody := `event: content_block_stop
data: {not valid json}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var hasError bool
	for evt := range ch {
		if evt.Type == "error" && evt.Error != nil &&
			strings.Contains(evt.Error.Error(), "parsing content_block_stop") {
			hasError = true
		}
	}
	assert.True(t, hasError, "should have received error event for malformed content_block_stop JSON")
}

// TestStreamToolUseEmptyArgs covers the empty-args fallback where a
// tool_use content block closes with zero accumulated input_json_delta
// fragments — the provider must emit a valid empty JSON object so
// downstream parsers don't fail on an empty payload.
func TestStreamToolUseEmptyArgs(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_empty","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_empty","name":"noop","input":{}}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var toolUseEvt *provider.StreamEvent
	for evt := range ch {
		if evt.Type == "tool_use" {
			e := evt
			toolUseEvt = &e
		}
	}
	require.NotNil(t, toolUseEvt, "should emit a tool_use event")
	require.NotNil(t, toolUseEvt.ToolUse)
	assert.JSONEq(t, `{}`, string(toolUseEvt.ToolUse.Input),
		"empty-args tool_use Input must default to valid JSON {}")
}

func TestStreamUnknownDeltaType(t *testing.T) {
	sseBody := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"unknown_delta","text":"test"}}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

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

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:       "claude-sonnet-4-5",
		System:      "You are helpful.",
		Messages:    []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens:   2048,
		Temperature: floatPtr(0.7),
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
	var apiReq map[string]any
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	assert.Equal(t, true, apiReq["stream"])
	assert.Equal(t, "claude-sonnet-4-5", apiReq["model"])
	assert.Equal(t, "You are helpful.", apiReq["system"])
	assert.Equal(t, float64(2048), apiReq["max_tokens"])
	assert.Equal(t, 0.7, apiReq["temperature"])

	// Verify tools are included
	tools, ok := apiReq["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool := tools[0].(map[string]any)
	assert.Equal(t, "read_file", tool["name"])
}

func TestStreamToolResultUsesContentField(t *testing.T) {
	var capturedBody []byte

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	// Send a tool_result message — the result text must serialize as "content", not "text"
	req := provider.CompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []provider.Message{
			provider.NewUserMessage("Read test.txt"),
			{
				Role: "assistant",
				Content: []provider.ContentBlock{
					{Type: "tool_use", ID: "toolu_1", Name: "file", Input: json.RawMessage(`{"path":"test.txt"}`)},
				},
			},
			provider.NewToolResultMessage("toolu_1", "file contents here", false),
		},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)
	for range ch {
	}

	// Parse the captured body and find the tool_result block
	var apiReq map[string]any
	err = json.Unmarshal(capturedBody, &apiReq)
	require.NoError(t, err)

	msgs := apiReq["messages"].([]any)
	// Third message is the tool_result
	toolResultMsg := msgs[2].(map[string]any)
	blocks := toolResultMsg["content"].([]any)
	block := blocks[0].(map[string]any)

	assert.Equal(t, "tool_result", block["type"])
	assert.Equal(t, "toolu_1", block["tool_use_id"])
	assert.Equal(t, "file contents here", block["content"], "tool_result must use 'content' field, not 'text'")
	assert.Nil(t, block["text"], "tool_result must not have 'text' field")
}

func TestStreamThinkingBlocks(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_t","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" about this."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Here is my answer."}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`

	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-api-key")
	p.SetHTTPClient(&http.Client{})

	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Think about this")},
		MaxTokens: 1024,
	}

	ch, err := p.Stream(context.Background(), req)
	require.NoError(t, err)

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Collect events by type
	var thinkingParts []string
	var textParts []string
	var hasStop bool
	for _, evt := range events {
		switch evt.Type {
		case "thinking_delta":
			thinkingParts = append(thinkingParts, evt.Text)
		case "text_delta":
			textParts = append(textParts, evt.Text)
		case "stop":
			hasStop = true
		}
	}

	assert.Equal(t, []string{"Let me think", " about this."}, thinkingParts)
	assert.Equal(t, []string{"Here is my answer."}, textParts)
	assert.True(t, hasStop, "should have received stop event")
}

func TestConvertContentBlocksHandlesThinkingType(t *testing.T) {
	blocks := []provider.ContentBlock{
		{Type: "thinking", Text: "internal reasoning"},
		{Type: "text", Text: "visible answer"},
	}

	out := convertContentBlocks(blocks)
	require.Len(t, out, 2)
	assert.Equal(t, "thinking", out[0].Type)
	assert.Equal(t, "internal reasoning", out[0].Text)
	assert.Equal(t, "text", out[1].Type)
	assert.Equal(t, "visible answer", out[1].Text)
}

func TestTransformerStripsEmptyTextViaNormalization(t *testing.T) {
	// Empty text blocks are stripped by normalization in ToProviderJSON.
	tr := &Transformer{}
	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
		Messages: []provider.Message{{
			Role: "user",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "text", Text: ""},
				{Type: "text", Text: "world"},
			},
		}},
	}

	body, err := tr.ToProviderJSON(req)
	require.NoError(t, err)

	var parsed struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Len(t, parsed.Messages, 1)

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(parsed.Messages[0].Content, &blocks))
	assert.Len(t, blocks, 2, "empty text block should be removed by normalization")
}

func TestProcessStream_ScannerError(t *testing.T) {
	pr, pw := io.Pipe()
	pw.CloseWithError(errors.New("connection reset by peer"))

	p := New("http://localhost", "test-key")
	ch := make(chan provider.StreamEvent)
	go p.processStream(context.Background(), io.NopCloser(pr), ch)

	var events []provider.StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	require.Len(t, events, 1)
	assert.Equal(t, agentsdk.EventError, events[0].Type)

	var pe *provider.ProviderError
	require.ErrorAs(t, events[0].Error, &pe)
	assert.Equal(t, provider.ErrStreamError, pe.Kind)
	assert.Equal(t, "anthropic", pe.Provider)
	assert.True(t, pe.IsRetryable())
}

func floatPtr(f float64) *float64 { return &f }

func TestHandleMessageStart_CacheTokens(t *testing.T) {
	p := New("http://localhost", "test-key")
	data := `{"message":{"id":"msg_01","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":2000,"cache_read_input_tokens":48000}}}`

	evt := p.handleMessageStart(data)
	require.NotNil(t, evt)
	assert.Equal(t, "message_start", evt.Type)
	assert.Equal(t, 100, evt.InputTokens)
	assert.Equal(t, 50, evt.OutputTokens)
	assert.Equal(t, 2000, evt.CacheCreationTokens)
	assert.Equal(t, 48000, evt.CacheReadTokens)
}
