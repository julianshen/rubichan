package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgent_GenerationStartsAtZero(t *testing.T) {
	prov := &dynamicMockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg)
	assert.Equal(t, int64(0), agent.Generation(), "generation should start at 0")
}

func TestAgent_GenerationIncrementsPerTurn(t *testing.T) {
	prov := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "text_delta", Text: "hi"},
				{Type: "stop", StopReason: "end_turn", InputTokens: 1, OutputTokens: 1},
			},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg)

	genBefore := agent.Generation()
	ch, err := agent.Turn(context.Background(), "hello")
	require.NoError(t, err)
	for range ch {
	}
	assert.Equal(t, genBefore+1, agent.Generation(), "generation should increment after Turn")
}

func TestAgent_GenerationDifferentAcrossTurns(t *testing.T) {
	prov := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "text_delta", Text: "first"},
				{Type: "stop", StopReason: "end_turn", InputTokens: 1, OutputTokens: 1},
			},
			{
				{Type: "text_delta", Text: "second"},
				{Type: "stop", StopReason: "end_turn", InputTokens: 1, OutputTokens: 1},
			},
		},
	}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	agent := New(prov, reg, autoApprove, cfg)

	ch, err := agent.Turn(context.Background(), "first")
	require.NoError(t, err)
	for range ch {
	}
	gen1 := agent.Generation()

	ch, err = agent.Turn(context.Background(), "second")
	require.NoError(t, err)
	for range ch {
	}
	gen2 := agent.Generation()

	assert.Equal(t, gen1+1, gen2, "generation should increment across turns")
}
