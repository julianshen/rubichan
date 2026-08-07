package agent

import (
	"context"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestBuildSummaryPrompt(t *testing.T) {
	prompt := buildSummaryPrompt("")
	require.Contains(t, prompt, "3-5 words")
	require.Contains(t, prompt, "present tense")
	require.Contains(t, prompt, "Reading runAgent.ts")
	require.NotContains(t, prompt, "Previous:")
}

func TestBuildSummaryPromptWithPrevious(t *testing.T) {
	prompt := buildSummaryPrompt("Previous summary")
	require.Contains(t, prompt, "Previous: \"Previous summary\"")
	require.Contains(t, prompt, "say something NEW")
}

func TestSummaryHandleStop(t *testing.T) {
	called := false
	handle := &SummaryHandle{
		stopFn: func() {
			called = true
		},
	}
	handle.Stop()
	require.True(t, called)
}

func TestStartAgentSummarization(t *testing.T) {
	oldInterval := summaryInterval
	summaryInterval = 50 * time.Millisecond
	// Restored via Cleanup rather than defer so it is ordered against the
	// summarizer's Stop, which is also a Cleanup. Cleanups run last-registered
	// first, so registering the restore here and Stop below means Stop wins:
	// the summarizer is halted before the global it reads is written back. A
	// defer would run before every Cleanup and restore the interval underneath
	// a still-running scheduleNext.
	t.Cleanup(func() { summaryInterval = oldInterval })

	// The summarizer invokes onSummary from its own goroutine. A buffered
	// channel both carries the value and supplies the happens-before edge, so
	// the test waits for the callback to actually run instead of sleeping and
	// hoping. Buffered and non-blocking to send, so a late tick after the
	// assertion cannot wedge the summarizer goroutine.
	received := make(chan string, 4)
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		return "Reading test.go", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "do something"}}},
		}
	}
	onSummary := func(taskID, summary string) {
		select {
		case received <- summary:
		default:
		}
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, onSummary)
	require.NotNil(t, handle)
	// Registered before the assertion so a failure still stops the summarizer.
	t.Cleanup(handle.Stop)

	select {
	case got := <-received:
		require.Equal(t, "Reading test.go", got)
	case <-time.After(2 * time.Second):
		t.Fatal("summarizer produced no summary")
	}
}

func TestSummarizerNotEnoughMessages(t *testing.T) {
	oldInterval := summaryInterval
	summaryInterval = 50 * time.Millisecond
	defer func() { summaryInterval = oldInterval }()

	callModelCalled := false
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		callModelCalled = true
		return "", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		}
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, nil)
	require.NotNil(t, handle)

	time.Sleep(150 * time.Millisecond)
	require.False(t, callModelCalled, "should not call model with < 3 messages")

	handle.Stop()
}

func TestSummarizerStopsCorrectly(t *testing.T) {
	oldInterval := summaryInterval
	summaryInterval = 50 * time.Millisecond
	defer func() { summaryInterval = oldInterval }()

	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		return "", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "do something"}}},
		}
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, nil)
	require.NotNil(t, handle)

	handle.Stop()
	time.Sleep(150 * time.Millisecond)
}

func TestFilterIncompleteToolCalls(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", Text: "thinking..."},
			{Type: "tool_use", Name: "shell"},
		}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", Text: "result"}}},
	}

	filtered := filterIncompleteToolCalls(messages)
	require.Len(t, filtered, 3)
	require.Equal(t, "user", filtered[0].Role)
	require.Equal(t, "assistant", filtered[1].Role)
	require.Equal(t, "user", filtered[2].Role)
}

func TestFilterIncompleteToolCallsNoToolUse(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	filtered := filterIncompleteToolCalls(messages)
	require.Len(t, filtered, 2)
}

func TestSummarizerPreviousSummaryTracking(t *testing.T) {
	oldInterval := summaryInterval
	summaryInterval = 50 * time.Millisecond
	// Cleanup, not defer — see TestStartAgentSummarization for why the ordering
	// against handle.Stop matters.
	t.Cleanup(func() { summaryInterval = oldInterval })

	callCount := 0
	callModel := func(ctx context.Context, messages []provider.Message, systemPrompt string) (string, error) {
		callCount++
		if callCount == 1 {
			return "First summary", nil
		}
		return "Second summary", nil
	}
	getMessages := func() []provider.Message {
		return []provider.Message{
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "do something"}}},
		}
	}

	// Same as above: onSummary runs on the summarizer's goroutine, so the test
	// waits for two callbacks to actually complete rather than sleeping for a
	// duration it hopes is long enough.
	received := make(chan string, 8)
	onSummary := func(taskID, summary string) {
		select {
		case received <- summary:
		default:
		}
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, onSummary)
	require.NotNil(t, handle)
	t.Cleanup(handle.Stop)

	var got []string
	for len(got) < 2 {
		select {
		case s := <-received:
			got = append(got, s)
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d summaries arrived, want 2", len(got))
		}
	}
	require.Equal(t, "First summary", got[0])
	require.Equal(t, "Second summary", got[1])
}
