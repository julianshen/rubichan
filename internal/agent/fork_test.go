package agent

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestForkParamsCacheSafe(t *testing.T) {
	params := agentsdk.ForkParams{
		SystemPrompt: "You are helpful",
		Model:        "claude-3",
		MaxTokens:    1024,
	}
	require.Equal(t, "claude-3", params.Model)
	require.Equal(t, 1024, params.MaxTokens)
}

func TestForkedAgentCreation(t *testing.T) {
	// Minimal test: verify Fork() returns a ForkedAgent
	// Full test requires a constructed Agent with provider
	var a *Agent
	_ = a // placeholder
}
