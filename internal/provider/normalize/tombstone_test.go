package normalize

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestFilterTombstoned(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Content: []agentsdk.ContentBlock{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{{Type: "text", Text: agentsdk.TombstoneMarker}}},
		{Role: "user", Content: []agentsdk.ContentBlock{{Type: "text", Text: "Do something"}}},
	}

	filtered := FilterTombstoned(msgs)
	require.Equal(t, 2, len(filtered))
	require.Equal(t, "Hello", filtered[0].Content[0].Text)
	require.Equal(t, "Do something", filtered[1].Content[0].Text)
}

func TestFilterTombstonedEmpty(t *testing.T) {
	filtered := FilterTombstoned(nil)
	require.Empty(t, filtered)
}

func TestFilterTombstonedAllTombstoned(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "assistant", Content: []agentsdk.ContentBlock{{Type: "text", Text: agentsdk.TombstoneMarker}}},
	}
	filtered := FilterTombstoned(msgs)
	require.Equal(t, 0, len(filtered))
}
