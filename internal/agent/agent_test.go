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

func TestTurnTextOnly(t *testing.T) {
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hello "},
			{Type: "text_delta", Text: "world!"},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "say hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have: text_delta "Hello ", text_delta "world!", done
	require.GreaterOrEqual(t, len(events), 3)

	// Verify text deltas
	var textContent string
	for _, ev := range events {
		if ev.Type == "text_delta" {
			textContent += ev.Text
		}
	}
	assert.Equal(t, "Hello world!", textContent)

	// Last event should be done
	assert.Equal(t, "done", events[len(events)-1].Type)

	// Conversation should have 2 messages: user + assistant
	msgs := agent.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Hello world!", msgs[1].Content[0].Text)
}

func TestTurnMaxTurnsExceeded(t *testing.T) {
	// Create a provider that always returns a tool call to force recursive loops.
	// But we set maxTurns to 0 so the first runLoop iteration hits the limit.
	mp := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "hi"},
			{Type: "stop"},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 0 // immediate limit
	agent := New(mp, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have error event about max turns and done event
	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
			assert.Contains(t, ev.Error.Error(), "max turns")
		}
	}
	assert.True(t, hasError, "should emit error event for max turns exceeded")
	assert.Equal(t, "done", events[len(events)-1].Type)
}
