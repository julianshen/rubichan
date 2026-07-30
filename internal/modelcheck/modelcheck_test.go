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
