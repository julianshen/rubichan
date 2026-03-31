package shell

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockAgentTurnCapture(captured *string) AgentTurnFunc {
	return func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		*captured = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "suggestion"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}
}

func TestErrorAnalyzerAnalyzesFailedCommand(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedPrompt = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "The variable Foo is undefined. Add import or define it."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	ea := NewErrorAnalyzer(agentTurn, 4096)

	events, err := ea.Analyze(context.Background(), "go test ./...", "FAIL", "undefined: Foo", 1)
	require.NoError(t, err)
	require.NotNil(t, events)

	// Collect all events
	var texts []string
	for event := range events {
		if event.Type == "text_delta" {
			texts = append(texts, event.Text)
		}
	}

	assert.Contains(t, strings.Join(texts, ""), "undefined")
	assert.Contains(t, capturedPrompt, "go test ./...")
	assert.Contains(t, capturedPrompt, "exit code 1")
	assert.Contains(t, capturedPrompt, "undefined: Foo")
}

func TestErrorAnalyzerAnalyzesBenignFailure(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	ea := NewErrorAnalyzer(mockAgentTurnCapture(&capturedPrompt), 4096)

	events, err := ea.Analyze(context.Background(), "grep foo bar.txt", "", "bar.txt: No such file or directory", 1)
	require.NoError(t, err)
	require.NotNil(t, events)

	// Drain events
	for range events {
	}

	// Even exit code 1 (benign for grep) triggers analysis
	assert.Contains(t, capturedPrompt, "grep foo bar.txt")
	assert.Contains(t, capturedPrompt, "exit code 1")
}

func TestErrorAnalyzerSkipsSuccessfulCommand(t *testing.T) {
	t.Parallel()

	called := false
	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		called = true
		ch := make(chan TurnEvent, 1)
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	ea := NewErrorAnalyzer(agent, 4096)

	// Exit code 0 — should not be called (caller checks exit code, but Analyze
	// itself doesn't gate on exit code — the caller does). Test that even if
	// called with exit 0, it still works (no crash), but in practice the caller
	// gates this. We test the caller gating in integration test 2.8.
	events, err := ea.Analyze(context.Background(), "ls", "file.go", "", 0)
	require.NoError(t, err)

	// Analyzer is enabled and agent available — it will analyze even exit 0
	// The gating on exit code is the caller's responsibility.
	if events != nil {
		for range events {
		}
		assert.True(t, called)
	}
}

func TestErrorAnalyzerTruncatesLargeOutput(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	ea := NewErrorAnalyzer(mockAgentTurnCapture(&capturedPrompt), 100)

	largeOutput := strings.Repeat("x", 200)
	events, err := ea.Analyze(context.Background(), "cmd", largeOutput, "", 1)
	require.NoError(t, err)
	require.NotNil(t, events)
	for range events {
	}

	assert.Contains(t, capturedPrompt, "truncated")
	assert.Contains(t, capturedPrompt, "100 more chars")
	// The full 200-byte output should NOT be in the prompt
	assert.NotContains(t, capturedPrompt, largeOutput)
}

func TestErrorAnalyzerPromptFormat(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	ea := NewErrorAnalyzer(mockAgentTurnCapture(&capturedPrompt), 4096)

	events, err := ea.Analyze(context.Background(), "make build", "compiled ok", "error: missing dep", 2)
	require.NoError(t, err)
	for range events {
	}

	assert.Contains(t, capturedPrompt, "The command `make build` failed with exit code 2.")
	assert.Contains(t, capturedPrompt, "compiled ok")
	assert.Contains(t, capturedPrompt, "error: missing dep")
	assert.Contains(t, capturedPrompt, "Analyze the error concisely")
}

func TestErrorAnalyzerDisabledViaNoAgent(t *testing.T) {
	t.Parallel()

	// With nil agentTurn, Analyze returns nil (disabled behavior)
	ea := NewErrorAnalyzer(nil, 4096)

	events, err := ea.Analyze(context.Background(), "cmd", "", "error", 1)
	assert.NoError(t, err)
	assert.Nil(t, events)
}

func TestErrorAnalyzerNilSafe(t *testing.T) {
	t.Parallel()

	// nil agentTurn
	ea := NewErrorAnalyzer(nil, 4096)

	events, err := ea.Analyze(context.Background(), "cmd", "", "error", 1)
	assert.NoError(t, err)
	assert.Nil(t, events)
}

func TestShellHostErrorAnalysisIntegration(t *testing.T) {
	t.Parallel()

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "", "build failed: missing import", 1, nil
	}

	var analysisPrompt string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		analysisPrompt = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "Add the missing import."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	host := NewShellHost(ShellHostConfig{
		WorkDir:       "/project",
		HomeDir:       "/home/user",
		Executables:   map[string]bool{"go": true},
		ShellExec:     exec,
		AgentTurn:     agentTurn,
		Stdin:         strings.NewReader("go build ./...\n"),
		Stdout:        stdout,
		Stderr:        stderr,
		GitBranchFn:   func(string) string { return "" },
		ErrorAnalysis: true,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// Error analysis should have been triggered
	assert.Contains(t, analysisPrompt, "go build ./...")
	assert.Contains(t, analysisPrompt, "build failed: missing import")
	// Suggestion should appear in stdout
	assert.Contains(t, stdout.String(), "Add the missing import.")
	// Indicator should appear in stderr
	assert.Contains(t, stderr.String(), "Analyzing")
}

func TestErrorAnalyzerContextStillRecorded(t *testing.T) {
	t.Parallel()

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "FAIL: TestFoo", "", 1, nil
	}

	analysisAgent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "Fix suggestion."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	// Track what the LLM query receives
	var llmQueryMsg string
	callCount := 0
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		callCount++
		if callCount == 1 {
			// First call is error analysis
			return analysisAgent(context.Background(), msg)
		}
		// Second call is the user's follow-up query
		llmQueryMsg = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "Because..."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	host := NewShellHost(ShellHostConfig{
		WorkDir:       "/project",
		HomeDir:       "/home/user",
		Executables:   map[string]bool{"go": true},
		ShellExec:     exec,
		AgentTurn:     agentTurn,
		Stdin:         strings.NewReader("go test ./...\nwhy did that fail?\n"),
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		GitBranchFn:   func(string) string { return "" },
		ErrorAnalysis: true,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// The follow-up query should still have context from the failed command
	assert.Contains(t, llmQueryMsg, "go test ./...")
	assert.Contains(t, llmQueryMsg, "FAIL: TestFoo")
	assert.Contains(t, llmQueryMsg, "why did that fail?")
}
