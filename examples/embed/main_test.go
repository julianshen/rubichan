package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbedderComposesThreeSeams is the example's reason for existing: it
// proves the three modules an embedder opts into actually compose and fire
// during a real turn against the core loop.
func TestEmbedderComposesThreeSeams(t *testing.T) {
	e := compose()

	assistant, err := e.driveTurn(context.Background(), "greet the release team")
	require.NoError(t, err)
	assert.Equal(t, "Release team greeted.", assistant)

	// Tool middleware wrapped the one tool call the script asked for.
	assert.Equal(t, 1, e.toolCalls.get(), "tool middleware must wrap the tool execution")

	// ContextStrategy: its section reached the system prompt the provider
	// received — the strategy genuinely contributed to prompt construction,
	// not just to a counter.
	require.NotEmpty(t, e.provider.capturedSystems())
	var sawSection bool
	for _, sys := range e.provider.capturedSystems() {
		if strings.Contains(sys, "Deployment Window") &&
			strings.Contains(sys, "deploys are frozen") {
			sawSection = true
		}
	}
	assert.True(t, sawSection, "ContextStrategy section must appear in the system prompt")
	assert.GreaterOrEqual(t, e.strategy.calls(), 1)

	// BackgroundTask observed the full lifecycle. EndSession fires on its
	// own goroutine after the loop exits, so wait for it.
	require.Eventually(t, func() bool {
		ev := e.auditor.events()
		return len(ev) > 0 && ev[len(ev)-1] == "end"
	}, 2*time.Second, 10*time.Millisecond, "EndSession must fire after the loop exits")

	ev := e.auditor.events()
	assert.Contains(t, ev, "start", "BackgroundTask must be started before a model call")
	assert.Contains(t, ev, "join", "BackgroundTask must be joined after tool execution")
	assert.Contains(t, ev, "end", "BackgroundTask must be signalled at session end")
}

// TestRunPrintsModuleObservations is a lightweight smoke test that the
// runnable entrypoint executes end to end without error.
func TestRunPrintsModuleObservations(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, run(&buf))
	out := buf.String()
	assert.Contains(t, out, "assistant said: Release team greeted.")
	assert.Contains(t, out, "tool middleware saw 1 call")
}
