package modelcheck_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

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
	return ch, nil
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
