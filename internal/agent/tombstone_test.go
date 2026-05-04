package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestConversationTombstone(t *testing.T) {
	conv := NewConversation("")
	conv.AddUser("Hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "Hi"}})
	conv.AddUser("Do something")

	// Tombstone the last user message
	conv.Tombstone(2, 3, agentsdk.TombstoneReasonModelFallback)

	msgs := conv.Messages()
	require.Equal(t, 3, len(msgs))
	require.False(t, agentsdk.IsTombstoned(msgs[0].Content[0].Text))
	require.False(t, agentsdk.IsTombstoned(msgs[1].Content[0].Text))
	require.True(t, agentsdk.IsTombstoned(msgs[2].Content[0].Text))
}

func TestTombstoneSinceLastAssistant(t *testing.T) {
	conv := NewConversation("")
	conv.AddUser("Hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "Hi"}})
	conv.AddUser("Do something")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "Working..."}}) // partial

	count := conv.TombstoneSinceLastAssistant(agentsdk.TombstoneReasonModelFallback)
	require.Equal(t, 0, count) // Last assistant is complete, nothing to tombstone

	// Now add a partial user message
	conv.AddUser("More")
	count = conv.TombstoneSinceLastAssistant(agentsdk.TombstoneReasonModelFallback)
	require.Equal(t, 1, count)
}
