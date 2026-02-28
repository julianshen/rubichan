package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestComputeSignalsEmpty(t *testing.T) {
	s := ComputeConversationSignals(nil)
	assert.Equal(t, 0.0, s.ErrorDensity)
	assert.Equal(t, 0.0, s.ToolCallDensity)
	assert.Equal(t, 0, s.MessageCount)
}

func TestComputeSignalsNoErrors(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	s := ComputeConversationSignals(messages)
	assert.Equal(t, 0.0, s.ErrorDensity)
}

func TestComputeSignalsAllErrors(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", IsError: true, Text: "err1"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", IsError: true, Text: "err2"}}},
	}
	s := ComputeConversationSignals(messages)
	assert.Equal(t, 1.0, s.ErrorDensity)
}

func TestComputeSignalsMixedErrors(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", IsError: true, Text: "err"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", IsError: true, Text: "err2"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hmm"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "try again"}}},
	}
	s := ComputeConversationSignals(messages)
	assert.InDelta(t, 0.4, s.ErrorDensity, 0.001) // 2/5
}

func TestComputeSignalsToolDensity(t *testing.T) {
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "read"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: "data"}}},
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	s := ComputeConversationSignals(messages)
	assert.InDelta(t, 0.5, s.ToolCallDensity, 0.001) // 2/4
}

func TestComputeSignalsMessageCount(t *testing.T) {
	messages := make([]provider.Message, 7)
	for i := range messages {
		messages[i] = provider.Message{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "msg"}}}
	}
	s := ComputeConversationSignals(messages)
	assert.Equal(t, 7, s.MessageCount)
}
