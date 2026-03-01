package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceCompactResult(t *testing.T) {
	// Budget is 500 tokens; filling ~40 messages will exceed it, causing
	// truncation to actually remove messages.
	cm := NewContextManager(500, 0)
	conv := NewConversation("sys")

	// Fill with enough messages to make compaction meaningful.
	for i := 0; i < 20; i++ {
		conv.AddUser("message content for testing compaction behavior")
		conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response content"}})
	}

	beforeTokens := cm.EstimateTokens(conv)
	beforeMsgs := len(conv.Messages())

	result := cm.ForceCompact(context.Background(), conv)

	assert.Equal(t, beforeTokens, result.BeforeTokens)
	assert.Equal(t, beforeMsgs, result.BeforeMsgCount)
	assert.LessOrEqual(t, result.AfterTokens, beforeTokens)
	assert.Greater(t, len(result.StrategiesRun), 0)
}

func TestForceCompactEmptyConversation(t *testing.T) {
	cm := NewContextManager(100000, 0)
	conv := NewConversation("sys")

	result := cm.ForceCompact(context.Background(), conv)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.BeforeMsgCount)
}
