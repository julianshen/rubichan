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
	defer func() { summaryInterval = oldInterval }()

	var summaryReceived string
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
		summaryReceived = summary
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, onSummary)
	require.NotNil(t, handle)

	time.Sleep(150 * time.Millisecond)
	require.Equal(t, "Reading test.go", summaryReceived)

	handle.Stop()
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
	defer func() { summaryInterval = oldInterval }()

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

	var summaries []string
	onSummary := func(taskID, summary string) {
		summaries = append(summaries, summary)
	}

	handle := StartAgentSummarization("task-1", callModel, "system", getMessages, onSummary)
	require.NotNil(t, handle)

	time.Sleep(200 * time.Millisecond)
	require.GreaterOrEqual(t, len(summaries), 2)
	require.Equal(t, "First summary", summaries[0])
	require.Equal(t, "Second summary", summaries[1])

	handle.Stop()
}
