package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyErrorToolResult(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: "error: file not found", IsError: true},
		},
	}
	assert.Equal(t, "HIGH", classifyMessageImportance(msg))
}

func TestClassifyLargeSuccessResult(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: strings.Repeat("x", 600)},
		},
	}
	assert.Equal(t, "LOW", classifyMessageImportance(msg))
}

func TestClassifySmallSuccessResult(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: "ok, 3 files changed"},
		},
	}
	assert.Equal(t, "MEDIUM", classifyMessageImportance(msg))
}

func TestClassifyToolUse(t *testing.T) {
	msg := provider.Message{
		Role: "assistant",
		Content: []provider.ContentBlock{
			{Type: "tool_use", ID: "t1", Name: "read_file", Input: []byte(`{"path":"main.go"}`)},
		},
	}
	assert.Equal(t, "MEDIUM", classifyMessageImportance(msg))
}

func TestClassifyShortUserText(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "text", Text: "no, use snake_case"},
		},
	}
	assert.Equal(t, "HIGH", classifyMessageImportance(msg))
}

func TestClassifyLongUserText(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "text", Text: strings.Repeat("a", 100)},
		},
	}
	assert.Equal(t, "MEDIUM", classifyMessageImportance(msg))
}

func TestClassifyAssistantText(t *testing.T) {
	msg := provider.Message{
		Role: "assistant",
		Content: []provider.ContentBlock{
			{Type: "text", Text: "Here is the implementation you requested."},
		},
	}
	assert.Equal(t, "MEDIUM", classifyMessageImportance(msg))
}

func TestClassifyMixedBlocksHighestWins(t *testing.T) {
	msg := provider.Message{
		Role: "user",
		Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: "success"},
			{Type: "tool_result", ToolUseID: "t2", Text: "error: permission denied", IsError: true},
		},
	}
	assert.Equal(t, "HIGH", classifyMessageImportance(msg))
}

func TestClassifyEmptyContent(t *testing.T) {
	msg := provider.Message{Role: "user", Content: nil}
	assert.Equal(t, "MEDIUM", classifyMessageImportance(msg))
}

// summarizerCapturingProvider records the CompletionRequest for assertion.
type summarizerCapturingProvider struct {
	captured *provider.CompletionRequest
}

func (p *summarizerCapturingProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	p.captured = &req
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: "summary text"}
	close(ch)
	return ch, nil
}

func TestSummarizeIncludesImportanceTags(t *testing.T) {
	mock := &summarizerCapturingProvider{}
	s := NewLLMSummarizer(mock, "test-model")

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "fix it"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "edit_file"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "error: not found", IsError: true}}},
	}

	_, err := s.Summarize(context.Background(), messages)
	require.NoError(t, err)
	require.NotNil(t, mock.captured)

	prompt := mock.captured.Messages[0].Content[0].Text
	assert.Contains(t, prompt, "[HIGH]")
	assert.Contains(t, prompt, "[MEDIUM]")
}

func TestSummarizePromptMentionsImportance(t *testing.T) {
	mock := &summarizerCapturingProvider{}
	s := NewLLMSummarizer(mock, "test-model")

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	_, err := s.Summarize(context.Background(), messages)
	require.NoError(t, err)
	require.NotNil(t, mock.captured)

	system := mock.captured.System
	assert.Contains(t, system, "[HIGH]")
	assert.Contains(t, system, "[MEDIUM]")
	assert.Contains(t, system, "[LOW]")
}
