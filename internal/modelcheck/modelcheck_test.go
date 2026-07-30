package modelcheck_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/modelcheck"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider scripts one stream per call so a test can drive the
// connectivity probe and the tool probe independently, and records the
// requests so the probes' shape can be asserted.
type fakeProvider struct {
	eventsByCall [][]provider.StreamEvent
	errByCall    []error
	requests     []provider.CompletionRequest
	streams      []chan provider.StreamEvent
	callCount    int
}

func (p *fakeProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	p.requests = append(p.requests, req)
	idx := p.callCount
	p.callCount++
	if idx < len(p.errByCall) && p.errByCall[idx] != nil {
		return nil, p.errByCall[idx]
	}
	var events []provider.StreamEvent
	if idx < len(p.eventsByCall) {
		events = p.eventsByCall[idx]
	}
	ch := make(chan provider.StreamEvent, len(events))
	for _, evt := range events {
		ch <- evt
	}
	close(ch)
	p.streams = append(p.streams, ch)
	return ch, nil
}

// undrained reports how many events are still sitting in the streams handed
// out. A real provider blocks on send until its context is cancelled, so
// anything left here would be a stranded producer in production.
func (p *fakeProvider) undrained() int {
	n := 0
	for _, ch := range p.streams {
		n += len(ch)
	}
	return n
}

func TestRunSuccess(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "text_delta", Text: "OK"}, {Type: "stop"}},
		{{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe", Input: []byte(`{}`)}}, {Type: "stop"}},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.NoError(t, err)
	require.Len(t, p.requests, 2)
	assert.Equal(t, "gpt-4o", p.requests[0].Model)
	assert.Equal(t, "capability_probe", p.requests[1].Tools[0].Name)
	assert.Contains(t, out.String(), "Provider: openai")
	assert.Contains(t, out.String(), "Capabilities:")
	assert.Contains(t, out.String(), "Tool support: PASS")
	assert.Contains(t, out.String(), "Model test: PASS")
}

func TestRunMissingModel(t *testing.T) {
	var out bytes.Buffer
	err := modelcheck.Run(context.Background(), &out, &fakeProvider{}, "openai", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is not configured")
}

func TestRunStreamErrorEvent(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{{{Type: "error", Error: fmt.Errorf("boom")}}}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model stream test failed")
}

func TestRunToolSupportMissing(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "text_delta", Text: "OK"}, {Type: "stop"}},
		{{Type: "stop"}},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Tool support: INCONCLUSIVE")
}

func TestRunRejectsNilProvider(t *testing.T) {
	var out bytes.Buffer
	err := modelcheck.Run(context.Background(), &out, nil, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider is nil")
	assert.Empty(t, out.String(), "nothing should be reported about a provider that does not exist")
}

func TestRunReportsConnectivityFailure(t *testing.T) {
	p := &fakeProvider{errByCall: []error{fmt.Errorf("dial tcp: refused")}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model connectivity test failed")
	assert.Contains(t, err.Error(), "dial tcp: refused", "the transport's reason is what makes this actionable")
}

// TestRunRequiresAStopEvent covers a stream that ends without stopping — a
// truncated or half-closed response. It has to fail: a model that never
// finished a sixteen-token reply has not demonstrated it works.
func TestRunRequiresAStopEvent(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "text_delta", Text: "OK"}},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model stream ended without stop event")
	assert.NotContains(t, out.String(), "Model test: PASS")
}

func TestRunReportsToolProbeRequestFailure(t *testing.T) {
	p := &fakeProvider{
		eventsByCall: [][]provider.StreamEvent{{{Type: "stop"}}},
		errByCall:    []error{nil, fmt.Errorf("429 rate limited")},
	}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool support test request failed")
	assert.Contains(t, err.Error(), "429 rate limited")
}

func TestRunReportsToolProbeStreamError(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{{Type: "error", Error: fmt.Errorf("tools unsupported")}},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool support stream failed")
}

// TestRunRequiresAStopEventFromTheToolProbe distinguishes a model that
// declined the probe from one whose stream died mid-probe. The first is
// inconclusive; the second is an error, because nothing was learned.
func TestRunRequiresAStopEventFromTheToolProbe(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{{Type: "text_delta", Text: "thinking"}},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool support stream ended without stop event")
	assert.NotContains(t, out.String(), "INCONCLUSIVE",
		"a dead stream must not be reported as the model declining the probe")
}

// TestRunProbeOffersExactlyOneTool pins the probe's shape. A probe that
// offered several tools, or a schema the model could not satisfy, would
// measure the prompt rather than the capability.
func TestRunProbeOffersExactlyOneTool(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe"}}, {Type: "stop"}},
	}}
	var out bytes.Buffer

	require.NoError(t, modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o"))
	require.Len(t, p.requests, 2)
	probe := p.requests[1]
	require.Len(t, probe.Tools, 1)
	assert.Equal(t, "capability_probe", probe.Tools[0].Name)
	assert.JSONEq(t, `{"type":"object","properties":{},"additionalProperties":false}`,
		string(probe.Tools[0].InputSchema))
	assert.Contains(t, probe.Messages[0].Content[0].Text, "capability_probe",
		"the prompt must name the tool it offers")
}

// TestRunIgnoresAnUnrelatedToolCall guards the name comparison: a model that
// invents its own tool has not demonstrated it can call the one it was given.
func TestRunIgnoresAnUnrelatedToolCall(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "something_else"}}, {Type: "stop"}},
	}}
	var out bytes.Buffer

	require.NoError(t, modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o"))
	assert.Contains(t, out.String(), "Tool support: INCONCLUSIVE")
}

// TestRunFailsOnErrorAfterToolUse covers a stream that demonstrates the
// capability and then falls over. Ollama does exactly this: it emits tool
// calls and, if the connection drops before the done chunk, reports "stream
// ended without done signal". A diagnostic that watched a probe fail and
// printed PASS is worse than useless.
func TestRunFailsOnErrorAfterToolUse(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe"}},
			{Type: "error", Error: fmt.Errorf("stream ended without done signal")},
		},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool support stream failed")
	assert.NotContains(t, out.String(), "Model test: PASS")
}

// TestRunFailsOnMissingStopAfterToolUse holds the two probes to the same
// standard: Run already fails the connectivity probe when its stream ends
// without a stop, so the tool probe cannot quietly accept one.
func TestRunFailsOnMissingStopAfterToolUse(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe"}}},
	}}
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool support stream ended without stop event")
}

// TestRunDrainsTheToolProbeStream pins that the probe consumes its stream to
// completion. Providers send with `select { case ch <- evt: case <-ctx.Done() }`
// and Run is handed a context that never cancels, so a probe that returned
// early would strand the producer goroutine and its connection.
func TestRunDrainsTheToolProbeStream(t *testing.T) {
	p := &fakeProvider{eventsByCall: [][]provider.StreamEvent{
		{{Type: "stop"}},
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe"}},
			{Type: "text_delta", Text: "trailing"},
			{Type: "stop"},
		},
	}}
	var out bytes.Buffer

	require.NoError(t, modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o"))
	assert.Contains(t, out.String(), "Tool support: PASS")
	assert.Equal(t, 0, p.undrained(), "every event offered must be consumed")
}

// blockingProvider mimics how real providers emit: a goroutine sending on an
// unbuffered channel with `select { case ch <- evt: case <-ctx.Done(): }`, as
// internal/provider/ollama and internal/provider/ssecompat both do. A consumer
// that stops reading strands that goroutine until the context is cancelled,
// which a fake with a buffered, pre-closed channel can never reveal.
type blockingProvider struct {
	eventsByCall [][]provider.StreamEvent
	callCount    int
	exited       chan struct{}
}

func newBlockingProvider(eventsByCall [][]provider.StreamEvent) *blockingProvider {
	return &blockingProvider{eventsByCall: eventsByCall, exited: make(chan struct{}, 8)}
}

func (p *blockingProvider) Stream(ctx context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	idx := p.callCount
	p.callCount++
	var events []provider.StreamEvent
	if idx < len(p.eventsByCall) {
		events = p.eventsByCall[idx]
	}

	ch := make(chan provider.StreamEvent)
	go func() {
		defer func() { p.exited <- struct{}{} }()
		defer close(ch)
		for _, evt := range events {
			select {
			case ch <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// awaitProducerExit fails if the producer goroutine is still parked on a send.
func (p *blockingProvider) awaitProducerExit(t *testing.T) {
	t.Helper()
	for i := 0; i < p.callCount; i++ {
		select {
		case <-p.exited:
		case <-time.After(2 * time.Second):
			t.Fatal("provider goroutine still blocked on send — the stream was abandoned without cancelling its context")
		}
	}
}

// TestRunReleasesTheProducerAfterAConnectivityError covers an error event that
// is not the end of the stream. Both the Ollama and SSE-compatible parsers
// emit a parse error and keep scanning, so returning on the error leaves the
// producer holding a connection with no one reading.
func TestRunReleasesTheProducerAfterAConnectivityError(t *testing.T) {
	p := newBlockingProvider([][]provider.StreamEvent{
		{
			{Type: "error", Error: fmt.Errorf("parsing chunk: unexpected token")},
			{Type: "text_delta", Text: "more output nobody is reading"},
			{Type: "stop"},
		},
	})
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model stream test failed")
	p.awaitProducerExit(t)
}

// TestRunReleasesTheProducerAfterAToolProbeError is the same hazard on the
// second probe, which is where review first spotted it.
func TestRunReleasesTheProducerAfterAToolProbeError(t *testing.T) {
	p := newBlockingProvider([][]provider.StreamEvent{
		{{Type: "stop"}},
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{Name: "capability_probe"}},
			{Type: "error", Error: fmt.Errorf("parsing chunk: unexpected token")},
			{Type: "text_delta", Text: "still going"},
			{Type: "stop"},
		},
	})
	var out bytes.Buffer

	err := modelcheck.Run(context.Background(), &out, p, "openai", "gpt-4o")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool support stream failed")
	p.awaitProducerExit(t)
}
