package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForkParams(t *testing.T) {
	p := ForkParams{
		SystemPrompt: "You are a summarizer",
		Model:        "claude-3",
		MaxTokens:    1024,
	}
	require.Equal(t, "claude-3", p.Model)
	require.Equal(t, 1024, p.MaxTokens)
}

func TestForkResult(t *testing.T) {
	r := ForkResult{Summary: "done", InputTokens: 100, OutputTokens: 50}
	require.Equal(t, "done", r.Summary)
	require.Equal(t, 100, r.InputTokens)
	require.Equal(t, 50, r.OutputTokens)
}
