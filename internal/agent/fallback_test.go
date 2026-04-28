package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type overloadedThenSuccessProvider struct {
	failCount atomic.Int32
}

func (p *overloadedThenSuccessProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if p.failCount.Add(1) <= 3 {
		return nil, &provider.ProviderError{
			Kind:      provider.ErrServerError,
			Message:   "overloaded: server capacity reached",
			Retryable: true,
		}
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: "fallback response"}
	ch <- provider.StreamEvent{Type: "stop", StopReason: "end_turn", InputTokens: 1, OutputTokens: 1}
	close(ch)
	return ch, nil
}

func TestStripThinkingBlocks(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "hello"},
		}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "thinking", Text: "let me think"},
			{Type: "text", Text: "answer"},
		}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "redacted_thinking"},
			{Type: "text", Text: "more"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 3, len(stripped), "should preserve non-thinking messages")
	assert.Equal(t, 1, len(stripped[1].Content), "assistant msg should have thinking removed")
	assert.Equal(t, "text", stripped[1].Content[0].Type)
}

func TestStripThinkingBlocks_RemovesAllThinking(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "thinking", Text: "deep thought"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 0, len(stripped), "message with only thinking should be removed")
}

func TestStripThinkingBlocks_PreservesNonThinkingMessages(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "hello"},
		}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{
			{Type: "text", Text: "response"},
		}},
	}
	stripped := stripThinkingBlocks(msgs)
	assert.Equal(t, 2, len(stripped))
	assert.Equal(t, 1, len(stripped[0].Content))
	assert.Equal(t, 1, len(stripped[1].Content))
}

func TestRunLoop_ModelOverloaded_FallsBack(t *testing.T) {
	prov := &overloadedThenSuccessProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg, WithFallbackModel("claude-haiku-4"))

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var output string
	var exitReason agentsdk.TurnExitReason
	var sawFallbackEvent bool
	for evt := range ch {
		if evt.Type == "text_delta" {
			output += evt.Text
		}
		if evt.Type == "model_fallback" {
			sawFallbackEvent = true
			assert.Equal(t, "claude-haiku-4", evt.Model)
		}
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.True(t, sawFallbackEvent, "should emit model_fallback event")
	assert.Contains(t, output, "fallback response")
	assert.Equal(t, agentsdk.ExitCompleted, exitReason)
}

func TestRunLoop_ModelOverloaded_NoFallbackConfigured(t *testing.T) {
	prov := &overloadedThenSuccessProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)

	var exitReason agentsdk.TurnExitReason
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitProviderError, exitReason, "without fallback, should exit with provider error")
}
