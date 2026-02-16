package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a simple mock that returns a fixed sequence of stream events.
type mockProvider struct {
	events []provider.StreamEvent
}

func (m *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, len(m.events))
	for _, e := range m.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// dynamicMockProvider returns different responses per call.
type dynamicMockProvider struct {
	responses [][]provider.StreamEvent
	callIdx   int
}

func (d *dynamicMockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	events := d.responses[d.callIdx]
	d.callIdx++
	ch := make(chan provider.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func autoApprove(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return true, nil
}

func autoDeny(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return false, nil
}

func TestNewAgent(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	agent := New(mp, reg, autoApprove, cfg)

	require.NotNil(t, agent)
	assert.Equal(t, cfg.Provider.Model, agent.model)
	assert.Equal(t, cfg.Agent.MaxTurns, agent.maxTurns)
	assert.NotNil(t, agent.conversation)
	assert.NotNil(t, agent.context)
	assert.NotNil(t, agent.provider)
	assert.NotNil(t, agent.tools)
	assert.NotNil(t, agent.approve)
}

func TestNewAgentSystemPrompt(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	agent := New(mp, reg, autoApprove, cfg)

	// System prompt should be non-empty
	assert.NotEmpty(t, agent.conversation.SystemPrompt())
}
